package mainserver

import (
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/protocol"
)

func TestSecondLoginOnAuthedConnectionIsRejected(t *testing.T) {
	s, c, _ := metaServer(t)
	if s.dispatch(c, protocol.CLogin, nil) {
		t.Fatal("повторный вход на авторизованном соединении прошёл")
	}
}

func TestSupersededConnectionCannotDispatch(t *testing.T) {
	s, c, a := metaServer(t)
	s.mu.Lock()
	s.byPlayer[a.PlayerID] = c
	s.mu.Unlock()
	if !s.dispatch(c, protocol.CKeepAliveRequest, []byte{0}) {
		t.Fatal("текущая сессия не смогла отправить keep-alive")
	}

	newer := &Conn{Server: s, PlayerID: a.PlayerID, SendCh: make(chan []byte, 16)}
	newer.Authed.Store(true)
	s.mu.Lock()
	s.byPlayer[a.PlayerID] = newer
	s.mu.Unlock()

	if s.dispatch(c, protocol.CKeepAliveRequest, []byte{0}) {
		t.Fatal("вытесненное соединение продолжило обслуживаться")
	}
}

func TestBindSessionDropsPreviousAlias(t *testing.T) {
	s, _, first := metaServer(t)
	second := s.store.CreateAccount("Second", []byte("token-second"))

	c := &Conn{Server: s, SendCh: make(chan []byte, 16)}
	s.bindSession(c, first.PlayerID)
	s.bindSession(c, second.PlayerID)

	s.mu.RLock()
	stale, aliased := s.byPlayer[first.PlayerID]
	current := s.byPlayer[second.PlayerID]
	s.mu.RUnlock()

	if aliased && stale == c {
		t.Fatal("соединение осталось привязанным к прежнему аккаунту")
	}
	if current != c {
		t.Fatal("новая привязка не установилась")
	}
}

func TestLoginLimiterStopsRepeatedAttempts(t *testing.T) {
	limiter := newLoginLimiter()
	allowed := 0
	for i := 0; i < loginMaxPerKey*2; i++ {
		if limiter.Allow("1.2.3.4|nick") {
			allowed++
		}
	}
	if allowed != loginMaxPerKey {
		t.Fatalf("пропущено %d попыток, ожидалось %d", allowed, loginMaxPerKey)
	}

	if !limiter.Allow("5.6.7.8|nick") {
		t.Fatal("лимит одного клиента заблокировал другого")
	}

	shifted := time.Now().Add(2 * loginWindow)
	limiter.nowSource = func() time.Time { return shifted }
	if !limiter.Allow("1.2.3.4|nick") {
		t.Fatal("окно лимита не истекает")
	}
}
