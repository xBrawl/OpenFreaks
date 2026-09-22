package mainserver

import (
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
	"strings"
	"time"
)

type practiceAllocator interface {
	JoinPractice(*battleserver.PracticeRequest) (*battleserver.BattleGrant, error)
}

func (s *Server) joinPractice(c *Conn, location int32) bool {
	if c.BattleID != "" {
		return c.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
	}
	backend, ok := s.battle.(practiceAllocator)
	if !ok {
		return c.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
	}
	acc, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	ch := acc.Characters[acc.SelectedChar]
	if ch == nil {
		return false
	}
	s.mm.Cancel(c.PlayerID)
	req := &battleserver.PracticeRequest{LocationID: int(location), Player: battleserver.Participant{PlayerID: c.PlayerID, Name: acc.Name, ModelID: acc.SelectedChar, HPLevel: ch.HPLevel, DamageLevel: ch.DamageLevel, SpeedLevel: ch.SpeedLevel, DashID: ch.DashID, UltID: ch.UltID, StarPowerID: ch.StarPowerID, TestBalance: s.balanceFor(c.PlayerID, acc.SelectedChar)}}
	grant, err := backend.JoinPractice(req)
	if err != nil {
		return c.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
	}
	loc := s.catData.LocByID[int(location)]
	mode := s.catData.ModeByName[loc.Mode]
	a := &store.Allocation{LocationID: location, ModeID: int32(mode.Id), StartedAt: time.Now().UnixMilli()}
	message := &protocol.BattleStartedMsg{ParticipantType: protocol.ParticipantPlayer, LocationName: grant.LocationName, OwnIndex: int32(grant.OwnIndex), OwnTeam: int32(grant.OwnTeam), Host: grant.Host, Port: int32(grant.Port), SessionID: grant.SessionID}
	for _, p := range grant.Players {
		a.Players = append(a.Players, store.BattleParticipant{PlayerID: p.PlayerID, Name: p.Name, Index: p.Index, Team: p.Team, IsBot: p.IsBot, CharacterModelID: p.ModelID, HPLevel: p.HPLevel, DamageLevel: p.DamageLevel, SpeedLevel: p.SpeedLevel, DashID: p.DashID, UltID: p.UltID, StarPowerID: p.StarPowerID, TestBalance: p.TestBalance})
		item := protocol.BattleStartedPlayer{Name: p.Name, ModelID: p.ModelID, Index: int32(p.Index), Team: int32(p.Team), HPLevel: p.HPLevel, DamageLevel: p.DamageLevel, SpeedLevel: p.SpeedLevel, DashID: p.DashID, UltimateUpgradeID: p.UltID}
		message.Players = append(message.Players, item)
		if p.PlayerID == c.PlayerID {
			for _, peer := range grant.Players {
				if peer.PlayerID == c.PlayerID || peer.IsBot {
					continue
				}
				if connection, ok := s.ConnByPlayer(peer.PlayerID); ok && connection.BattleID == grant.BattleID {
					w := protocol.NewWriter()
					item.Encode(w)
					connection.Send(protocol.SBattlePlayerJoined, w.Bytes())
				}
			}
		}
	}
	s.store.Meta.Allocations[grant.BattleID] = a
	c.BattleID = grant.BattleID
	return c.Send(protocol.SBattleStarted, message.Encode())
}
func practiceBattle(id string) bool { return strings.HasPrefix(id, "practice:") }
