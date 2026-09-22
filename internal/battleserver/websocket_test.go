package battleserver

import (
	"context"
	"encoding/binary"
	"github.com/coder/websocket"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebBattleSessionReplacementAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	bs := NewServerWithCatalog(Config{TickMs: 50, ReadinessTimeout: time.Second}, nil, nil, cat)
	defer func() { bs.Close(); bs.Wait() }()
	grant, err := bs.JoinPractice(&PracticeRequest{LocationID: cat.LocByName["experimental_test_arena"].Id, Player: Participant{PlayerID: 1, Name: "web", ModelID: int32(cat.Heroes()[0].Id), HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, StarPowerID: -1, UltID: -1}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(bs.WebSocketHandler(ctx, nil))
	defer server.Close()
	dial := func(sid int64, accept bool) *websocket.Conn {
		t.Helper()
		ws, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{Subprotocols: []string{"openfreaks.battle.v1"}})
		if e != nil {
			t.Fatal(e)
		}
		data := make([]byte, 8)
		binary.BigEndian.PutUint64(data, uint64(sid))
		if e = ws.Write(ctx, websocket.MessageBinary, data); e != nil {
			t.Fatal(e)
		}
		_, ack, e := ws.Read(ctx)
		if accept && (e != nil || len(ack) != 1 || ack[0] != 1) {
			t.Fatalf("auth ack %x %v", ack, e)
		}
		if !accept && e == nil {
			t.Fatal("invalid grant accepted")
		}
		return ws
	}
	rejected := dial(grant.SessionID+1, false)
	rejected.CloseNow()
	first := dial(grant.SessionID, true)
	defer first.CloseNow()
	_, _, err = first.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second := dial(grant.SessionID, true)
	defer second.CloseNow()

	closed := false
	for i := 0; i < 10; i++ {
		if _, _, err = first.Read(ctx); err != nil {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("old socket remained alive")
	}
	actions := protocol.EncodeBattleClientFrame(protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{{Type: protocol.ActionWalk, X: 2000, Y: 2000}}))
	if err = second.Write(ctx, websocket.MessageBinary, actions); err != nil {
		t.Fatal(err)
	}
	if _, _, err = second.Read(ctx); err != nil {
		t.Fatal("replacement no snapshots", err)
	}
	if !bs.Leave(grant.BattleID, 1) {
		t.Fatal("leave failed")
	}
	rejected = dial(grant.SessionID, false)
	rejected.CloseNow()
}
func TestWebSnapshotQueueIsBoundedAndKeepsNewest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &webPeer{ctx: ctx, cancel: cancel, control: make(chan []byte, 64), snapshot: make(chan []byte, 1)}
	for i := 0; i < 10000; i++ {
		if !w.send([]byte{byte(i)}, true) {
			t.Fatal("snapshot enqueue failed")
		}
	}
	if len(w.snapshot) != 1 || (<-w.snapshot)[0] != byte(9999%256) {
		t.Fatal("old snapshots queued")
	}
	for i := 0; i < 64; i++ {
		if !w.send([]byte{byte(i)}, false) {
			t.Fatal("control queue early failure")
		}
	}
	if w.send([]byte{1}, false) {
		t.Fatal("unbounded control queue")
	}
	cancel()
	if w.send([]byte{2}, true) {
		t.Fatal("send after close")
	}
}
func TestWebBattleRejectsTextAndMalformedActions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	bs := NewServerWithCatalog(Config{TickMs: 50}, nil, nil, cat)
	defer func() { bs.Close(); bs.Wait() }()
	server := httptest.NewServer(bs.WebSocketHandler(ctx, nil))
	defer server.Close()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{Subprotocols: []string{"openfreaks.battle.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.Write(ctx, websocket.MessageText, []byte("123"))
	if _, _, err = ws.Read(ctx); err == nil {
		t.Fatal("text authentication accepted")
	}
}
