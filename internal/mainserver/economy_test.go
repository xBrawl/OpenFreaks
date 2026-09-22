package mainserver

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"testing"
	"time"
)

func TestDailyFreeOfferCannotBeFarmed(t *testing.T) {
	s, c, a := metaServer(t)
	offer := s.catData.Spec("shop_offers", "daily_karit")
	_, end := offerWindow(offer, time.Now().Unix())
	w := protocol.NewWriter()
	w.WriteInt(int32(offer.ID))
	w.WriteLong(end)
	before := a.Wallet.Karit
	for i := 0; i < 10; i++ {
		if !s.dispatch(c, protocol.CShopOfferPurchaseRequest, w.Bytes()) {
			t.Fatal("dispatch failed")
		}
	}
	if a.Wallet.Karit-before != 100 {
		t.Fatalf("free offer awarded %d", a.Wallet.Karit-before)
	}
}
func TestPassUsesYAMLPricesAndClaimsOnce(t *testing.T) {
	s, c, a := metaServer(t)
	a.Wallet.VoidGems = 500
	s.ensurePass(a)
	a.Wallet.PassPoints = 100
	p := s.store.Meta.Player(a.PlayerID)
	for tier := 1; tier <= 2; tier++ {
		w := protocol.NewWriter()
		w.WriteLong(p.Pass.EndsAt)
		w.WriteInt(int32(tier))
		if !s.dispatch(c, protocol.CBrawlPassPurchaseRequest, w.Bytes()) {
			t.Fatal("purchase failed")
		}
	}
	if a.Wallet.VoidGems != 251 || p.Pass.Tier != 2 {
		t.Fatal("premium upgrade did not charge only the difference")
	}
	before := a.Wallet
	for i := 0; i < 3; i++ {
		s.dispatch(c, protocol.CBrawlPassClaimRequest, longPayload(p.Pass.EndsAt))
	}
	if a.Wallet.Karit-before.Karit != 60 || a.Wallet.UpgradeTokens-before.UpgradeTokens != 60 {
		t.Fatalf("wrong YAML rewards: before=%+v after=%+v", before, a.Wallet)
	}
	if p.Pass.ClaimedFree != 3 || p.Pass.ClaimedPremium != 3 || p.Pass.ClaimedPremiumPlus != 3 {
		t.Fatal("claimed tier bitmasks incorrect")
	}
}
func TestUpgradeChangesOnlyRequestedStatAndValidatesAbilityOwner(t *testing.T) {
	s, c, a := metaServer(t)
	ch := a.Characters[a.SelectedChar]
	before := a.Wallet.Karit
	w := protocol.NewWriter()
	w.WriteInt(a.SelectedChar)
	w.WriteInt(0)
	w.WriteInt(2)
	s.dispatch(c, protocol.CPurchaseCharacterUpgradeRequest, w.Bytes())
	if ch.HPLevel != 1 || ch.DamageLevel != 2 || ch.SpeedLevel != 1 || a.Wallet.Karit != before-20 {
		t.Fatalf("wrong stat upgrade: %+v", ch)
	}
	a.Wallet.UpgradeTokens = 100
	wrong := 0
	for _, d := range s.catData.Definitions["star_powers"] {
		if d.String("target_character_name") != s.catData.CharByID[int(a.SelectedChar)].Name {
			wrong = d.ID
			break
		}
	}
	w = protocol.NewWriter()
	w.WriteInt(a.SelectedChar)
	w.WriteInt(2)
	w.WriteInt(int32(wrong))
	s.dispatch(c, protocol.CPurchaseCharacterUpgradeRequest, w.Bytes())
	s.dispatch(c, protocol.CCharacterStarPowerChange, intPayload(int32(wrong)))
	if ch.StarPowerID != -1 || a.Wallet.UpgradeTokens != 100 {
		t.Fatal("foreign passive accepted or charged")
	}
}
