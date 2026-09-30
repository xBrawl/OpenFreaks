package battleserver

import (
	"fmt"
	"strings"
)

type PracticeRequest struct {
	LocationID int
	Player     Participant
}

// Тестовая арена — личная комната: игрок один на один с шаром Карити.
// Раньше все входы делили одну вечную комнату на 11 команд, и новый игрок
// подсаживался в уже идущий бой с подменой героя; ни один другой режим так не
// стартует. Теперь комната собирается обычным Allocate, как мегабосс: тик 0,
// сервер ждёт подключения игрока, а когда игрок уходит, комната закрывается
// без итогов (Match.Run).
const practiceBossModel = "karity_boss_character"

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
	boss := s.cat.CharByName[practiceBossModel]
	if boss == nil {
		return nil, fmt.Errorf("practice boss missing")
	}
	// Прошлая комната игрока (вылет, повторный вход) закрывается: у каждого
	// входа своя свежая.
	s.leavePractice(p.PlayerID)
	p.Index, p.Team = 0, 0
	p.AugmentIDs = nil
	parts := []*Participant{&p, {PlayerID: -1001, IsBot: true, Index: 1, Team: 1, ModelID: int32(boss.Id), Name: "КАРИТИ", HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltID: -1, StarPowerID: -1}}
	id := fmt.Sprintf("practice:%d:%d:%x", loc.Id, p.PlayerID, NextSessionID())
	sessions, host, port, err := s.Allocate(&AllocateRequest{BattleID: id, LocationID: loc.Id, Participants: parts})
	if err != "" {
		return nil, fmt.Errorf("%s", err)
	}
	grant := &BattleGrant{BattleID: id, SessionID: sessions[0], LocationName: loc.Name, Host: host, Port: port}
	for _, part := range parts {
		copy := *part
		grant.Players = append(grant.Players, &copy)
	}
	return grant, nil
}

// leavePractice выводит игрока из всех его комнат тестовой арены.
func (s *Server) leavePractice(playerID int64) {
	s.mu.Lock()
	var ids []string
	for id, m := range s.matches {
		if !strings.HasPrefix(id, "practice:") {
			continue
		}
		for _, part := range m.parts {
			if part != nil && part.PlayerID == playerID && !part.IsBot {
				ids = append(ids, id)
				break
			}
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.Leave(id, playerID)
	}
}
