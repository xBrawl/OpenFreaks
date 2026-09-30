package mainserver

import (
	"sort"
	"sync"
	"time"

	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/sim"
	"github.com/freaks/openfreaks/internal/store"
)

type Matchmaking struct {
	done      chan struct{}
	stopOnce  sync.Once
	s         *Server
	mu        sync.Mutex
	queue     map[int64]*queueEntry
	rosters   map[string]map[int]int64
	modes     map[string]int32
	locations map[string]int32
	applied   map[string]bool // result idempotency (ResultId battle:tick)
	pending   map[string]time.Time
}

type queueEntry struct {
	partyID      int64
	assignedTeam int
	playerID     int64
	location     int32
	queuedAt     time.Time
	lastStatus   time.Time
	botsNow      bool // игрок сам попросил не ждать людей
}

func NewMatchmaking(s *Server) *Matchmaking {
	m := &Matchmaking{done: make(chan struct{}), s: s, queue: map[int64]*queueEntry{}, rosters: map[string]map[int]int64{}, modes: map[string]int32{}, locations: map[string]int32{}, applied: map[string]bool{}, pending: map[string]time.Time{}}
	go m.loop()
	return m
}

func (m *Matchmaking) botsEnabled() bool { return m.s.cfg.Testing.EnableBots }

func (m *Matchmaking) botFillDelay() time.Duration {
	d := m.s.cfg.Matchmaking.BotFillSeconds
	if d <= 0 {
		d = 60
	}
	return time.Duration(d) * time.Second
}

// secondsLeft is the countdown shown in the search screen: how long the queue
// still waits for real people before the match starts (with bots if needed).
func (m *Matchmaking) secondsLeft(location int32, oldest, now time.Time) int32 {
	wait := m.botFillDelay()
	if loc := m.s.catData.LocByID[int(location)]; loc != nil && loc.Mode == "mega_boss" {
		wait = time.Minute
	} else if !m.botsEnabled() {
		return int32(m.s.cfg.Matchmaking.StatusSecondsLeft)
	}
	return int32(max(0, int((wait-now.Sub(oldest)+time.Second-1)/time.Second)))
}

func (m *Matchmaking) Enqueue(pid int64, location int32) bool {
	location = m.s.dailyLocationID(location)
	if m.s.teamCapacity(location) == 0 {
		return false
	}
	members := []int64{pid}
	partyID := int64(0)
	if t := m.s.store.TeamForPlayer(pid); t != nil {
		if canonical := m.s.dailyLocationID(t.LocationID); canonical != 0 {
			t.LocationID = canonical
		}
		if t.OwnerID != pid || t.LocationID != location || len(t.Members) > m.s.teamCapacity(location) {
			return false
		}
		members = nil
		partyID = t.ID
		for id, member := range t.Members {
			if id != pid && member.State != 3 {
				return false
			}
			members = append(members, id)
		}
		sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	}
	for _, id := range members {
		c, ok := m.s.ConnByPlayer(id)
		if !ok || c.BattleID != "" {
			return false
		}
	}
	m.mu.Lock()
	for _, id := range members {
		if old := m.queue[id]; old != nil {
			m.mu.Unlock()
			return false
		}
	}
	now := time.Now()
	for _, id := range members {
		m.queue[id] = &queueEntry{playerID: id, location: location, partyID: partyID, queuedAt: now}
	}
	m.mu.Unlock()
	if t, ok := m.s.store.GetTeam(partyID); ok {
		for _, member := range t.Members {
			member.State = 4
		}
		m.s.broadcastTeamUpdate(t.ID)
	}
	for _, id := range members {
		if c, ok := m.s.ConnByPlayer(id); ok {
			seconds := m.secondsLeft(location, now, now)
			c.Send(protocol.SMatchmaking, protocol.EncodeMatchmaking(seconds, int32(len(members)), location))
		}
	}
	return true
}

// BotsNow: игрок (или хозяин команды) не хочет ждать людей — матч собирается
// сразу, свободные места займут боты.
func (m *Matchmaking) BotsNow(pid int64) {
	if !m.botsEnabled() {
		return
	}
	m.mu.Lock()
	e := m.queue[pid]
	if e == nil {
		m.mu.Unlock()
		return
	}
	if loc := m.s.catData.LocByID[int(e.location)]; loc == nil {
		m.mu.Unlock()
		return
	}
	if e.partyID > 0 {
		if t, ok := m.s.store.GetTeam(e.partyID); !ok || t.OwnerID != pid {
			m.mu.Unlock()
			return
		}
	}
	for _, other := range m.queue {
		if other == e || e.partyID > 0 && other.partyID == e.partyID {
			other.botsNow = true
		}
	}
	m.mu.Unlock()
	m.tryAllocate(pid)
}

// Queued сообщает, стоит ли игрок в очереди поиска.
func (m *Matchmaking) Queued(pid int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queue[pid] != nil
}

func (m *Matchmaking) Cancel(pid int64) {
	m.mu.Lock()
	entry := m.queue[pid]
	cancelled := []int64{}
	for id, e := range m.queue {
		if id == pid || entry != nil && entry.partyID > 0 && e.partyID == entry.partyID {
			delete(m.queue, id)
			cancelled = append(cancelled, id)
		}
	}
	m.mu.Unlock()
	for _, id := range cancelled {
		if c, ok := m.s.ConnByPlayer(id); ok {
			c.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
		}
	}
	if entry != nil && entry.partyID > 0 {
		if t, ok := m.s.store.GetTeam(entry.partyID); ok {
			for _, member := range t.Members {
				if member.State == 4 {
					member.State = 1
				}
			}
			m.s.broadcastTeamUpdate(t.ID)
		}
	}
}

func (m *Matchmaking) loop() {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
			m.tick()
		}
	}
}
func (m *Matchmaking) Stop() {
	m.stopOnce.Do(func() {
		if m.done != nil {
			close(m.done)
		}
	})
}

func (m *Matchmaking) tick() {

	if m.quiet() {
		return
	}
	m.s.runOperation(func() bool { m.tickLocked(); return true })
}

func (m *Matchmaking) quiet() bool {
	m.mu.Lock()
	busy := len(m.queue) > 0 || len(m.pending) > 0
	m.mu.Unlock()
	if busy {
		return false
	}
	defer m.s.store.LockState()()
	return len(m.s.store.Meta.Arenas) == 0 && len(m.s.store.Meta.Allocations) == 0
}
func (m *Matchmaking) tickLocked() {
	now := time.Now()
	m.s.tickArenas()
	// Keep enough time for a split Battle outbox to retry after a Main restart.
	// Old allocations cannot reserve a player forever when a room was lost.
	for id, a := range m.s.store.Meta.Allocations {
		if now.Unix()-unixSeconds(a.StartedAt) > 7*24*3600 {
			delete(m.s.store.Meta.Allocations, id)
		}
	}
	m.mu.Lock()

	if m.botsEnabled() {
		var oldest *queueEntry
		for _, e := range m.queue {
			if oldest == nil || e.queuedAt.Before(oldest.queuedAt) {
				oldest = e
			}
		}
		if oldest != nil && now.Sub(oldest.queuedAt) >= m.botFillDelay() {
			m.mu.Unlock()
			m.tryAllocate(oldest.playerID)
			m.mu.Lock()
		}
	}
	interval := m.s.cfg.Matchmaking.StatusIntervalMs
	if interval <= 0 {
		interval = 1000
	}

	for _, e := range m.queue {
		if now.Sub(e.lastStatus) >= time.Duration(interval)*time.Millisecond {
			e.lastStatus = now
			pid, loc, count := e.playerID, e.location, 0
			for _, other := range m.queue {
				if other.location == loc {
					count++
				}
			}
			oldest := e.queuedAt
			for _, other := range m.queue {
				if other.location == loc && other.queuedAt.Before(oldest) {
					oldest = other.queuedAt
				}
			}
			seconds := m.secondsLeft(loc, oldest, now)
			m.mu.Unlock()
			if conn, ok := m.s.ConnByPlayer(pid); ok {
				conn.Send(protocol.SMatchmaking, protocol.EncodeMatchmaking(seconds, int32(count), loc))
			}
			m.mu.Lock()
		}
	}

	var allocPIDs []int64
	if len(m.queue) > 0 {
		oldestByLocation := map[int32]*queueEntry{}
		for _, e := range m.queue {
			if prev, ok := oldestByLocation[e.location]; !ok || e.queuedAt.Before(prev.queuedAt) {
				oldestByLocation[e.location] = e
			}
		}
		for _, e := range oldestByLocation {
			allocPIDs = append(allocPIDs, e.playerID)
		}

		sort.Slice(allocPIDs, func(i, j int) bool {
			return m.queue[allocPIDs[i]].queuedAt.Before(m.queue[allocPIDs[j]].queuedAt)
		})
		if len(allocPIDs) > maxAllocationsPerTick {
			allocPIDs = allocPIDs[:maxAllocationsPerTick]
		}
	}

	timeout := m.s.cfg.Matchmaking.AllocationTimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	expired := map[string][]int64{}
	for battleID, deadline := range m.pending {
		if now.After(deadline) {
			for _, pid := range m.rosters[battleID] {
				expired[battleID] = append(expired[battleID], pid)
			}
			delete(m.pending, battleID)
			delete(m.rosters, battleID)
			delete(m.modes, battleID)
			delete(m.locations, battleID)
			m.mu.Unlock()
			m.s.log.Warn("allocated battle timed out with no result", "battle", battleID)
			m.mu.Lock()
		}
	}
	m.mu.Unlock()
	// Бой без результата не должен навсегда держать игроков «в бою»: иначе их
	// нельзя позвать в команду, а их команда не может искать матч.
	for battleID, pids := range expired {
		for _, pid := range pids {
			if c, ok := m.s.ConnByPlayer(pid); ok && c.BattleID == battleID {
				c.BattleID = ""
				m.s.releaseTeamMember(pid)
			}
		}
	}
	for _, pid := range allocPIDs {
		m.tryAllocate(pid)
	}
}

const maxAllocationsPerTick = 4

func (m *Matchmaking) tryAllocate(primaryPID int64) {
	m.mu.Lock()
	primary, ok := m.queue[primaryPID]
	if !ok {
		m.mu.Unlock()
		return
	}
	fillWithBots := m.botsEnabled() && (primary.botsNow || time.Since(primary.queuedAt) >= m.botFillDelay())
	minP := m.s.cfg.Matchmaking.MinPlayers
	if minP < 1 {
		minP = 1
	}

	locID := int(primary.location)
	loc := m.s.catData.LocByID[locID]
	if loc == nil {
		m.mu.Unlock()
		return
	}
	mode := m.s.catData.ModeByName[loc.Mode]
	if mode == nil {
		m.mu.Unlock()
		return
	}
	entries := []*queueEntry{}
	for _, e := range m.queue {
		if e.location == primary.location {
			entries = append(entries, e)
		}
	}
	if mode.Name == "mega_boss" {
		group := planQueue(entries, 1, 6, 6)
		if !(primary.botsNow && m.botsEnabled()) && time.Since(primary.queuedAt) < time.Minute {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		if len(group) < 3 && !m.botsEnabled() {
			for _, e := range group {
				m.Cancel(e.playerID)
			}
			return
		}
		m.allocate(locID, group, len(group) < 3 && m.botsEnabled())
		return
	}
	if len(m.queue) < minP && !fillWithBots {
		m.mu.Unlock()
		return
	}
	// Заражение: все начинают выжившими в одной команде (команду 1 даёт бой).
	teams := max(1, mode.TeamCount)
	if mode.Name == "infection" {
		teams = 1
	}
	group := planQueue(entries, teams, max(1, mode.TeamPlayerCount), mode.MaxPlayers)
	if m.botsEnabled() && !fillWithBots && len(group) < mode.MaxPlayers {
		m.mu.Unlock()
		return
	}
	if len(group) < minP && !fillWithBots {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.allocate(locID, group, fillWithBots)
}

func (m *Matchmaking) allocate(locID int, group []*queueEntry, fillWithBots bool) {
	cat := m.s.catData
	loc, ok := cat.LocByID[locID]
	if !ok || loc.Hidden {

		m.dequeueGroup(group)
		for _, e := range group {
			if conn, ok := m.s.ConnByPlayer(e.playerID); ok {
				conn.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
			}
		}
		return
	}
	mode, ok := cat.ModeByName[loc.Mode]
	if !ok {
		m.dequeueGroup(group)
		for _, e := range group {
			if conn, ok := m.s.ConnByPlayer(e.playerID); ok {
				conn.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
			}
		}
		return
	}
	battleID := newBattleID()
	parts := make([]*battleserver.Participant, 0, len(group)+4)
	protos := make([]protocol.BattleStartedPlayer, 0, len(group)+4)
	teamSizes := make([]int, max1int(mode.TeamCount))
	for i, e := range group {
		acc, ok := m.s.store.GetAccount(e.playerID)
		name := "Player"
		var modelID int32 = 1
		var hp, dmg, spd int32 = 1, 1, 1
		var ult, dash, star int32 = -1, -1, -1
		if ok {
			name = acc.Name
			modelID = acc.SelectedChar
			if ch, has := acc.Characters[acc.SelectedChar]; has {
				hp, dmg, spd = ch.HPLevel, ch.DamageLevel, ch.SpeedLevel
				ult = ch.UltID
				if ch.IsUltUpgraded && ult < 0 {
					ult = firstUltUpgradeFor(cat, acc.SelectedChar)
				}
				dash = ch.DashID
				star = ch.StarPowerID
			}
		}
		team := e.assignedTeam
		teamSizes[team]++
		balance := m.s.balanceFor(e.playerID, modelID)
		if mode.Name == "mega_boss" {
			balance = content.TestBalance{}
		}
		// Кошмар: в бою все — Исследователи без гаджетов и прокачки (sim/nightmare.go).
		// Снаряжение в составе обнуляем, иначе клиент ждал бы в кадре рывок героя.
		if mode.Name == "nightmare" {
			balance = content.TestBalance{}
			ult, dash, star = -1, -1, -1
			hp, dmg, spd = 1, 1, 1
		}
		parts = append(parts, &battleserver.Participant{
			TestBalance: balance, PlayerID: e.playerID, Index: i, Team: team, Name: name, ModelID: modelID,
			HPLevel: hp, DamageLevel: dmg, SpeedLevel: spd, UltID: ult, DashID: dash, StarPowerID: star,
		})
		protos = append(protos, protocol.BattleStartedPlayer{
			PlayerID: e.playerID, Name: name, ModelID: modelID, Index: int32(i), Team: int32(team),
			HPLevel: hp, DamageLevel: dmg, SpeedLevel: spd,
			UltimateUpgradeID: ult, DashID: dash,
		})
	}
	if fillWithBots {
		m.appendBots(&parts, &protos, teamSizes, mode, cat)
	}
	if mode.Name == "mega_boss" {
		boss := megaBossForDay(cat, time.Now())
		if boss == nil || len(parts) < 3 || len(parts) > 6 {
			return
		}
		index := len(parts)
		parts = append(parts, &battleserver.Participant{PlayerID: int64(-1000 - index), Index: index, Team: 1, IsBot: true, Name: "КАРИТИ", ModelID: int32(boss.Id), HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, UltID: -1, DashID: -1, StarPowerID: -1})
		protos = append(protos, protocol.BattleStartedPlayer{Name: "КАРИТИ", ModelID: int32(boss.Id), Index: int32(index), Team: 1, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, UltimateUpgradeID: -1, DashID: -1})
	}
	// Party-on-single-team guard (C# parity): keep the party queued until
	// another team is present or bot fill completes the roster.
	if mode.TeamCount > 1 && !fillWithBots {
		seen := map[int]bool{}
		for _, p := range parts {
			seen[p.Team] = true
		}
		if len(seen) < 2 {
			m.s.log.Info("matchmaking waiting for opposition", "loc", loc.Id, "players", len(parts))
			return
		}
	}
	if len(parts) == 0 {
		return
	}
	sessions, host, port, allocErr := m.s.battle.Allocate(&battleserver.AllocateRequest{
		BattleID: battleID, LocationID: loc.Id, LocationName: loc.Name,
		ModeID: int32(mode.Id), Participants: parts,
	})
	if allocErr != "" {
		m.s.log.Warn("battle allocate failed", "err", allocErr)
		m.dequeueGroup(group)
		for _, e := range group {
			if conn, ok := m.s.ConnByPlayer(e.playerID); ok {
				conn.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
			}
		}
		return
	}

	m.dequeueGroup(group)
	if host == "" {
		host = m.s.advHost
	}
	if port == 0 {
		port = m.s.advPort
	}
	roster := map[int]int64{}
	for _, p := range parts {
		roster[p.Index] = p.PlayerID
	}
	timeout := m.s.cfg.Matchmaking.AllocationTimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	m.mu.Lock()
	m.rosters[battleID] = roster
	m.modes[battleID] = int32(mode.Id)
	m.locations[battleID] = int32(loc.Id)

	grace := max(120, m.s.cfg.Matchmaking.BattleResultGraceSeconds)
	m.pending[battleID] = time.Now().Add(time.Duration(mode.DurationTicks)*50*time.Millisecond + time.Duration(timeout+grace)*time.Second)
	m.mu.Unlock()
	allocation := &store.Allocation{LocationID: int32(loc.Id), ModeID: int32(mode.Id), StartedAt: time.Now().UnixMilli()}
	for _, p := range parts {
		allocation.Players = append(allocation.Players, store.BattleParticipant{TestBalance: p.TestBalance, PlayerID: p.PlayerID, IsBot: p.IsBot, Name: p.Name, Index: p.Index, Team: p.Team, CharacterModelID: p.ModelID, HPLevel: p.HPLevel, DamageLevel: p.DamageLevel, SpeedLevel: p.SpeedLevel, StarPowerID: p.StarPowerID, DashID: p.DashID, UltID: p.UltID, Trophies: m.trophiesOf(p.PlayerID)})
	}
	m.s.store.Meta.Allocations[battleID] = allocation
	if mode.Arena && len(loc.RoundLocations) > 0 {
		m.s.beginArena(battleID, allocation)
	}
	vc := m.s.cfg.Voice
	for _, p := range parts {
		if p.IsBot {
			continue
		}
		conn, ok := m.s.ConnByPlayer(p.PlayerID)
		if !ok {
			continue
		}
		conn.BattleID = battleID
		if t := m.s.store.TeamForPlayer(p.PlayerID); t != nil {
			t.Members[p.PlayerID].State = 5
		}
		msg := &protocol.BattleStartedMsg{
			ParticipantType: protocol.ParticipantPlayer,
			LocationName:    loc.Name,
			OwnIndex:        int32(p.Index), OwnTeam: int32(p.Team),
			Players: protos, Host: host, Port: int32(port),
			SessionID: sessions[p.Index],
			VoiceHost: vc.Host, VoicePort: int32(vc.Port), VoiceToken: vc.Token,
		}
		m.s.sendBattleStarted(conn, msg)
	}
}

func (m *Matchmaking) dequeueGroup(group []*queueEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range group {
		delete(m.queue, e.playerID)
	}
}

func (m *Matchmaking) appendBots(parts *[]*battleserver.Participant, protos *[]protocol.BattleStartedPlayer, teamSizes []int, mode *content.GameMode, cat *content.CatalogData) {
	heroes := cat.Heroes()
	capacity := mode.MaxPlayers
	if mode.Name == "mega_boss" {
		capacity = 3
	}
	if len(heroes) == 0 || len(*parts) >= capacity {
		return
	}
	taken := make([]string, 0, len(*parts))
	for _, p := range *parts {
		taken = append(taken, p.Name)
	}
	names := newBotNamePool(taken)
	for index := len(*parts); index < capacity; index++ {
		ch := heroes[index%len(heroes)]
		team := 0
		for t := 1; mode.Name != "mega_boss" && mode.Name != "infection" && t < len(teamSizes); t++ {
			if teamSizes[t] < teamSizes[team] {
				team = t
			}
		}
		teamSizes[team]++
		name := names.take(index)
		var hp, damage, speed int32
		if mode.Name == "mega_boss" && len(*parts) > 0 {
			hp, damage, speed = (*parts)[0].HPLevel, (*parts)[0].DamageLevel, (*parts)[0].SpeedLevel
		}
		*parts = append(*parts, &battleserver.Participant{
			PlayerID: int64(-1000 - index), IsBot: true, Index: index, Team: team,
			Name: name, ModelID: int32(ch.Id),
			HPLevel: hp, DamageLevel: damage, SpeedLevel: speed,
			UltID: -1, DashID: -1, StarPowerID: -1,
		})
		*protos = append(*protos, protocol.BattleStartedPlayer{
			Name: name, ModelID: int32(ch.Id),
			Index: int32(index), Team: int32(team),
			HPLevel: hp, DamageLevel: damage, SpeedLevel: speed,
			UltimateUpgradeID: -1, DashID: -1,
		})
	}
}

func firstUltUpgradeFor(cat *content.CatalogData, charID int32) int32 {
	for _, u := range cat.UltUpgrades {
		if u == nil {
			continue
		}
		if ch, ok := cat.CharByName[u.TargetChar]; ok && int32(ch.Id) == charID {
			return int32(u.Id)
		}
	}
	return -1
}

func max1int(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

func (m *Matchmaking) OnBattleResult(r *battleserver.Result) bool {
	return m.s.runOperation(func() bool { return m.settleBattleResult(r) })
}

// releaseAbortedAllocation закрывает матч без начислений: квитанция ставится,
// чтобы повтор доставки не пытался снова, allocation и очереди чистятся.
func (m *Matchmaking) releaseAbortedAllocation(r *battleserver.Result) bool {
	m.s.log.Warn("battle aborted without result", "battle", r.BattleID, "reason", r.AbortReason)
	m.s.store.Meta.Receipts["battle:"+r.BattleID] = time.Now().UnixMilli()
	delete(m.s.store.Meta.Allocations, r.BattleID)
	m.mu.Lock()
	delete(m.rosters, r.BattleID)
	delete(m.modes, r.BattleID)
	delete(m.locations, r.BattleID)
	delete(m.pending, r.BattleID)
	m.mu.Unlock()
	return true
}

func (m *Matchmaking) settleBattleResult(r *battleserver.Result) bool {
	if r == nil {
		return false
	}
	if m.s.store.Meta.Receipts["battle:"+r.BattleID] > 0 {
		return true
	}
	m.s.applyCheatReports(r)
	allocation := m.s.store.Meta.Allocations[r.BattleID]
	if allocation == nil {
		m.s.log.Warn("ignored result without durable allocation", "battle", r.BattleID)
		return false
	}

	// Тестовая арена — тренировка: ни кубков, ни записи в историю боёв.
	if r.Aborted && r.FinalTick < 1 || practiceBattle(r.BattleID) {
		return m.releaseAbortedAllocation(r)
	}
	if r.ModeID != allocation.ModeID || len(r.Players) != len(allocation.Players) || r.FinalTick < 1 {
		return false
	}
	byIndex := map[int]*sim.Player{}
	for _, p := range r.Players {
		if p == nil || byIndex[p.Index] != nil || p.Kills < 0 || p.DamageDealt < 0 || p.Deaths < 0 {
			return false
		}
		byIndex[p.Index] = p
	}
	for _, slot := range allocation.Players {
		p := byIndex[slot.Index]
		if p == nil || p.PlayerID != slot.PlayerID || p.ResultTeam() != slot.Team || p.ResultModel() != slot.CharacterModelID {
			return false
		}
	}
	if allocation.ArenaID != "" {
		return m.s.settleArenaRoom(r, allocation)
	}
	mode := m.s.catData.ModeByID[int(allocation.ModeID)]
	ranks := resultRanks(r, mode)
	if len(allocation.FinalRanks) > 0 {
		ranks = allocation.FinalRanks
	}
	record := store.BattleRecord{BattleID: r.BattleID, LocationID: allocation.LocationID, ModeID: allocation.ModeID, StartedAt: allocation.StartedAt, TotalTicks: r.FinalTick, TrophyDeltas: map[int64]int32{}, Ranks: map[int64]int32{}}
	for _, h := range r.Highlights {
		record.Highlights = append(record.Highlights, store.BattleHighlight{Tick: h.Tick, Kind: int32(h.Kind), Player: int32(h.Player), Count: int32(h.Count)})
	}
	for _, slot := range allocation.Players {
		rank := ranks[slot.Team]
		// Кошмар и Заражение делят победу не по командам: место — у каждого своё.
		if own, ok := r.PlayerRanks[slot.Index]; ok {
			rank = own
		}
		slot.Rank = int32(rank)
		slot.ModeRole = int32(r.PlayerRoles[slot.Index])
		if slot.Abandoned {
			byIndex[slot.Index].Abandoned = true
		}
		record.Participants = append(record.Participants, slot.PlayerID)
		if a, ok := m.s.store.GetAccount(slot.PlayerID); ok && !slot.IsBot {
			delta := int32(-5)
			if !slot.Abandoned && !byIndex[slot.Index].Abandoned && isWinningRank(mode, int32(rank)) {
				delta = 8
			} else if rank < 0 {
				delta = 0
			}
			if mode != nil && mode.Name == "mega_boss" {
				delta = 0
			}
			before := a.Trophies
			a.Trophies = max(0, a.Trophies+delta)
			delta = a.Trophies - before
			if ch := a.Characters[slot.CharacterModelID]; ch != nil {
				ch.Trophies = max(0, ch.Trophies+delta)
			}
			record.TrophyDeltas[a.PlayerID] = delta
			record.Ranks[a.PlayerID] = int32(rank)
			slot.Trophies = a.Trophies
		}
		record.Players = append(record.Players, slot)
	}
	m.s.recordBattleProgress(r, record)
	if mode != nil && mode.Name == "mega_boss" && r.WinnerTeam == 0 {
		for _, slot := range allocation.Players {
			if slot.IsBot || slot.Team != 0 || slot.Abandoned || byIndex[slot.Index].Abandoned {
				continue
			}
			if a, ok := m.s.store.GetAccount(slot.PlayerID); ok {
				m.s.changeCurrency(a, 1, 500, "mega_boss_win", "battle:"+r.BattleID)
				m.s.changeCurrency(a, 4, 20, "mega_boss_win", "battle:"+r.BattleID)
			}
		}
	}
	m.s.store.Meta.Receipts["battle:"+r.BattleID] = time.Now().UnixMilli()
	delete(m.s.store.Meta.Allocations, r.BattleID)
	m.s.store.AddBattle(record)
	// Задержка доставки результата: от последнего тика боя до записи здесь.
	// Тестеры жаловались, что экран итогов появляется долго.
	m.s.log.Info("battle settled", "battle", r.BattleID, "mode", allocation.ModeID, "ticks", r.FinalTick,
		"deliveryMs", time.Now().UnixMilli()-allocation.StartedAt-r.DurationMs)
	m.mu.Lock()
	delete(m.rosters, r.BattleID)
	delete(m.modes, r.BattleID)
	delete(m.locations, r.BattleID)
	delete(m.pending, r.BattleID)
	m.mu.Unlock()
	for _, own := range record.Players {
		if own.IsBot || own.PlayerID <= 0 {
			continue
		}
		conn, ok := m.s.ConnByPlayer(own.PlayerID)
		// Вышедший раньше конца тоже возвращается в лобби команды.
		if ok && conn.BattleID == "" {
			m.s.releaseTeamMember(own.PlayerID)
		}
		if !ok || conn.BattleID != r.BattleID {
			continue
		}
		conn.BattleID = ""
		m.s.releaseTeamMember(own.PlayerID)
		players := []protocol.BattleEndedPlayer{}
		for _, p := range record.Players {
			stats := byIndex[p.Index]
			// 30–32 — место, роль в режиме и победа игрока: в Кошмаре и Заражении
			// победа своя у каждого, а экран итогов больших боёв показывает роли.
			// Старые клиенты незнакомые ключи статистики пропускают.
			players = append(players, protocol.BattleEndedPlayer{CharacterModelID: p.CharacterModelID, Name: p.Name, Trophies: p.Trophies, PowerLevel: max(1, p.HPLevel+p.DamageLevel+p.SpeedLevel-2), IsOwnPlayer: p.PlayerID == own.PlayerID, IsOwnTeam: p.Team == own.Team, Statistics: [][2]int32{{1, int32(stats.Kills)}, {2, int32(stats.DamageDealt)}, {3, int32(stats.DamageTaken)}, {4, int32(stats.HealingDone)}, {5, int32(stats.Deaths)}, {8, int32(stats.UltimateUses)},
				{30, p.Rank}, {31, p.ModeRole}, {32, int32(boolInt32(isWinningRank(mode, p.Rank)))}}})
		}
		if !conn.Send(protocol.SBattleEnded, protocol.EncodeBattleEnded(allocation.ModeID, own.Rank, record.TrophyDeltas[own.PlayerID], false, players)) {
			m.s.log.Warn("battle result could not be sent to client", "battle", r.BattleID, "player", own.PlayerID)
		}
	}
	return true
}
func boolInt32(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

func isWinningRank(mode *content.GameMode, rank int32) bool {
	if rank <= 0 {
		return false
	}
	if mode != nil && (mode.Showdown || mode.Ascent) && mode.TeamCount > 2 {
		return int(rank) <= max(1, mode.TeamCount/2)
	}
	return rank == 1
}
func resultRanks(r *battleserver.Result, mode *content.GameMode) map[int]int {
	result := map[int]int{}
	for _, p := range r.Players {
		result[p.Team] = 2
		if p.Team == r.WinnerTeam {
			result[p.Team] = 1
		}
		if r.WinnerTeam < 0 {
			result[p.Team] = -1
		}
	}
	if mode != nil && mode.Ascent {
		for team := range result {
			points := 0
			if team < len(r.TeamPoints) {
				points = r.TeamPoints[team]
			}
			rank := 1
			for other := range result {
				if other < len(r.TeamPoints) && r.TeamPoints[other] > points {
					rank++
				}
			}
			if r.WinnerTeam < 0 && points == 0 {
				rank = -1
			}
			result[team] = rank
		}
		return result
	}
	if mode == nil || !mode.Showdown || len(result) <= 2 {
		return result
	}
	teams := []int{}
	alive := map[int]bool{}
	last := map[int]int32{}
	for team := range result {
		teams = append(teams, team)
	}
	for _, p := range r.Players {
		if p.Alive && !p.Abandoned {
			alive[p.Team] = true
		}
		last[p.Team] = max(last[p.Team], p.DeathTick)
	}
	sort.Slice(teams, func(i, j int) bool {
		a, b := teams[i], teams[j]
		if a == r.WinnerTeam {
			return true
		}
		if b == r.WinnerTeam {
			return false
		}
		if alive[a] != alive[b] {
			return alive[a]
		}
		if last[a] != last[b] {
			return last[a] > last[b]
		}
		return a < b
	})
	for i, team := range teams {
		result[team] = i + 1
		if i > 0 && alive[team] == alive[teams[i-1]] && last[team] == last[teams[i-1]] && team != r.WinnerTeam && teams[i-1] != r.WinnerTeam {
			result[team] = result[teams[i-1]]
		}
	}
	return result
}

func (m *Matchmaking) trophiesOf(pid int64) int32 {
	if acc, ok := m.s.store.GetAccount(pid); ok {
		return acc.Trophies
	}
	return 0
}
