package mainserver

import (
	"io"
	"log/slog"
	"testing"

	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/store"
)

func TestExistingRabbitOwnersReceiveLevel17AndAllAbilitiesOnce(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	st := store.New(dir)
	ids := make([]int32, 0, len(cat.Heroes()))
	for _, hero := range cat.Heroes() {
		ids = append(ids, int32(hero.Id))
	}
	st.ConfigureHeroes(ids)
	first := st.CreateAccount("RabbitOwner", []byte("test"))
	second := st.CreateAccount("AnotherRabbitOwner", []byte("test"))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{store: st, catData: cat, log: log, conns: map[int64]*Conn{}}

	accounts, ok := s.grantExistingRabbitMaxLevels()
	if !ok || accounts != 2 {
		t.Fatalf("grant: accounts=%d ok=%v", accounts, ok)
	}
	rabbit := cat.CharByName["default_character"]
	if rabbit == nil {
		t.Fatal("Rabbit character is missing")
	}
	for _, account := range []*store.Account{first, second} {
		owned := account.Characters[int32(rabbit.Id)]
		if owned == nil || owned.ProgressionPoints != cat.Progression.MaxPoints ||
			owned.HPLevel != int32(cat.Stats["hit_points"].MaxLevel) ||
			owned.DamageLevel != int32(cat.Stats["damage"].MaxLevel) ||
			owned.SpeedLevel != int32(cat.Stats["speed"].MaxLevel) {
			t.Fatalf("Rabbit progression was not maxed for account %d: %+v", account.PlayerID, owned)
		}
		meta := st.Meta.Player(account.PlayerID)
		for _, star := range cat.Definitions["star_powers"] {
			if star.String("target_character_name") == rabbit.Name && !meta.OwnedStars[star.ID] {
				t.Fatalf("Rabbit star power %d is locked", star.ID)
			}
		}
		for _, dash := range cat.Definitions["dashes"] {
			if dash.String("target_character_name") == rabbit.Name && !meta.OwnedDashes[dash.ID] {
				t.Fatalf("Rabbit dash %d is locked", dash.ID)
			}
		}
		if owned.DashID <= 0 || !owned.IsUltUpgraded || owned.UltID <= 0 || owned.StarPowerID <= 0 {
			t.Fatalf("Rabbit's selected abilities were not initialized: %+v", owned)
		}
	}
	if st.Meta.Receipts[existingRabbitGrantKey] == 0 {
		t.Fatal("Rabbit grant marker was not recorded")
	}

	reloaded := store.New(dir)
	for _, account := range []*store.Account{first, second} {
		got, found := reloaded.GetAccount(account.PlayerID)
		if !found || got.Characters[int32(rabbit.Id)].ProgressionPoints != cat.Progression.MaxPoints {
			t.Fatalf("Rabbit progression did not persist for account %d", account.PlayerID)
		}
	}
	s2 := &Server{store: reloaded, catData: cat, log: log, conns: map[int64]*Conn{}}
	if accounts, ok = s2.grantExistingRabbitMaxLevels(); !ok || accounts != 0 {
		t.Fatalf("grant reran: accounts=%d ok=%v", accounts, ok)
	}
}
