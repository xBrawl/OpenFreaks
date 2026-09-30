package mainserver

import (
	"io"
	"log/slog"
	"testing"

	"github.com/freaks/openfreaks/internal/config"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/store"
)

func TestNewCatalogHeroPersistsBeforeAnyPlayerLogsIn(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	st := store.New(dir)
	oldHero := int32(cat.Heroes()[0].Id)
	st.ConfigureHeroes([]int32{oldHero})
	account := st.CreateAccount("Existing", []byte("test"))
	account.Characters[oldHero].HPLevel = 3
	st.Meta.Receipts[existingMaxLevelGrantKey] = 1
	if err = st.Save(); err != nil {
		t.Fatal(err)
	}
	s := NewServer(config.Node{}, st, nil, cat, nil, "", 0, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	defer s.Close()
	restored := store.New(dir)
	got, ok := restored.GetAccount(account.PlayerID)
	if !ok || got.Characters[int32(cat.CharByName["default_character"].Id)] == nil {
		t.Fatal("Rabbit was not durably granted during startup")
	}
	if got.SelectedChar != oldHero || got.Characters[oldHero].HPLevel != 3 {
		t.Fatal("catalog synchronization changed existing selection or progression")
	}
	if got.Online {
		t.Fatal("stale online presence survived startup")
	}
}
