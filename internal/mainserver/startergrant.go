package mainserver

import (
	"fmt"
	"time"

	"github.com/freaks/openfreaks/internal/store"
)

// grantStarterUpgradeTokens applies the one-off test grant to every account
// that predates the starter balance. The durable Completed marker makes the
// migration idempotent across restarts and repeated deployments.
func (s *Server) grantStarterUpgradeTokens() (int, error) {
	before := s.store.DumpState()
	granted := 0
	for id := range before.Accounts {
		account, ok := s.store.GetAccount(id)
		if !ok {
			continue
		}
		meta := s.store.Meta.Player(id)
		if _, done := meta.Completed[store.StarterUpgradeGrantKey]; done {
			continue
		}
		if !s.changeCurrency(account, 4, int(store.StarterUpgradeTokens), "starter_grant", store.StarterUpgradeGrantKey) {
			continue
		}
		meta.Completed[store.StarterUpgradeGrantKey] = time.Now().UnixMilli()
		granted++
	}
	if granted == 0 {
		return 0, nil
	}
	if err := s.store.Save(); err != nil {
		s.store.RestoreState(before)
		return 0, fmt.Errorf("persist starter upgrade-token grant: %w", err)
	}
	return granted, nil
}
