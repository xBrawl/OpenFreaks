package mainserver

import (
	"crypto/rand"
	"fmt"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
	"math/big"
	"strconv"
	"strings"
	"time"
)

func (s *Server) currencyID(name string) int {
	for _, c := range s.catData.Currencies {
		if c.Name == name {
			return c.Id
		}
	}
	return 0
}
func (s *Server) currencyAmount(text string) (int, int, bool) {
	fields := strings.Fields(text)
	if len(fields) == 1 && fields[0] == "free" {
		return 0, 0, true
	}
	if len(fields) != 2 {
		return 0, 0, false
	}
	n, err := strconv.Atoi(fields[0])
	id := s.currencyID(fields[1])
	return id, n, err == nil && n >= 0 && n <= 1000000000 && id > 0
}
func randomN(n int) int {
	if n <= 1 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("OS random source unavailable")
	}
	return int(v.Int64())
}
func (s *Server) rewardDrops(a *store.Account, reward content.Spec) ([]protocol.ItemDrop, int, bool) {
	name := reward.Text()
	count := 1
	if reward.At("currency").Exists() {
		name = fmt.Sprintf("%d %s", reward.Int("count", 1), reward.String("currency"))
	}
	if id, n, ok := s.currencyAmount(name); ok && id > 0 && n > 0 {
		return []protocol.ItemDrop{{DropType: 0, ModelID: int32(id), Count: int32(n)}}, 0, true
	}
	if reward.At("skin").Exists() {
		skin := s.catData.Spec("skins", reward.String("skin"))
		if !skin.Exists() {
			return nil, 0, false
		}
		if a.OwnedSkins[int32(skin.ID)] {
			return []protocol.ItemDrop{{DropType: 0, ModelID: 1, Count: 100}}, 0, true
		}
		return []protocol.ItemDrop{{DropType: 2, ModelID: int32(skin.ID), Count: 1}}, 0, true
	}
	if reward.At("box").Exists() {
		name = reward.String("box")
	}
	if strings.HasPrefix(name, "box ") {
		name = strings.TrimPrefix(name, "box ")
	}
	if name == "test_portal" {
		id, n := 1, 50
		switch roll := randomN(100); {
		case roll >= 98:
			id, n = 3, 5
		case roll >= 90:
			id, n = 4, 10
		case roll >= 70:
			id, n = 2, 20
		}
		return []protocol.ItemDrop{{DropType: 0, ModelID: int32(id), Count: int32(n)}}, 3, true
	}
	box := s.catData.Spec("gacha_boxes", name)
	if !box.Exists() {
		return nil, 0, false
	}
	drops := []protocol.ItemDrop{}
	for _, currency := range box.At("guaranteed_drops").List() {
		id := s.currencyID(currency.Text())
		if id == 0 {
			return nil, 0, false
		}
		amount := 50
		if id == 4 {
			amount = 10
		}
		drops = append(drops, protocol.ItemDrop{DropType: 0, ModelID: int32(id), Count: int32(amount * count)})
	}
	if randomN(100) < box.Int("character_chance_percent") {
		candidates := []*content.Character{}
		total := 0
		weights := []int{}
		for _, ch := range s.catData.Heroes() {
			if a.Characters[int32(ch.Id)] != nil {
				continue
			}
			weight := box.At("character_rarity_weights").Int(ch.Rarity, 1)
			if weight > 0 {
				total += weight
				weights = append(weights, weight)
				candidates = append(candidates, ch)
			}
		}
		if total > 0 {
			roll := randomN(total)
			for i, ch := range candidates {
				roll -= weights[i]
				if roll < 0 {
					drops = append(drops, protocol.ItemDrop{DropType: 1, ModelID: int32(ch.Id), Count: 1})
					break
				}
			}
		}
	}
	item := 1
	if name == "big_box" {
		item = 2
	}
	return drops, item, len(drops) > 0
}
func (s *Server) canGrant(a *store.Account, drops []protocol.ItemDrop) bool {
	totals := map[int]int64{}
	for _, d := range drops {
		if d.Count <= 0 {
			return false
		}
		switch d.DropType {
		case 0:
			if walletCurrency(a, int(d.ModelID)) == nil {
				return false
			}
			totals[int(d.ModelID)] += int64(d.Count)
		case 1:
			ch := s.catData.CharByID[int(d.ModelID)]
			if ch == nil || !ch.IsHero() {
				return false
			}
		case 2:
			if !s.catData.SpecByID("skins", int(d.ModelID)).Exists() {
				return false
			}
		default:
			return false
		}
	}
	for id, n := range totals {
		if int64(*walletCurrency(a, id))+n > 2147483647 {
			return false
		}
	}
	return true
}
func (s *Server) grant(a *store.Account, drops []protocol.ItemDrop, reason, ref string) {
	for _, d := range drops {
		switch d.DropType {
		case 0:
			s.changeCurrency(a, int(d.ModelID), int(d.Count), reason, ref)
		case 1:
			if a.Characters[d.ModelID] == nil {
				a.Characters[d.ModelID] = &store.OwnedCharacter{CharacterModelID: d.ModelID, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, StarPowerID: -1, DashID: -1, UltID: -1}
			}
		case 2:
			a.OwnedSkins[d.ModelID] = true
		}
	}
}
func sendDeliveries(c *Conn, items []protocol.ItemDelivery) {
	if len(items) == 0 {
		return
	}
	w := protocol.NewWriter()
	w.WriteInt(int32(len(items)))
	for _, item := range items {
		item.Encode(w)
	}
	c.Send(protocol.SItemsDelivery, w.Bytes())
}
func offerWindow(offer content.Spec, now int64) (int64, int64) {
	switch offer.String("rotation") {
	case "daily":
		start := now / 86400 * 86400
		return start, start + 86400
	case "weekly":
		start := now / (7 * 86400) * (7 * 86400)
		return start, start + 7*86400
	}
	return 0, -1
}
func (s *Server) onShopPurchase(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	id, err := r.ReadInt()
	if err != nil {
		return false
	}
	end, err := r.ReadLong()
	if err != nil || !r.Ended() {
		return false
	}
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	offer := s.catData.SpecByID("shop_offers", int(id))
	code := int32(2)
	var drops []protocol.ItemDrop
	item := 0
	if offer.Exists() && (!offer.Bool("development_only") || s.cfg.Testing.AllowDebugBattleFlags) {
		start, until := offerWindow(offer, time.Now().Unix())
		priceID, price, valid := s.currencyAmount(offer.String("price", "free"))
		last := unixSeconds(s.store.Meta.Player(a.PlayerID).Offers[int(id)])
		switch {
		case !valid:
			code = 2
		case end != until:
			code = 4
		case last > 0 && (until < 0 && !offer.Bool("repeatable") || until >= 0 && last >= start && (!offer.Bool("repeatable") || price == 0)):
			code = 3
		case price > 0 && (walletCurrency(a, priceID) == nil || int(*walletCurrency(a, priceID)) < price):
			code = 1
		default:
			var validReward bool
			drops, item, validReward = s.rewardDrops(a, offer.At("reward"))
			if validReward && s.canGrant(a, drops) {
				ref := fmt.Sprintf("offer:%d:%d", id, until)
				if price > 0 {
					s.changeCurrency(a, priceID, -price, "purchase_offer", ref)
				}
				s.grant(a, drops, "shop_reward", ref)
				s.store.Meta.Player(a.PlayerID).Offers[int(id)] = time.Now().Unix()
				code = 0
			}
		}
	}
	c.Send(protocol.SShopOfferPurchaseResponse, protocol.EncodeCodeBool(code, code == 0))
	if code == 0 {
		sendDeliveries(c, []protocol.ItemDelivery{{ItemType: int32(item), Drops: drops}})
		return s.sendHome(c)
	}
	return true
}
func (s *Server) onSkinPurchase(c *Conn, payload []byte) bool {
	id, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	skin := s.catData.SpecByID("skins", int(id))
	code := int32(2)
	if skin.Exists() {
		ch := s.catData.CharByName[skin.String("character_name", skin.String("target_character_name"))]
		currency, price, valid := s.currencyAmount(skin.String("price"))
		if a.OwnedSkins[id] {
			code = 3
		} else if ch != nil && a.Characters[int32(ch.Id)] != nil && valid {
			if price == 0 || s.changeCurrency(a, currency, -price, "purchase_skin", fmt.Sprintf("skin:%d", id)) {
				a.OwnedSkins[id] = true
				code = 0
			} else {
				code = 1
			}
		}
	}
	c.Send(protocol.SSkinPurchaseResponse, protocol.EncodeCodeBool(code, code == 0))
	if code == 0 {
		return s.sendHome(c)
	}
	return true
}
func timestamp(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return -1
	}
	return t.Unix()
}
func (s *Server) activePass() content.Spec {
	now := time.Now().Unix()
	for _, season := range s.catData.Documents["battle_pass"].At("seasons").List() {
		if timestamp(season.String("starts_at")) <= now && timestamp(season.String("ends_at")) > now {
			return season
		}
	}
	return content.Spec{}
}
func (s *Server) ensurePass(a *store.Account) content.Spec {
	season := s.activePass()
	if !season.Exists() {
		return season
	}
	p := s.store.Meta.Player(a.PlayerID)
	id := season.Int("season_id")
	if p.Pass.Season != id {
		p.Pass = store.PassProgress{Season: id, EndsAt: timestamp(season.String("ends_at"))}
		a.Wallet.PassPoints = 0
	} else {
		p.Pass.EndsAt = timestamp(season.String("ends_at"))
	}
	return season
}
func passPrice(season content.Spec, tier int) int {
	switch tier {
	case 1:
		return season.At("prices").Int("premium")
	case 2:
		return season.At("prices").Int("premium_plus")
	}
	return 0
}
func (s *Server) onPassPurchase(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	end, err := r.ReadLong()
	if err != nil {
		return false
	}
	tier, err := r.ReadInt()
	if err != nil || !r.Ended() {
		return false
	}
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	season := s.ensurePass(a)
	p := s.store.Meta.Player(a.PlayerID)
	code := int32(0)
	switch {
	case !season.Exists() || end != p.Pass.EndsAt:
		code = 2
	case tier < 1 || tier > 2:
		code = 4
	case int(tier) <= p.Pass.Tier:
		code = 3
	default:
		price := passPrice(season, int(tier)) - passPrice(season, p.Pass.Tier)
		if price <= 0 {
			code = 4
		} else if !s.changeCurrency(a, 3, -price, "battle_pass_purchase", fmt.Sprintf("pass:%d:tier:%d", p.Pass.Season, tier)) {
			code = 1
		} else {
			p.Pass.Tier = int(tier)
		}
	}
	c.Send(protocol.SBrawlPassPurchaseResponse, protocol.EncodeCodeBool(code, code == 0))
	if code == 0 {
		return s.sendHome(c)
	}
	return true
}
func (s *Server) onPassClaim(c *Conn, payload []byte) bool {
	end, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	season := s.ensurePass(a)
	p := s.store.Meta.Player(a.PlayerID)
	if !season.Exists() || end != p.Pass.EndsAt {
		return c.Send(protocol.SBrawlPassClaimResponse, protocol.EncodeCodeBool(2, false))
	}
	masks := []int{p.Pass.ClaimedFree, p.Pass.ClaimedPremium, p.Pass.ClaimedPremiumPlus}
	items := []protocol.ItemDelivery{}
	all := []protocol.ItemDrop{}
	for index, tier := range season.At("tiers").List() {
		if index >= 63 || tier.Int("required_points") > int(a.Wallet.PassPoints) {
			continue
		}
		for track := 0; track <= p.Pass.Tier && track < 3; track++ {
			bit := 1 << index
			if masks[track]&bit != 0 {
				continue
			}
			rewards := tier.At([]string{"free", "premium", "premium_plus"}[track]).List()
			if len(rewards) == 0 {
				continue
			}
			for _, reward := range rewards {
				drops, item, valid := s.rewardDrops(a, reward)
				if !valid {
					return c.Send(protocol.SBrawlPassClaimResponse, protocol.EncodeCodeBool(1, false))
				}
				all = append(all, drops...)
				items = append(items, protocol.ItemDelivery{ItemType: int32(item), Drops: drops})
			}
			masks[track] |= bit
		}
	}
	if len(items) == 0 || !s.canGrant(a, all) {
		return c.Send(protocol.SBrawlPassClaimResponse, protocol.EncodeCodeBool(1, false))
	}
	s.grant(a, all, "battle_pass_claim", fmt.Sprintf("pass:%d", p.Pass.Season))
	p.Pass.ClaimedFree, p.Pass.ClaimedPremium, p.Pass.ClaimedPremiumPlus = masks[0], masks[1], masks[2]
	c.Send(protocol.SBrawlPassClaimResponse, protocol.EncodeCodeBool(0, true))
	sendDeliveries(c, items)
	return s.sendHome(c)
}
func (s *Server) ensureAbilityOwnership(a *store.Account) *store.PlayerMeta {
	m := s.store.Meta.Player(a.PlayerID)
	for _, ch := range a.Characters {
		if ch.StarPowerID > 0 {
			m.OwnedStars[int(ch.StarPowerID)] = true
		}
		if ch.DashID > 0 {
			m.OwnedDashes[int(ch.DashID)] = true
		}
	}
	return m
}
func indexedPrice(prices []int, index, fallback int) int {
	if len(prices) == 0 {
		return fallback
	}
	return max(1, prices[min(max(0, index), len(prices)-1)])
}
func (s *Server) onPurchaseUpgrade(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	charID, err := r.ReadInt()
	if err != nil {
		return false
	}
	kind, err := r.ReadInt()
	if err != nil {
		return false
	}
	modelID, err := r.ReadInt()
	if err != nil || !r.Ended() {
		return false
	}
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	ch := a.Characters[charID]
	def := s.catData.CharByID[int(charID)]
	success := false
	if ch != nil && def != nil {
		meta := s.ensureAbilityOwnership(a)
		price, currency := 0, 4
		var apply func()
		switch kind {
		case 0:
			stat := s.catData.SpecByID("character_stats", int(modelID))
			var level *int32
			switch stat.String("name") {
			case "hit_points":
				level = &ch.HPLevel
			case "damage":
				level = &ch.DamageLevel
			case "speed":
				level = &ch.SpeedLevel
			}
			if level != nil && *level < int32(stat.Int("max_level", 5)) && ch.ProgressionPoints < s.catData.Progression.MaxPoints {
				price = indexedPrice(s.catData.Progression.KaritCosts, ch.ProgressionPoints, 100)
				currency = 1
				apply = func() { *level++; ch.ProgressionPoints++ }
			}
		case 1:
			d := s.catData.SpecByID("dashes", int(modelID))
			if d.String("target_character_name") == def.Name && !meta.OwnedDashes[int(modelID)] {
				price = indexedPrice(s.catData.Progression.TokenCosts, 7, 10)
				apply = func() { meta.OwnedDashes[int(modelID)] = true; ch.DashID = modelID }
			}
		case 2:
			d := s.catData.SpecByID("star_powers", int(modelID))
			if d.String("target_character_name") == def.Name && !meta.OwnedStars[int(modelID)] {
				price = max(1, d.Int("unlock_upgrade_tokens_spent", 10))
				apply = func() { meta.OwnedStars[int(modelID)] = true; ch.StarPowerID = modelID }
			}
		case 3:
			d := s.catData.SpecByID("ultimate_upgrades", int(modelID))
			if d.String("target_character_name") == def.Name && !ch.IsUltUpgraded {
				price = indexedPrice(s.catData.Progression.TokenCosts, 15, 20)
				apply = func() { ch.IsUltUpgraded = true; ch.UltID = modelID }
			}
		}
		if apply != nil && price > 0 && s.changeCurrency(a, currency, -price, "character_upgrade", fmt.Sprintf("character:%d:%d:%d", charID, kind, modelID)) {
			apply()
			success = true
		}
	}
	c.Send(protocol.SCharacterStatUpgradeResponse, protocol.EncodeBoolOnly(success))
	if success {
		s.broadcastTeam(c)
		return s.sendHome(c)
	}
	return true
}
