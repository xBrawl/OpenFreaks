package mainserver

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"github.com/coder/websocket"
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/config"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var webSmokeSequence atomic.Int64

func TestWebSocketRegistrationMatchAndBattle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default().Main
	cfg.Authentication.EnableTestRegistration = true
	cfg.Testing.EnableBots = true
	cfg.Matchmaking.BotFillSeconds = 1
	bs := battleserver.NewServerWithCatalog(battleserver.Config{TickMs: 50, ReadinessTimeout: time.Second}, nil, nil, cat)
	s := NewServer(cfg, store.New(""), content.Load("../../deploy/content-real"), cat, bs, "127.0.0.1", 9610, nil, nil)
	defer func() { s.Close(); bs.Close(); bs.Wait() }()
	mainHTTP := httptest.NewServer(s.WebSocketHandler(ctx, nil))
	defer mainHTTP.Close()
	battleHTTP := httptest.NewServer(bs.WebSocketHandler(ctx, nil))
	defer battleHTTP.Close()
	mainURL, battleURL := "ws"+strings.TrimPrefix(mainHTTP.URL, "http"), "ws"+strings.TrimPrefix(battleHTTP.URL, "http")
	external := os.Getenv("OPENFREAKS_WEB_MAIN") != ""
	if external {
		mainURL = os.Getenv("OPENFREAKS_WEB_MAIN")
		battleURL = os.Getenv("OPENFREAKS_WEB_BATTLE")
		if battleURL == "" {
			t.Fatal("battle endpoint required")
		}
	}
	ws, _, err := websocket.Dial(ctx, mainURL, &websocket.DialOptions{Subprotocols: []string{"openfreaks.main.v1"}, HTTPHeader: webSmokeHeaders()})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(32 << 20)
	send := func(id int16, body []byte) {
		t.Helper()
		if err := ws.Write(ctx, websocket.MessageBinary, protocol.EncodeTCPFrame(id, body)); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (int16, []byte) {
		t.Helper()
		kind, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind != websocket.MessageBinary || len(data) < 6 {
			t.Fatal("invalid WS frame")
		}
		id, n, err := protocol.DecodeTCPHeader(data[:6])
		if err != nil || int(n) != len(data)-6 {
			t.Fatal("not exactly one TCP frame")
		}
		return id, data[6:]
	}
	w := protocol.NewWriter()
	w.WriteInt(123)
	send(protocol.CClientHello, w.Bytes())
	if id, _ := read(); id != protocol.SServerHello {
		t.Fatal(id)
	}
	send(protocol.CModelsRequest, emptyPayload())
	if id, _ := read(); id != protocol.SModelsResponse {
		t.Fatal(id)
	}
	w = protocol.NewWriter()
	w.WriteLong(-1)
	w.WriteString("test-login-v1:" + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(`{"nickname":"ws%d_%d","password":"pass1234","register":true,"invitationCode":"VERITYINFREAKS"}`, time.Now().UnixMilli(), webSmokeSequence.Add(1)))))
	w.WriteInt(1)
	send(protocol.CLogin, w.Bytes())
	if id, _ := read(); id != protocol.SLoginOk {
		t.Fatal(id)
	}
	if id, _ := read(); id != protocol.SHomeData {
		t.Fatal(id)
	}
	w = protocol.NewWriter()
	w.WriteInt(1)
	w.WriteBool(false)
	w.WriteBool(false)
	send(protocol.CStartBattle, w.Bytes())
	var session int64
	for {
		id, data := read()
		if id == protocol.SMatchmaking {
			continue
		}
		if id != protocol.SBattleStarted {
			t.Fatal(id)
		}
		r := protocol.NewReader(data)
		r.ReadInt()
		r.ReadString()
		r.ReadInt()
		r.ReadInt()
		n, _ := r.ReadCollectionCount()
		for i := 0; i < n; i++ {
			r.ReadString()
			for j := 0; j < 8; j++ {
				r.ReadInt()
			}
			k, _ := r.ReadCollectionCount()
			for j := 0; j < k; j++ {
				r.ReadInt()
			}
		}
		r.ReadString()
		r.ReadInt()
		session, _ = r.ReadLong()
		break
	}
	send(64, intPayload(5))
	if id, payload := read(); id != 64 {
		t.Fatalf("emote response id=%d", id)
	} else {
		r := protocol.NewReader(payload)
		_, e := r.ReadInt()
		emoji, e2 := r.ReadInt()
		if e != nil || e2 != nil || emoji != 5 || !r.Ended() {
			t.Fatal("invalid emote echo")
		}
	}
	combat, _, err := websocket.Dial(ctx, battleURL, &websocket.DialOptions{Subprotocols: []string{"openfreaks.battle.v1"}, HTTPHeader: webSmokeHeaders()})
	if err != nil {
		t.Fatal(err)
	}
	defer combat.CloseNow()
	combat.SetReadLimit(256 << 10)
	auth := make([]byte, 8)
	binary.BigEndian.PutUint64(auth, uint64(session))
	if err = combat.Write(ctx, websocket.MessageBinary, auth); err != nil {
		t.Fatal(err)
	}
	_, ack, err := combat.Read(ctx)
	if err != nil || len(ack) != 1 || ack[0] != 1 {
		t.Fatalf("battle ack: %v %x", err, ack)
	}
	actions := protocol.EncodeBattleClientFrame(protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{{Type: protocol.ActionWalk, X: 1000, Y: 1000}}))
	if err = combat.Write(ctx, websocket.MessageBinary, actions); err != nil {
		t.Fatal(err)
	}
	_, snapshot, err := combat.Read(ctx)
	if err != nil || len(snapshot) < 12 {
		t.Fatalf("snapshot: %v", err)
	}
	send(protocol.CExitBattle, emptyPayload())
	if external {
		return
	}
	for i := 0; i < 100; i++ {
		bsStore := s.Store()
		unlock := bsStore.LockState()
		allocation := bsStore.Meta.Allocations
		left := false
		for _, a := range allocation {
			for _, p := range a.Players {
				if !p.IsBot && p.Abandoned {
					left = true
				}
			}
		}
		unlock()
		if left {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("web client leave not processed")
}
func TestWebSocketMainRejectsUnauthenticatedCommandsAndOrigin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Default().Main
	s := NewServer(cfg, store.New(""), nil, nil, nil, "", 0, nil, nil)
	defer s.Close()
	server := httptest.NewServer(s.WebSocketHandler(ctx, nil))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	_, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"openfreaks.main.v1"}, HTTPHeader: http.Header{"Origin": []string{"https://untrusted.example"}}})
	if err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatal("foreign origin accepted")
	}
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"openfreaks.main.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.Write(ctx, websocket.MessageBinary, protocol.EncodeTCPFrame(protocol.CHomeRequest, emptyPayload()))
	deadline, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if _, _, err := ws.Read(deadline); err == nil {
		t.Fatal("preauth command accepted")
	}
}

func TestExternal20WebPlayers(t *testing.T) {
	if os.Getenv("OPENFREAKS_WEB_LOAD20") != "1" || os.Getenv("OPENFREAKS_WEB_MAIN") == "" {
		t.Skip("explicit external web load only")
	}
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) { t.Parallel(); TestWebSocketRegistrationMatchAndBattle(t) })
	}
}

func webSmokeHeaders() http.Header {
	h := http.Header{}
	if origin := os.Getenv("OPENFREAKS_WEB_ORIGIN"); origin != "" {
		h.Set("Origin", origin)
	}
	return h
}
