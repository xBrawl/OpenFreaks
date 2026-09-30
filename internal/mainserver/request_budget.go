package mainserver

import (
	"sync"
	"time"
)

type requestBudget struct {
	mu     sync.Mutex
	at     time.Time
	tokens float64
}

func (b *requestBudget) allow(now time.Time, authenticated bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	rate, burst := float64(5), float64(12)
	if authenticated {
		rate = 50
		burst = 80
	}
	if b.at.IsZero() {
		b.tokens = burst
	} else {
		b.tokens = min(burst, b.tokens+max(0, now.Sub(b.at).Seconds())*rate)
	}
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
