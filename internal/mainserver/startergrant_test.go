package mainserver

import (
	"testing"

	"github.com/freaks/openfreaks/internal/config"
	"github.com/freaks/openfreaks/internal/store"
)

func TestStarterUpgradeTokensForNewAndExistingAccounts(t *testing.T) {
	s := NewServer(config.Default().Main, store.New(t.TempDir()), nil, nil, nil, "", 0, nil, nil)
	defer s.Close()
	fresh := s.store.CreateAccount("Fresh", []byte("token"))
	if fresh.Wallet.UpgradeTokens != store.StarterUpgradeTokens {
		t.Fatalf("new account tokens=%d", fresh.Wallet.UpgradeTokens)
	}
	if _, ok := s.store.Meta.Player(fresh.PlayerID).Completed[store.StarterUpgradeGrantKey]; !ok {
		t.Fatal("new account lacks durable starter marker")
	}

	legacy := s.store.CreateAccount("Legacy", []byte("legacy"))
	legacy.Wallet.UpgradeTokens = 10
	delete(s.store.Meta.Player(legacy.PlayerID).Completed, store.StarterUpgradeGrantKey)
	granted, err := s.grantStarterUpgradeTokens()
	if err != nil || granted != 1 || legacy.Wallet.UpgradeTokens != 160 {
		t.Fatalf("grant=%d err=%v tokens=%d", granted, err, legacy.Wallet.UpgradeTokens)
	}
	granted, err = s.grantStarterUpgradeTokens()
	if err != nil || granted != 0 || legacy.Wallet.UpgradeTokens != 160 {
		t.Fatalf("grant repeated: grant=%d err=%v tokens=%d", granted, err, legacy.Wallet.UpgradeTokens)
	}
}
