package mainserver

import "time"

const existingMaxLevelGrantKey = "existing-characters-level-17-v1"

func (s *Server) grantExistingMaxCharacterLevels() (int, int, bool) {
	if s.catData.Progression.MaxPoints == 0 {
		return 0, 0, true
	}
	if s.catData.Progression.MaxPoints != 16 ||
		s.catData.Stats["hit_points"] == nil ||
		s.catData.Stats["damage"] == nil ||
		s.catData.Stats["speed"] == nil {
		return 0, 0, false
	}
	accounts, characters := 0, 0
	ok := s.runOperation(func() bool {
		if s.store.Meta.Receipts[existingMaxLevelGrantKey] != 0 {
			return true
		}
		for _, playerID := range s.store.AccountIDs() {
			account, found := s.store.GetAccount(playerID)
			if !found {
				continue
			}
			meta := s.store.Meta.Player(playerID)
			updated := false
			for charID, owned := range account.Characters {
				def := s.catData.CharByID[int(charID)]
				if owned == nil || def == nil || !def.IsHero() {
					continue
				}
				owned.ProgressionPoints = 16
				owned.HPLevel = int32(s.catData.Stats["hit_points"].MaxLevel)
				owned.DamageLevel = int32(s.catData.Stats["damage"].MaxLevel)
				owned.SpeedLevel = int32(s.catData.Stats["speed"].MaxLevel)
				bestStar, bestStarTier := int32(-1), -1
				for _, star := range s.catData.Definitions["star_powers"] {
					if star.String("target_character_name") != def.Name {
						continue
					}
					meta.OwnedStars[star.ID] = true
					if tier := star.Int("unlock_upgrade_tokens_spent"); tier > bestStarTier {
						bestStar, bestStarTier = int32(star.ID), tier
					}
				}
				if owned.StarPowerID <= 0 {
					owned.StarPowerID = bestStar
				}
				for _, dash := range s.catData.Definitions["dashes"] {
					if dash.String("target_character_name") != def.Name {
						continue
					}
					meta.OwnedDashes[dash.ID] = true
					if owned.DashID <= 0 {
						owned.DashID = int32(dash.ID)
					}
				}
				for _, upgrade := range s.catData.Definitions["ultimate_upgrades"] {
					if upgrade.String("target_character_name") != def.Name {
						continue
					}
					owned.IsUltUpgraded = true
					if owned.UltID <= 0 {
						owned.UltID = int32(upgrade.ID)
					}
				}
				characters++
				updated = true
			}
			if updated {
				accounts++
			}
		}
		s.store.Meta.Receipts[existingMaxLevelGrantKey] = time.Now().UnixMilli()
		return true
	})
	return accounts, characters, ok
}
