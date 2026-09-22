package mainserver

import (
	"fmt"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/sim"
	"github.com/freaks/openfreaks/internal/store"
	"time"
)

func (s *Server) ensureMissions(a *store.Account) {
	meta := s.store.Meta.Player(a.PlayerID)
	now := time.Now().Unix()
	catalog := []content.Spec{}

	supported := map[string]bool{"matches_played": true, "wins": true, "wins_no_death": true, "kills": true, "kills_with_ultimate": true, "damage": true, "ultimate_usages": true, "dash_usages": true}
	for _, m := range s.catData.Definitions["missions"] {
		if supported[m.String("name")] {
			catalog = append(catalog, m)
		}
	}
	if len(catalog) == 0 {
		return
	}
	for key, m := range meta.Missions {
		if m.End <= now {
			delete(meta.Missions, key)
		}
	}
	for duration := 0; duration < 2; duration++ {
		span := int64(86400)
		if duration == 1 {
			span *= 7
		}
		start := now / span * span
		exists := false
		for _, m := range meta.Missions {
			if m.DurationType == duration && m.Start == start {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		for i := 0; i < min(3, len(catalog)); i++ {
			def := catalog[(int(start/span)+int(a.PlayerID%1000)+i*3+duration)%len(catalog)]
			difficulty := duration
			targets, rewards := def.At("progress").List(), def.At("rewards").List()
			if len(targets) == 0 || len(rewards) == 0 {
				continue
			}
			m := &store.MissionProgress{ModelID: def.ID, DurationType: duration, ModeID: -1, CharacterID: -1, Target: targets[min(difficulty, len(targets)-1)].Number(), Reward: rewards[min(difficulty, len(rewards)-1)].Number(), Start: start, End: start + span}
			if i == 2 && def.Bool("is_character_specific") {
				m.CharacterID = int(a.SelectedChar)
			}
			meta.Missions[fmt.Sprintf("%d:%d", m.ModelID, duration)] = m
		}
	}
}
func (s *Server) battleEconomy(p *sim.Player, rank int32, record store.BattleRecord) {
	a, ok := s.store.GetAccount(p.PlayerID)
	if !ok {
		return
	}
	season := s.ensurePass(a)
	key := "defeat"
	if isWinningRank(s.catData.ModeByID[int(record.ModeID)], rank) {
		key = "win"
	}
	points := season.At("battle_points").Int(key)
	if points > 0 {
		s.changeCurrency(a, 5, points, "battle_pass_points", "battle:"+record.BattleID)
	}
	s.ensureMissions(a)
	meta := s.store.Meta.Player(a.PlayerID)
	now := time.Now().Unix()
	for _, m := range meta.Missions {
		if m.Claimed || m.End <= now || m.Target <= 0 || m.CharacterID > 0 && m.CharacterID != int(p.ModelID) || m.ModeID > 0 && m.ModeID != int(record.ModeID) {
			continue
		}
		amount := 0
		switch s.catData.SpecByID("missions", m.ModelID).String("name") {
		case "matches_played":
			amount = 1
		case "wins":
			if isWinningRank(s.catData.ModeByID[int(record.ModeID)], rank) {
				amount = 1
			}
		case "wins_no_death":
			if isWinningRank(s.catData.ModeByID[int(record.ModeID)], rank) && p.Deaths == 0 {
				amount = 1
			}
		case "kills":
			amount = p.Kills
		case "kills_with_ultimate":
			amount = p.KillsWithUltimate
		case "damage":
			amount = p.DamageDealt
		case "ultimate_usages":
			amount = p.UltimateUses
		case "dash_usages":
			amount = p.DashUses
		}
		m.Progress = min(m.Target, m.Progress+max(0, amount))
		if m.Progress >= m.Target && s.changeCurrency(a, 5, m.Reward, "mission_reward", fmt.Sprintf("mission:%d:%d:%d", m.ModelID, m.DurationType, m.Start)) {
			m.Claimed = true
		}
	}
}
