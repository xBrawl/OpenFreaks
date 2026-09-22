package battleserver

import (
	"fmt"
	"github.com/freaks/openfreaks/internal/sim"
	"time"
)

type PracticeRequest struct {
	LocationID int
	Player     Participant
}

const (
	practicePlayerSlots = 10
	practiceBossSlot    = practicePlayerSlots
	practiceSlots       = practicePlayerSlots + 1
	practiceBossModel   = "karity_boss_character"
)

func (s *Server) JoinPractice(req *PracticeRequest) (*BattleGrant, error) {
	if req == nil {
		return nil, fmt.Errorf("missing request")
	}
	loc := s.cat.LocByID[req.LocationID]
	p := req.Player
	if loc == nil || loc.Mode != "test_arena" || p.PlayerID <= 0 || p.IsBot || !p.TestBalance.Valid() || p.HPLevel < 1 || p.HPLevel > 16 || p.DamageLevel < 1 || p.DamageLevel > 16 || p.SpeedLevel < 1 || p.SpeedLevel > 16 {
		return nil, fmt.Errorf("invalid practice participant")
	}
	ch := s.cat.CharByID[int(p.ModelID)]
	if ch == nil || !ch.IsHero() {
		return nil, fmt.Errorf("invalid hero")
	}
	for _, ability := range []struct {
		table string
		id    int32
	}{{"dashes", p.DashID}, {"star_powers", p.StarPowerID}, {"ultimate_upgrades", p.UltID}} {
		if ability.id > 0 && s.cat.SpecByID(ability.table, int(ability.id)).String("target_character_name") != ch.Name {
			return nil, fmt.Errorf("invalid ability")
		}
	}
	for shard := 0; shard < 2; shard++ {
		id := fmt.Sprintf("practice:%d:%d", loc.Id, shard)
		s.mu.Lock()
		m := s.matches[id]
		s.mu.Unlock()
		if m == nil {
			parts := []*Participant{}
			for i := 0; i < practiceSlots; i++ {
				part := &Participant{PlayerID: int64(-1000 - i), IsBot: true, Index: i, Team: i, ModelID: p.ModelID, Name: "Свободно", HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltID: -1, StarPowerID: -1}
				if i == practiceBossSlot {
					if boss := s.cat.CharByName[practiceBossModel]; boss != nil {
						part.ModelID = int32(boss.Id)
						part.Name = "КАРИТИ"
					}
				}
				parts = append(parts, part)
			}
			if _, _, _, err := s.Allocate(&AllocateRequest{BattleID: id, LocationID: loc.Id, Participants: parts}); err != "" {
				return nil, fmt.Errorf("%s", err)
			}
		}
		s.mu.Lock()
		m = s.matches[id]
		if m == nil {
			s.mu.Unlock()
			continue
		}
		m.worldMu.Lock()
		m.peerMu.Lock()
		index := -1

		for i := 0; i < practicePlayerSlots; i++ {
			old := m.parts[i]
			if old.PlayerID == p.PlayerID && !m.departed[i] {
				index = i
				break
			}
			if index < 0 && (old.IsBot || m.departed[i]) {
				index = i
			}
		}
		if index < 0 {
			m.peerMu.Unlock()
			m.worldMu.Unlock()
			s.mu.Unlock()
			continue
		}
		old := m.World.Players[index]
		sid := m.SessionIDs[index]
		delete(s.grants, sid)
		if peer := s.peers[sid]; peer != nil {
			peer.Close()
			s.byAddr[peer.Addr.IP.String()]--
			delete(s.peers, sid)
		}
		delete(m.peers, index)
		delete(m.departed, index)
		m.generations[index]++
		p.Index = index
		p.Team = index
		p.AugmentIDs = nil
		fresh, err := sim.BuildWorld(s.cat, sim.BuildParams{LocationID: loc.Id, Players: []sim.BuildPlayer{{PlayerID: p.PlayerID, Index: index, Team: index, Name: p.Name, CharacterID: int(p.ModelID), HPLevel: int(p.HPLevel), DamageLevel: int(p.DamageLevel), SpeedLevel: int(p.SpeedLevel), DashID: int(p.DashID), UltUpgradeID: int(p.UltID), StarPowerID: int(p.StarPowerID), TestBalance: p.TestBalance}}})
		if err != nil {
			m.peerMu.Unlock()
			m.worldMu.Unlock()
			s.mu.Unlock()
			return nil, err
		}
		replacement := fresh.Players[0]
		replacement.World = m.World

		objectIndex := m.World.AllocCharacterIndex()
		if objectIndex == 0 {
			m.peerMu.Unlock()
			m.worldMu.Unlock()
			s.mu.Unlock()
			return nil, fmt.Errorf("practice_room_exhausted")
		}
		replacement.ObjectIndex = objectIndex
		m.World.Players[index] = replacement
		m.parts[index] = &p
		projectiles := m.World.Projectiles[:0]
		for _, v := range m.World.Projectiles {
			if v.Owner != old {
				projectiles = append(projectiles, v)
			}
		}
		m.World.Projectiles = projectiles
		areas := m.World.Areas[:0]
		for _, v := range m.World.Areas {
			if v.Owner != old {
				areas = append(areas, v)
			}
		}
		m.World.Areas = areas
		minions := m.World.Minions[:0]
		for _, v := range m.World.Minions {
			if v.Owner != old {
				minions = append(minions, v)
			}
		}
		m.World.Minions = minions
		m.World.TouchMinions()
		sid = NextSessionID()
		m.SessionIDs[index] = sid
		s.grants[sid] = &Grant{SessionID: sid, Index: index, ExpiresAt: time.Now().Add(24 * time.Hour)}
		grant := &BattleGrant{BattleID: id, SessionID: sid, LocationName: loc.Name, Host: s.cfg.AdvertisedHost, Port: s.cfg.AdvertisedPort, OwnIndex: index, OwnTeam: index}
		for i := 0; i < practiceSlots; i++ {
			copy := *m.parts[i]
			grant.Players = append(grant.Players, &copy)
		}
		m.peerMu.Unlock()
		m.worldMu.Unlock()
		s.mu.Unlock()
		return grant, nil
	}
	return nil, fmt.Errorf("practice capacity reached")
}
