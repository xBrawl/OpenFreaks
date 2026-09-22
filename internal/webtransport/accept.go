package webtransport

import (
	"context"
	"github.com/coder/websocket"
	"net"
	"net/http"
	"strings"
	"sync"
)

type Acceptor struct {
	Origins                       []string
	MaxConnections, MaxPerAddress int
	mu                            sync.Mutex
	active                        int
	byAddress                     map[string]int
}

// Acquire bounds even unauthenticated/upgrading connections. RemoteAddr, not an
// untrusted forwarding header, is used; reverse proxies need an appropriate cap.
func (a *Acceptor) Acquire(w http.ResponseWriter, r *http.Request) (func(), bool) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", 405)
		return nil, false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, "invalid remote address", 400)
		return nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	maximum, perIP := a.MaxConnections, a.MaxPerAddress
	if maximum <= 0 {
		maximum = 128
	}
	if perIP <= 0 {
		perIP = 64
	}
	if a.byAddress == nil {
		a.byAddress = map[string]int{}
	}
	if a.active >= maximum || a.byAddress[host] >= perIP {
		http.Error(w, "connection capacity", 503)
		return nil, false
	}
	a.active++
	a.byAddress[host]++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.active--
			a.byAddress[host]--
			if a.byAddress[host] == 0 {
				delete(a.byAddress, host)
			}
			a.mu.Unlock()
		})
	}, true
}
func (a *Acceptor) Accept(parent context.Context, w http.ResponseWriter, r *http.Request, protocol string) (*websocket.Conn, context.Context, func(), bool) {
	if parent.Err() != nil {
		http.Error(w, "server stopping", 503)
		return nil, nil, nil, false
	}
	release, ok := a.Acquire(w, r)
	if !ok {
		return nil, nil, nil, false
	}
	offered := false
	for _, v := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if strings.TrimSpace(v) == protocol {
			offered = true
		}
	}
	if !offered {
		release()
		http.Error(w, "unsupported websocket subprotocol", 400)
		return nil, nil, nil, false
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: a.Origins, Subprotocols: []string{protocol}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		release()
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(ctx, func() { conn.CloseNow() })
	return conn, ctx, func() { cancel(); stop(); conn.CloseNow(); release() }, true
}
