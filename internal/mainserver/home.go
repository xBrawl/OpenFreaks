package mainserver

import (
	"sort"
	"time"

	"github.com/freaks/openfreaks/internal/protocol"
)

func (s *Server) sendHome(c *Conn) bool {
	acc, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	s.ensurePass(acc)
	s.ensureMissions(acc)
	meta := s.ensureAbilityOwnership(acc)
	msg := &protocol.HomeDataMsg{
		PlayerID: acc.PlayerID, PlayerName: acc.Name, Trophies: acc.Trophies,
		Onboarding: acc.Onboarding, Karit: acc.Wallet.Karit, Sparks: acc.Wallet.Sparks,
		VoidGems: acc.Wallet.VoidGems, UpgradeTokens: acc.Wallet.UpgradeTokens,
		PassPoints: acc.Wallet.PassPoints, AllianceID: acc.AllianceID,
		PassSeason: int32(meta.Pass.Season), PassSeasonEnd: meta.Pass.EndsAt, PassTier: int32(meta.Pass.Tier), ClaimedFree: int64(meta.Pass.ClaimedFree), ClaimedPremium: int64(meta.Pass.ClaimedPremium), ClaimedPlus: int64(meta.Pass.ClaimedPremiumPlus),
	}
	for _, ch := range acc.Characters {
		stars, dashes := []int32{}, []int32{}
		if model := s.catData.CharByID[int(ch.CharacterModelID)]; model != nil {
			for _, sp := range s.catData.Definitions["star_powers"] {
				if sp.String("target_character_name") == model.Name && meta.OwnedStars[sp.ID] {
					stars = append(stars, int32(sp.ID))
				}
			}
			for _, dash := range s.catData.Definitions["dashes"] {
				if dash.String("target_character_name") == model.Name && meta.OwnedDashes[dash.ID] {
					dashes = append(dashes, int32(dash.ID))
				}
			}
		}
		msg.Characters = append(msg.Characters, protocol.HomeCharacter{
			CharacterModelID:   ch.CharacterModelID,
			StarPowerIDs:       stars,
			DashIDs:            dashes,
			CurrentStarPowerID: ch.StarPowerID, CurrentDashID: ch.DashID,
			IsUltUpgraded: ch.IsUltUpgraded,
			HPLevel:       ch.HPLevel, DamageLevel: ch.DamageLevel, SpeedLevel: ch.SpeedLevel,
			Trophies: ch.Trophies, ProgressionPoints: int32(ch.ProgressionPoints), IsCurrent: ch.IsSelected,
		})
	}
	sort.Slice(msg.Characters, func(i, j int) bool { return msg.Characters[i].CharacterModelID < msg.Characters[j].CharacterModelID })
	now := time.Now().Unix()
	for _, offer := range s.catData.Definitions["shop_offers"] {
		if offer.Bool("development_only") && !s.cfg.Testing.AllowDebugBattleFlags {
			continue
		}
		currency, price, valid := s.currencyAmount(offer.String("price", "free"))
		if !valid {
			continue
		}
		if price == 0 {
			currency = -1
		}
		_, count, rewardCurrency := s.currencyAmount(offer.String("reward"))
		if !rewardCurrency {
			count = 1
		}
		start, end := offerWindow(offer, now)
		last := unixSeconds(meta.Offers[offer.ID])
		purchased := last > 0 && (end < 0 || last >= start)
		repeat := offer.Bool("repeatable") && price > 0
		msg.ShopOffers = append(msg.ShopOffers, protocol.HomeShopOffer{ModelID: int32(offer.ID), Count: int32(count), Price: int32(price), PriceCurrencyModelID: int32(currency), EndTimestamp: end, IsPurchased: purchased, Repeatable: repeat})
	}
	for _, m := range meta.Missions {
		if m.End <= now {
			continue
		}
		msg.Missions = append(msg.Missions, protocol.HomeMission{MissionModelID: int32(m.ModelID), DurationType: int16(m.DurationType), TargetGameModeModelID: int32(m.ModeID), TargetCharModelID: int32(m.CharacterID), Reward: int32(m.Reward), Current: int32(m.Progress), Target: int32(m.Target), Start: m.Start, End: m.End})
	}
	sort.Slice(msg.Missions, func(i, j int) bool {
		if msg.Missions[i].DurationType != msg.Missions[j].DurationType {
			return msg.Missions[i].DurationType < msg.Missions[j].DurationType
		}
		return msg.Missions[i].MissionModelID < msg.Missions[j].MissionModelID
	})
	skins := make([]int32, 0, len(acc.OwnedSkins))
	for id := range acc.OwnedSkins {
		skins = append(skins, id)
	}
	msg.OwnedSkins = skins
	sent := c.Send(protocol.SHomeData, msg.Encode())
	if sent && c.TeamID > 0 {
		s.broadcastTeam(c)
	}
	return sent
}
