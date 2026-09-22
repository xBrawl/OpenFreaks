package mainserver

import (
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/store"
)

// Called once, in the same transaction as the immutable result receipt.
func (s *Server) recordBattleProgress(r *battleserver.Result, record store.BattleRecord) {
	for _, p := range r.Players {
		if p.IsBot || p.PlayerID <= 0 || p.Abandoned {
			continue
		}
		rank := record.Ranks[p.PlayerID]
		s.battleEconomy(p, rank, record)
		meta := s.store.Meta.Player(p.PlayerID)
		if meta.ResultRanks == nil {
			meta.ResultRanks = map[int]map[int]int{}
		}
		if meta.ResultRanks[int(record.ModeID)] == nil {
			meta.ResultRanks[int(record.ModeID)] = map[int]int{}
		}
		meta.ResultRanks[int(record.ModeID)][int(rank)]++
		m := meta.Mastery[int(p.ModelID)]
		if m == nil {
			m = &store.Mastery{}
			meta.Mastery[int(p.ModelID)] = m
		}
		role := ""
		if ch := s.catData.CharByID[int(p.ModelID)]; ch != nil {
			role = ch.Role
		}
		roleScore := 0
		switch role {
		case "attack":
			roleScore = p.Kills*10 + min(20, p.DamageDealt/500)
		case "defense":
			roleScore = max(0, 3-p.Deaths)*8 + min(15, p.DamageDealt/1000)
		case "support":
			roleScore = max(0, 3-p.Deaths)*8 + p.Kills*5
		}
		style := 0
		if p.Deaths == 0 {
			style += 20
		}
		if p.Kills >= 3 {
			style += 20
		}
		if p.DamageDealt >= 5000 {
			style += 20
		}
		points := 25 + min(120, p.DamageDealt/100) + p.Kills*15 + roleScore + style
		if isWinningRank(s.catData.ModeByID[int(record.ModeID)], rank) {
			points += 40
			m.Wins++
		}
		m.Points = min(2000000000, m.Points+points)
		m.Matches++
		m.Damage += int64(p.DamageDealt)
		m.RoleScore += roleScore
		m.StyleScore += style
		if al, ok := s.playerAlliance(p.PlayerID); ok {
			mates := 0
			roles := map[string]bool{}
			for _, other := range r.Players {
				if other.IsBot || other.PlayerID <= 0 || other.Team != p.Team {
					continue
				}
				a, ok := s.playerAlliance(other.PlayerID)
				if !ok || a.ID != al.ID {
					continue
				}
				mates++
				if c := s.catData.CharByID[int(other.ModelID)]; c != nil {
					roles[c.Role] = true
				}
			}
			points := 10
			if isWinningRank(s.catData.ModeByID[int(record.ModeID)], rank) {
				points += 15
			}
			if mates > 1 {
				points += 10
			}
			if len(roles) > 1 {
				points += 5
			}
			s.addReactor(al.ID, reactorSeason(), p.PlayerID, points, 1)
		}
	}
	for _, bond := range s.store.Meta.Bonds {
		if bond.Requester != 0 {
			continue
		}
		a, b := record.Ranks[bond.Low], record.Ranks[bond.High]
		if a == 0 || b == 0 {
			continue
		}
		bond.Matches++
		if isWinningRank(s.catData.ModeByID[int(record.ModeID)], a) && isWinningRank(s.catData.ModeByID[int(record.ModeID)], b) {
			teamA, teamB := -1, -2
			for _, p := range record.Players {
				if p.PlayerID == bond.Low {
					teamA = p.Team
				}
				if p.PlayerID == bond.High {
					teamB = p.Team
				}
			}
			if teamA == teamB {
				bond.Wins++
			}
		}
	}
}
