package mainserver

import (
	"net"
	"strings"
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
	mu         sync.Mutex
	attempts   map[string][]time.Time
	sweptAt    time.Time
	nowSource  func() time.Time
	hashAt     time.Time
	hashTokens float64
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
	return host + "|" + strings.ToUpper(strings.TrimSpace(nickname))
}

func (l *loginLimiter) Allow(key string) bool {
	return l.allow(key, loginMaxPerKey)
}

// Пароли и регистрации делят бюджет адреса: смена ника не обходит PBKDF2-лимит.
func (l *loginLimiter) AllowAddress(c *Conn) bool {
	return l.allow("address:"+loginKey(c, ""), 32)
}

// Bound expensive PBKDF2 work across different accounts and source addresses.
func (l *loginLimiter) AllowPasswordWork() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowSource()
	if l.hashAt.IsZero() {
		l.hashTokens = 16
	} else {
		l.hashTokens = min(16, l.hashTokens+max(0, now.Sub(l.hashAt).Seconds())*8)
	}
	l.hashAt = now
	if l.hashTokens < 1 {
		return false
	}
	l.hashTokens--
	return true
}

func (l *loginLimiter) allow(key string, maximum int) bool {
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
	if len(kept) >= maximum {
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
