package mainserver

import (
	"net"
	"sync"
	"time"
)

const (
	loginWindow      = time.Minute
	loginMaxPerKey   = 8
	loginSweepEvery  = 5 * time.Minute
	loginSweepMinLen = 256
)

type loginLimiter struct {
	mu        sync.Mutex
	attempts  map[string][]time.Time
	sweptAt   time.Time
	nowSource func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: map[string][]time.Time{}, nowSource: time.Now}
}

func loginKey(c *Conn, nickname string) string {
	host := "unknown"
	if c != nil && c.Conn != nil && c.Conn.RemoteAddr() != nil {
		if h, _, err := net.SplitHostPort(c.Conn.RemoteAddr().String()); err == nil {
			host = h
		} else {
			host = c.Conn.RemoteAddr().String()
		}
	}
	return host + "|" + nickname
}

func (l *loginLimiter) Allow(key string) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowSource()
	l.sweepLocked(now)
	kept := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if now.Sub(at) < loginWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) >= loginMaxPerKey {
		l.attempts[key] = kept
		return false
	}
	l.attempts[key] = append(kept, now)
	return true
}

func (l *loginLimiter) sweepLocked(now time.Time) {
	if len(l.attempts) < loginSweepMinLen || now.Sub(l.sweptAt) < loginSweepEvery {
		return
	}
	l.sweptAt = now
	for key, times := range l.attempts {
		fresh := false
		for _, at := range times {
			if now.Sub(at) < loginWindow {
				fresh = true
				break
			}
		}
		if !fresh {
			delete(l.attempts, key)
		}
	}
}
