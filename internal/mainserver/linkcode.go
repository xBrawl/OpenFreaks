package mainserver

import (
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	linkCodeTTL         = 10 * time.Minute
	linkCodeMinInterval = 15 * time.Second
	linkCodeSweepEvery  = 5 * time.Minute

	linkCodeGroups   = 2
	linkCodeGroupLen = 4
	linkCodePrefix   = "FRK"
)

const linkCodeAlphabet = "ACDEFGHJKLMNPQRTUVWXYZ234679"

var (
	errLinkCodeTooSoon = errors.New("link_code_too_soon")
	errLinkCodeRandom  = errors.New("link_code_random_failed")
)

type linkCodeEntry struct {
	Expires  time.Time
	PlayerID int64
}

type linkCodes struct {
	byCode   map[string]linkCodeEntry
	byPlayer map[int64]string
	issuedAt map[int64]time.Time
	nowSrc   func() time.Time
	sweptAt  time.Time
	mu       sync.Mutex
}

func newLinkCodes() *linkCodes {
	return &linkCodes{
		byCode:   make(map[string]linkCodeEntry),
		byPlayer: make(map[int64]string),
		issuedAt: make(map[int64]time.Time),
	}
}

func (l *linkCodes) now() time.Time {
	if l.nowSrc != nil {
		return l.nowSrc()
	}
	return time.Now()
}

func (l *linkCodes) Issue(playerID int64) (string, time.Duration, error) {
	if playerID == 0 {
		return "", 0, errLinkCodeRandom
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	if last, ok := l.issuedAt[playerID]; ok && now.Sub(last) < linkCodeMinInterval {
		return "", linkCodeMinInterval - now.Sub(last), errLinkCodeTooSoon
	}

	code, err := l.freshCodeLocked()
	if err != nil {
		return "", 0, err
	}

	if previous, ok := l.byPlayer[playerID]; ok {
		delete(l.byCode, previous)
	}

	l.byCode[code] = linkCodeEntry{PlayerID: playerID, Expires: now.Add(linkCodeTTL)}
	l.byPlayer[playerID] = code
	l.issuedAt[playerID] = now

	return code, linkCodeTTL, nil
}

func (l *linkCodes) Redeem(code string) (int64, bool) {
	normalized := normalizeLinkCode(code)
	if normalized == "" {
		return 0, false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	entry, ok := l.byCode[normalized]
	if !ok || !now.Before(entry.Expires) {

		if ok {
			delete(l.byCode, normalized)
			delete(l.byPlayer, entry.PlayerID)
		}
		return 0, false
	}

	delete(l.byCode, normalized)
	delete(l.byPlayer, entry.PlayerID)
	return entry.PlayerID, true
}

func (l *linkCodes) freshCodeLocked() (string, error) {

	for attempt := 0; attempt < 8; attempt++ {
		code, err := generateLinkCode()
		if err != nil {
			return "", err
		}
		if _, taken := l.byCode[code]; !taken {
			return code, nil
		}
	}
	return "", errLinkCodeRandom
}

func (l *linkCodes) sweepLocked(now time.Time) {
	if now.Sub(l.sweptAt) < linkCodeSweepEvery {
		return
	}
	l.sweptAt = now

	for code, entry := range l.byCode {
		if !now.Before(entry.Expires) {
			delete(l.byCode, code)
			delete(l.byPlayer, entry.PlayerID)
		}
	}
	for player, at := range l.issuedAt {
		if now.Sub(at) > linkCodeTTL {
			delete(l.issuedAt, player)
		}
	}
}

func generateLinkCode() (string, error) {
	body := make([]byte, 0, linkCodeGroups*linkCodeGroupLen)
	buf := make([]byte, 1)
	limit := byte(256 - 256%len(linkCodeAlphabet))

	for len(body) < cap(body) {
		if _, err := rand.Read(buf); err != nil {
			return "", errLinkCodeRandom
		}

		if buf[0] >= limit {
			continue
		}
		body = append(body, linkCodeAlphabet[int(buf[0])%len(linkCodeAlphabet)])
	}

	var b strings.Builder
	b.WriteString(linkCodePrefix)
	for g := 0; g < linkCodeGroups; g++ {
		b.WriteByte('-')
		b.Write(body[g*linkCodeGroupLen : (g+1)*linkCodeGroupLen])
	}
	return b.String(), nil
}

func normalizeLinkCode(input string) string {
	if len(input) > 64 {
		return ""
	}

	upper := strings.ToUpper(strings.TrimSpace(input))
	want := linkCodeGroups * linkCodeGroupLen

	body := make([]byte, 0, len(linkCodePrefix)+want)
	for i := 0; i < len(upper); i++ {
		ch := upper[i]
		if ch == '-' || ch == ' ' {
			continue
		}
		if strings.IndexByte(linkCodeAlphabet, ch) < 0 || len(body) == cap(body) {
			return ""
		}
		body = append(body, ch)
	}

	text := string(body)
	if len(text) == len(linkCodePrefix)+want && strings.HasPrefix(text, linkCodePrefix) {
		text = text[len(linkCodePrefix):]
	}
	if len(text) != want {
		return ""
	}

	var b strings.Builder
	b.WriteString(linkCodePrefix)
	for g := 0; g < linkCodeGroups; g++ {
		b.WriteByte('-')
		b.WriteString(text[g*linkCodeGroupLen : (g+1)*linkCodeGroupLen])
	}
	return b.String()
}
