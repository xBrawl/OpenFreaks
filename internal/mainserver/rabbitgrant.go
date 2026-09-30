package mainserver

import "time"

const existingRabbitGrantKey = "existing-rabbit-level-17-all-abilities-v1"

func (s *Server) grantExistingRabbitMaxLevels() (int, bool) {
	rabbit := s.catData.CharByName["default_character"]
	if rabbit == nil || !rabbit.IsHero() || s.catData.Progression.MaxPoints != 16 ||
		s.catData.Stats["hit_points"] == nil || s.catData.Stats["damage"] == nil || s.catData.Stats["speed"] == nil {
		return 0, false
	}
	updated := 0
	ok := s.runOperation(func() bool {
		if s.store.Meta.Receipts[existingRabbitGrantKey] != 0 {
			return true
		}
		for _, playerID := range s.store.AccountIDs() {
			account, exists := s.store.GetAccount(playerID)
			if !exists || account.Characters[int32(rabbit.Id)] == nil {
				continue
			}
			character := account.Characters[int32(rabbit.Id)]
			character.ProgressionPoints = 16
			character.HPLevel = int32(s.catData.Stats["hit_points"].MaxLevel)
			character.DamageLevel = int32(s.catData.Stats["damage"].MaxLevel)
			character.SpeedLevel = int32(s.catData.Stats["speed"].MaxLevel)

			meta := s.store.Meta.Player(playerID)
			bestStar, bestTier := int32(-1), -1
			for _, star := range s.catData.Definitions["star_powers"] {
				if star.String("target_character_name") == rabbit.Name {
					meta.OwnedStars[star.ID] = true
					if tier := star.Int("unlock_upgrade_tokens_spent"); tier > bestTier {
						bestStar, bestTier = int32(star.ID), tier
					}
				}
			}
			if character.StarPowerID <= 0 {
				character.StarPowerID = bestStar
			}
			for _, dash := range s.catData.Definitions["dashes"] {
				if dash.String("target_character_name") == rabbit.Name {
					meta.OwnedDashes[dash.ID] = true
					if character.DashID <= 0 {
						character.DashID = int32(dash.ID)
					}
				}
			}
			for _, upgrade := range s.catData.Definitions["ultimate_upgrades"] {
				if upgrade.String("target_character_name") == rabbit.Name {
					character.IsUltUpgraded = true
					if character.UltID <= 0 {
						character.UltID = int32(upgrade.ID)
					}
				}
			}
			updated++
		}
		s.store.Meta.Receipts[existingRabbitGrantKey] = time.Now().UnixMilli()
		return true
	})
	return updated, ok
}
