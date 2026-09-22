package mainserver

import (
	"io"
	"log/slog"
	"testing"

	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/store"
)

func TestExistingAccountsReachLevel17Once(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	st := store.New(dataDir)
	ids := make([]int32, 0, len(cat.Heroes()))
	for _, hero := range cat.Heroes() {
		ids = append(ids, int32(hero.Id))
	}
	st.ConfigureHeroes(ids)
	first := st.CreateAccount("ExistingOne", []byte("first"))
	st.CreateAccount("ExistingTwo", []byte("second"))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{store: st, catData: cat, log: log, conns: map[int64]*Conn{}}
	accounts, characters, ok := s.grantExistingMaxCharacterLevels()
	if !ok || accounts != 2 || characters != 2*len(cat.Heroes()) {
		t.Fatalf("grant: accounts=%d characters=%d ok=%v", accounts, characters, ok)
	}
	for _, owned := range first.Characters {
		if owned.ProgressionPoints != 16 || owned.HPLevel != 5 || owned.DamageLevel != 5 || owned.SpeedLevel != 5 {
			t.Fatalf("character not at level 17: %+v", owned)
		}
		if !owned.IsUltUpgraded || owned.UltID <= 0 {
			t.Fatalf("ultimate remains locked: %+v", owned)
		}
	}
	if st.Meta.Receipts[existingMaxLevelGrantKey] == 0 {
		t.Fatal("durable migration marker missing")
	}

	reloaded := store.New(dataDir)
	reloaded.ConfigureHeroes(ids)
	newAccount := reloaded.CreateAccount("AfterGrant", []byte("new"))
	s2 := &Server{store: reloaded, catData: cat, log: log, conns: map[int64]*Conn{}}
	accounts, characters, ok = s2.grantExistingMaxCharacterLevels()
	if !ok || accounts != 0 || characters != 0 {
		t.Fatalf("migration reran: accounts=%d characters=%d ok=%v", accounts, characters, ok)
	}
	for _, owned := range newAccount.Characters {
		if owned.ProgressionPoints != 0 || owned.HPLevel != 1 || owned.DamageLevel != 1 || owned.SpeedLevel != 1 {
			t.Fatalf("new account was granted level 17: %+v", owned)
		}
	}
}
