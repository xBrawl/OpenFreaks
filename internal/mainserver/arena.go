package mainserver

import (
	"crypto/rand"
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/sim"
	"github.com/freaks/openfreaks/internal/store"
	"math/big"
	"sort"
	"time"
)

func (s *Server) beginArena(room string, a *store.Allocation) {
	t := &store.ArenaTournament{ID: "arena:" + room, LobbyLocationID: int(a.LocationID), Round: 1, CreatedAt: a.StartedAt, PhaseAt: time.Now().UnixMilli(), Phase: "lobby", Players: map[int64]*store.ArenaPlayer{}, Teams: map[int]*store.ArenaTeam{}, Battles: map[string]bool{room: true}}
	for _, slot := range a.Players {
		t.Players[slot.PlayerID] = &store.ArenaPlayer{Slot: slot, CurrentRoom: room}
		t.Teams[slot.Team] = &store.ArenaTeam{Lives: 3}
	}
	s.store.Meta.Arenas[t.ID] = t
	a.ArenaID = t.ID
	a.ArenaLobby = true
	s.prepareArenaChoices(t)
}
func (s *Server) prepareArenaChoices(t *store.ArenaTournament) {
	for _, p := range t.Players {
		p.Selected = false
		p.Choices = nil
		p.Rerolls = nil
		if p.Slot.Abandoned || t.Teams[p.Slot.Team].Lives <= 0 {
			continue
		}
		for i := 0; i < 3; i++ {
			id := s.pickArenaAugment(p)
			if id < 1 {
				break
			}
			p.Choices = append(p.Choices, id)
			p.Rerolls = append(p.Rerolls, 1)
		}
		if p.Slot.IsBot && len(p.Choices) > 0 {
			p.Augments = append(p.Augments, p.Choices[0])
			p.Selected = true
			p.Choices = nil
		}
	}
}
func (s *Server) pickArenaAugment(p *store.ArenaPlayer) int32 {
	excluded := map[int32]bool{}
	for _, id := range p.Augments {
		excluded[id] = true
	}
	for _, id := range p.Choices {
		excluded[id] = true
	}
	available := []int32{}
	for _, d := range s.catData.Definitions["augments"] {
		if !excluded[int32(d.ID)] && sim.SupportedAugments[d.String("name")] {
			available = append(available, int32(d.ID))
		}
	}
	if len(available) == 0 {
		return -1
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(available))))
	if err != nil {
		return available[0]
	}
	return available[n.Int64()]
}
func (s *Server) arenaPlayer(c *Conn) (*store.ArenaTournament, *store.ArenaPlayer) {
	a := s.store.Meta.Allocations[c.BattleID]
	if a == nil || !a.ArenaLobby {
		return nil, nil
	}
	t := s.store.Meta.Arenas[a.ArenaID]
	if t == nil || t.Phase != "lobby" {
		return nil, nil
	}
	p := t.Players[c.PlayerID]
	if p == nil || p.Slot.Abandoned || t.Teams[p.Slot.Team].Lives <= 0 {
		return nil, nil
	}
	return t, p
}
func (s *Server) sendArenaLobby(c *Conn) bool {
	t, p := s.arenaPlayer(c)
	if p == nil {
		return true
	}
	w := protocol.NewWriter()
	w.WriteInt(int32(t.Round))
	w.WriteInt(int32(t.Teams[p.Slot.Team].Lives))
	choices := p.Choices
	if p.Selected {
		choices = nil
	}
	w.WriteInt(int32(len(choices)))
	for i, id := range choices {
		(&protocol.AugmentSlot{ModelID: id, RerollsLeft: p.Rerolls[i]}).Encode(w)
	}
	return c.Send(protocol.SArenaLobbyInfo, w.Bytes())
}
func (s *Server) onAugmentSelect(c *Conn, payload []byte) bool {
	id, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	_, p := s.arenaPlayer(c)
	if p == nil || p.Selected {
		return true
	}
	for _, choice := range p.Choices {
		if choice == id {
			p.Augments = append(p.Augments, id)
			p.Selected = true
			break
		}
	}
	return s.sendArenaLobby(c)
}
func (s *Server) onAugmentReroll(c *Conn, payload []byte) bool {
	slot, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	_, p := s.arenaPlayer(c)
	if p == nil || p.Selected {
		return true
	}
	if slot < 0 || int(slot) >= len(p.Choices) || p.Rerolls[slot] <= 0 {
		return s.sendArenaLobby(c)
	}
	id := s.pickArenaAugment(p)
	if id < 1 {
		return s.sendArenaLobby(c)
	}
	p.Choices[slot] = id
	p.Rerolls[slot]--
	w := protocol.NewWriter()
	w.WriteInt(slot)
	(&protocol.AugmentSlot{ModelID: id, RerollsLeft: p.Rerolls[slot]}).Encode(w)
	return c.Send(protocol.SArenaAugmentRerollResponse, w.Bytes())
}
func (s *Server) tickArenas() {
	s.retryBattleLeaves()
	s.recoverArenaRooms()
	for _, t := range s.store.Meta.Arenas {
		s.allocateArenaPending(t)
		if len(t.Battles) == 0 && len(t.PendingTeams) == 0 {
			s.arenaNextPhase(t)
			continue
		}
		if t.Phase != "lobby" || time.Now().UnixMilli()-t.PhaseAt < 2000 {
			continue
		}
		for id, p := range t.Players {
			if p.Slot.IsBot || p.Selected {
				continue
			}
			if c, ok := s.ConnByPlayer(id); ok && c.ArenaInfoBattle != c.BattleID && time.Since(c.AuthedAt) > 2*time.Second {
				if _, player := s.arenaPlayer(c); player != nil {
					s.sendArenaLobby(c)
					c.ArenaInfoBattle = c.BattleID
				}
			}
		}
	}
}

type arenaRoomStatus interface {
	RoomStatus([]string) (map[string]bool, error)
}

func (s *Server) recoverArenaRooms() {
	if time.Since(s.arenaCheckedAt) < 5*time.Second {
		return
	}
	s.arenaCheckedAt = time.Now()
	backend, ok := s.battle.(arenaRoomStatus)
	if !ok {
		return
	}
	ids := []string{}
	for _, t := range s.store.Meta.Arenas {
		for id := range t.Battles {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	status, err := backend.RoomStatus(ids)
	if err != nil {
		return
	}
	if s.arenaMissing == nil {
		s.arenaMissing = map[string]time.Time{}
	}
	for id, present := range status {
		if present {
			delete(s.arenaMissing, id)
			continue
		}
		if s.arenaMissing[id].IsZero() {
			s.arenaMissing[id] = time.Now()
			continue
		}
		// Allow committed results/outbox retries to arrive before replacing a
		// lost room. A replacement replays the round without consuming a life.
		if time.Since(s.arenaMissing[id]) < 30*time.Second {
			continue
		}
		a := s.store.Meta.Allocations[id]
		if a == nil {
			delete(s.arenaMissing, id)
			continue
		}
		t := s.store.Meta.Arenas[a.ArenaID]
		if t == nil {
			continue
		}
		teams := map[int]bool{}
		for _, p := range a.Players {
			if member := t.Players[p.PlayerID]; member != nil && t.Teams[member.Slot.Team].Lives > 0 {
				teams[member.Slot.Team] = true
			}
		}
		plan := []int{}
		for team := range teams {
			plan = append(plan, team)
		}
		sort.Ints(plan)
		delete(t.Battles, id)
		delete(s.store.Meta.Allocations, id)
		delete(s.arenaMissing, id)
		s.store.Meta.Receipts["battle:"+id] = time.Now().UnixMilli()
		if len(plan) > 0 {
			t.PendingTeams = append(t.PendingTeams, plan)
		}
	}
}
func arenaTeams(t *store.ArenaTournament) []int {
	teams := []int{}
	for id, team := range t.Teams {
		if team.Lives > 0 {
			teams = append(teams, id)
		}
	}
	sort.Ints(teams)
	return teams
}
func (s *Server) arenaNextPhase(t *store.ArenaTournament) {
	if len(t.Battles) > 0 || len(t.PendingTeams) > 0 {
		return
	}
	teams := arenaTeams(t)
	if len(teams) <= 1 {
		s.finishArena(t, teams)
		return
	}
	t.PhaseAt = time.Now().UnixMilli()
	if t.Phase == "lobby" {
		for _, p := range t.Players {
			if !p.Selected && len(p.Choices) > 0 {
				p.Augments = append(p.Augments, p.Choices[0])
				p.Selected = true
			}
			p.Choices = nil
		}
		t.Phase = "duels"
		shift := t.Round % len(teams)
		teams = append(append([]int(nil), teams[shift:]...), teams[:shift]...)

		for i := 0; i+1 < len(teams); i += 2 {
			t.PendingTeams = append(t.PendingTeams, []int{teams[i], teams[i+1]})
		}
	} else {
		t.Round++
		t.Phase = "lobby"
		s.prepareArenaChoices(t)
		t.PendingTeams = [][]int{teams}
	}
	s.allocateArenaPending(t)
}
func (s *Server) allocateArenaPending(t *store.ArenaTournament) {
	for len(t.PendingTeams) > 0 {
		teams := t.PendingTeams[0]
		loc := s.catData.LocByID[t.LobbyLocationID]
		if loc == nil {
			return
		}
		lobby := t.Phase == "lobby"
		if !lobby {
			if len(loc.RoundLocations) == 0 {
				return
			}
			loc = s.catData.LocByName[loc.RoundLocations[(t.Round-1)%len(loc.RoundLocations)]]
			if loc == nil {
				return
			}
		}
		mode := s.catData.ModeByName[loc.Mode]
		if mode == nil {
			return
		}
		members := []*store.ArenaPlayer{}
		teamMap := map[int]int{}
		for i, id := range teams {
			teamMap[id] = i
		}
		for _, p := range t.Players {
			if _, ok := teamMap[p.Slot.Team]; ok && !p.Slot.Abandoned {
				members = append(members, p)
			}
		}
		sort.Slice(members, func(i, j int) bool { return members[i].Slot.Index < members[j].Slot.Index })
		if len(members) == 0 {
			t.PendingTeams = t.PendingTeams[1:]
			continue
		}
		parts := []*battleserver.Participant{}
		protos := []protocol.BattleStartedPlayer{}
		for index, p := range members {
			v := p.Slot
			team := teamMap[v.Team]
			parts = append(parts, &battleserver.Participant{TestBalance: v.TestBalance, PlayerID: v.PlayerID, IsBot: v.IsBot, Index: index, Team: team, Name: v.Name, ModelID: v.CharacterModelID, HPLevel: v.HPLevel, DamageLevel: v.DamageLevel, SpeedLevel: v.SpeedLevel, UltID: v.UltID, DashID: v.DashID, StarPowerID: v.StarPowerID, AugmentIDs: append([]int32(nil), p.Augments...)})
			protos = append(protos, protocol.BattleStartedPlayer{PlayerID: v.PlayerID, Name: v.Name, ModelID: v.CharacterModelID, Index: int32(index), Team: int32(team), HPLevel: v.HPLevel, DamageLevel: v.DamageLevel, SpeedLevel: v.SpeedLevel, UltimateUpgradeID: v.UltID, DashID: v.DashID, AugmentIDs: append([]int32(nil), p.Augments...)})
		}
		id := newBattleID()
		sessions, host, port, err := s.battle.Allocate(&battleserver.AllocateRequest{ArenaRound: !lobby, BattleID: id, LocationID: loc.Id, ModeID: int32(mode.Id), Participants: parts})
		if err != "" {
			s.log.Warn("arena room awaits capacity", "arena", t.ID, "error", err)
			return
		}
		a := &store.Allocation{ArenaID: t.ID, ArenaLobby: lobby, LocationID: int32(loc.Id), ModeID: int32(mode.Id), StartedAt: time.Now().UnixMilli()}
		for i, p := range members {
			p.CurrentRoom = id
			slot := p.Slot
			slot.Index = i
			slot.Team = parts[i].Team
			slot.AugmentIDs = append([]int32(nil), p.Augments...)
			a.Players = append(a.Players, slot)
		}
		s.store.Meta.Allocations[id] = a
		t.Battles[id] = true
		t.PendingTeams = t.PendingTeams[1:]
		for _, p := range parts {
			if p.IsBot {
				continue
			}
			if c, ok := s.ConnByPlayer(p.PlayerID); ok {
				c.BattleID = id
				c.ArenaInfoBattle = ""
				s.sendBattleStarted(c, &protocol.BattleStartedMsg{ParticipantType: protocol.ParticipantPlayer, LocationName: loc.Name, OwnIndex: int32(p.Index), OwnTeam: int32(p.Team), Players: protos, Host: host, Port: int32(port), SessionID: sessions[p.Index]})
			}
		}
	}
}
func (s *Server) settleArenaRoom(r *battleserver.Result, a *store.Allocation) bool {
	t := s.store.Meta.Arenas[a.ArenaID]
	if t == nil {
		return false
	}
	if !t.Battles[r.BattleID] {
		return true
	}
	delete(t.Battles, r.BattleID)
	delete(s.store.Meta.Allocations, r.BattleID)
	s.store.Meta.Receipts["battle:"+r.BattleID] = time.Now().UnixMilli()
	s.mm.mu.Lock()
	delete(s.mm.pending, r.BattleID)
	delete(s.mm.rosters, r.BattleID)
	delete(s.mm.modes, r.BattleID)
	delete(s.mm.locations, r.BattleID)
	s.mm.mu.Unlock()
	if !a.ArenaLobby {
		t.TotalTicks += r.FinalTick
		teams := map[int]int{}
		record := store.BattleRecord{BattleID: r.BattleID, LocationID: a.LocationID, ModeID: a.ModeID, StartedAt: a.StartedAt, TotalTicks: r.FinalTick, TrophyDeltas: map[int64]int32{}, Ranks: map[int64]int32{}}
		for _, result := range r.Players {
			p := t.Players[result.PlayerID]
			if p == nil {
				continue
			}
			teams[result.Team] = p.Slot.Team
			p.Kills += result.Kills
			p.DamageDealt += result.DamageDealt
			p.DamageTaken += result.DamageTaken
			p.HealingDone += result.HealingDone
			p.Deaths += result.Deaths
			p.UltimateUses += result.UltimateUses
			p.DashUses += result.DashUses
			p.KillsWithUltimate += result.KillsWithUltimate
			slot := a.Players[result.Index]
			slot.Rank = 2
			if result.Team == r.WinnerTeam {
				slot.Rank = 1
			}
			if r.WinnerTeam < 0 {
				slot.Rank = -1
			}
			record.Participants = append(record.Participants, slot.PlayerID)
			record.Players = append(record.Players, slot)
			record.Ranks[slot.PlayerID] = slot.Rank
		}
		s.store.AddBattle(record)
		for local, original := range teams {
			if local != r.WinnerTeam {
				team := t.Teams[original]
				team.Lives = max(0, team.Lives-1)
				if team.Lives == 0 {
					team.EliminatedRound = t.Round
				}
			}
		}
	}
	for _, p := range a.Players {
		if c, ok := s.ConnByPlayer(p.PlayerID); ok && c.BattleID == r.BattleID {
			c.Send(protocol.SBattleEnded, protocol.EncodeBattleEnded(a.ModeID, 1, 0, true, nil))
		}
	}
	s.arenaNextPhase(t)
	return true
}
func (s *Server) finishArena(t *store.ArenaTournament, alive []int) {
	loc := s.catData.LocByID[t.LobbyLocationID]
	mode := s.catData.ModeByName[loc.Mode]
	a := &store.Allocation{LocationID: int32(loc.Id), ModeID: int32(mode.Id), StartedAt: t.CreatedAt, FinalRanks: map[int]int{}}
	r := &battleserver.Result{BattleID: t.ID, ModeID: int32(mode.Id), FinalTick: max(1, t.TotalTicks), WinnerTeam: -1}
	if len(alive) == 1 {
		r.WinnerTeam = alive[0]
	}
	for id, team := range t.Teams {
		rank := 1
		for other, v := range t.Teams {
			if other != id && (v.Lives > 0 && team.Lives <= 0 || v.EliminatedRound > team.EliminatedRound && team.Lives <= 0) {
				rank++
			}
		}
		a.FinalRanks[id] = rank
	}
	if len(alive) == 0 {
		for id, team := range t.Teams {
			if team.EliminatedRound == t.Round {
				a.FinalRanks[id] = -1
			}
		}
	}
	players := []*store.ArenaPlayer{}
	for _, p := range t.Players {
		players = append(players, p)
	}
	sort.Slice(players, func(i, j int) bool { return players[i].Slot.Index < players[j].Slot.Index })
	for _, p := range players {
		v := p.Slot
		a.Players = append(a.Players, v)
		r.Players = append(r.Players, &sim.Player{PlayerID: v.PlayerID, IsBot: v.IsBot, Abandoned: v.Abandoned, Index: v.Index, Team: v.Team, ModelID: v.CharacterModelID, Name: v.Name, Alive: v.Team == r.WinnerTeam, HeroType: "hero", Kills: p.Kills, DamageDealt: p.DamageDealt, DamageTaken: p.DamageTaken, HealingDone: p.HealingDone, Deaths: p.Deaths, UltimateUses: p.UltimateUses, DashUses: p.DashUses, KillsWithUltimate: p.KillsWithUltimate})
		if c, ok := s.ConnByPlayer(v.PlayerID); ok && !v.Abandoned && c.BattleID == p.CurrentRoom {
			c.BattleID = t.ID
		}
	}
	s.store.Meta.Allocations[t.ID] = a
	if s.mm.settleBattleResult(r) {
		delete(s.store.Meta.Arenas, t.ID)
	}
}
