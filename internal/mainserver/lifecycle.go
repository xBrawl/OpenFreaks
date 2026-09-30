package mainserver

import (
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/protocol"
	"time"
)

type battleLifecycle interface {
	Resume(string, int64) (*battleserver.BattleGrant, error)
	Leave(string, int64) bool
}

func (s *Server) resumeBattle(c *Conn) bool {
	backend, ok := s.battle.(battleLifecycle)
	if !ok {
		return true
	}
	for id, allocation := range s.store.Meta.Allocations {
		for _, p := range allocation.Players {
			if p.PlayerID != c.PlayerID || p.Abandoned || p.IsBot {
				continue
			}
			// Тестовая арена не кончается: если клиент из неё вылетел, при входе
			// его не затягиваем обратно (иначе вылет повторяется на каждом входе),
			// а освобождаем слот.
			if practiceBattle(id) {
				backend.Leave(id, c.PlayerID)
				continue
			}
			grant, err := backend.Resume(id, c.PlayerID)
			if err != nil {
				continue
			}
			message := &protocol.BattleStartedMsg{ParticipantType: protocol.ParticipantPlayer, LocationName: grant.LocationName, OwnIndex: int32(grant.OwnIndex), OwnTeam: int32(grant.OwnTeam), Host: grant.Host, Port: int32(grant.Port), SessionID: grant.SessionID}
			for _, player := range grant.Players {
				message.Players = append(message.Players, protocol.BattleStartedPlayer{PlayerID: player.PlayerID, Name: player.Name, ModelID: player.ModelID, Index: int32(player.Index), Team: int32(player.Team), HPLevel: player.HPLevel, DamageLevel: player.DamageLevel, SpeedLevel: player.SpeedLevel, UltimateUpgradeID: player.UltID, DashID: player.DashID, AugmentIDs: player.AugmentIDs})
			}
			c.BattleID = id
			return s.sendBattleStarted(c, message)
		}
	}
	for _, t := range s.store.Meta.Arenas {
		if p := t.Players[c.PlayerID]; p != nil && !p.Slot.Abandoned && t.Teams[p.Slot.Team].Lives > 0 {
			c.BattleID = p.CurrentRoom
			break
		}
	}
	return true
}
func (s *Server) leaveBattle(c *Conn) bool {
	for _, t := range s.store.Meta.Arenas {
		if p := t.Players[c.PlayerID]; p != nil && p.CurrentRoom == c.BattleID {
			p.Slot.Abandoned = true
			allLeft, hasHumans := true, false
			for _, mate := range t.Players {
				if mate.Slot.Team == p.Slot.Team && !mate.Slot.IsBot {
					hasHumans = true
					if !mate.Slot.Abandoned {
						allLeft = false
					}
				}
			}
			if hasHumans && allLeft {
				t.Teams[p.Slot.Team].Lives = 0
				t.Teams[p.Slot.Team].EliminatedRound = t.Round
			}
		}
	}
	if allocation := s.store.Meta.Allocations[c.BattleID]; allocation != nil {

		for i := range allocation.Players {
			if allocation.Players[i].PlayerID == c.PlayerID {
				allocation.Players[i].Abandoned = true
				if backend, ok := s.battle.(battleLifecycle); ok {
					allocation.Players[i].LeaveDelivered = backend.Leave(c.BattleID, c.PlayerID)
				}
			}
		}
	}
	c.BattleID = ""
	s.releaseTeamMember(c.PlayerID)
	return true
}

// The abandoned flag is durable, so a main restart also resumes failed leaves.
func (s *Server) retryBattleLeaves() {
	if time.Since(s.leaveCheckedAt) < 5*time.Second {
		return
	}
	s.leaveCheckedAt = time.Now()
	backend, ok := s.battle.(battleLifecycle)
	if !ok {
		return
	}
	for id, allocation := range s.store.Meta.Allocations {
		for i := range allocation.Players {
			p := &allocation.Players[i]
			if p.Abandoned && !p.IsBot && !p.LeaveDelivered {
				p.LeaveDelivered = backend.Leave(id, p.PlayerID)
			}
		}
	}
}
