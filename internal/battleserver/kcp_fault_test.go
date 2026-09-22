package battleserver

import (
	"bytes"
	"fmt"
	kcp "github.com/xtaci/kcp-go/v5"
	"testing"
	"time"
)

func TestKCPRecoversLossDuplicationAndReordering(t *testing.T) {
	type packet struct {
		server bool
		data   []byte
		at     time.Time
	}
	queue := []packet{}
	sent := 0
	schedule := func(server bool, b []byte) {
		sent++
		if sent%6 == 0 {
			return
		}
		delay := time.Duration(20+(sent*17)%70) * time.Millisecond
		queue = append(queue, packet{server, append([]byte(nil), b...), time.Now().Add(delay)})
		if sent%11 == 0 {
			queue = append(queue, packet{server, append([]byte(nil), b...), time.Now().Add(delay + 25*time.Millisecond)})
		}
	}
	p := newPeer(77, 88, nil, nil)
	p.output = func(b []byte) { schedule(false, b[9:]) }
	received := [][]byte{}
	p.onApp = func(_ *Peer, b []byte, _ bool) { received = append(received, b) }
	client := kcp.NewKCP(88, func(b []byte, n int) { schedule(true, b[:n]) })
	client.SetMtu(kcpMSS + kcpOverhead)
	client.WndSize(512, 512)
	client.NoDelay(1, 10, 2, 1)
	p.SendHello()
	client.Send([]byte{hdrHello})
	want := [][]byte{}
	for i := 0; i < 12; i++ {
		message := bytes.Repeat([]byte(fmt.Sprintf("message-%02d;", i)), 400)
		want = append(want, message)
		client.Send(append([]byte{hdrData}, message...))
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(received) < len(want) {
		client.Update()
		p.Tick()
		now := time.Now()
		due := []packet{}
		future := []packet{}
		for _, b := range queue {
			if !now.Before(b.at) {
				due = append(due, b)
			} else {
				future = append(future, b)
			}
		}
		queue = future
		for _, b := range due {
			if b.server {
				p.handleReliable(b.data)
			} else {
				client.Input(b.data, kcp.IKCP_PACKET_REGULAR, true)
			}
		}
		for n := client.PeekSize(); n >= 0; n = client.PeekSize() {
			b := make([]byte, n)
			client.Recv(b)
			if n == 5 && b[0] == hdrPing {
				client.Send(append([]byte{hdrPong}, b[1:]...))
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(received) != len(want) {
		t.Fatalf("recovered %d/%d messages", len(received), len(want))
	}
	for i := range want {
		if !bytes.Equal(want[i], received[i]) {
			t.Fatalf("corrupt, duplicated or reordered message %d", i)
		}
	}
}
func TestAuthenticationGateAndSnapshotBackpressure(t *testing.T) {
	p := newPeer(1, 2, nil, nil)
	calls := 0
	p.onApp = func(_ *Peer, _ []byte, _ bool) { calls++ }
	p.handleUnreliable(hdrData, []byte("forbidden"))
	p.deliverReliableMessage([]byte{hdrData, 1})
	if calls != 0 {
		t.Fatal("application data before Hello")
	}
	p.deliverReliableMessage([]byte{hdrHello})
	p.handleUnreliable(hdrData, []byte("accepted"))
	if calls != 1 {
		t.Fatal("authenticated data rejected")
	}
	for i := 0; i < 500; i++ {
		p.SendSnapshot(bytes.Repeat([]byte{byte(i)}, 10000))
	}
	p.mu.Lock()
	queued := p.engine.WaitSnd()
	pending := append([]byte(nil), p.pendingSnapshot...)
	p.mu.Unlock()
	if queued > 64 || len(pending) != 10000 || pending[0] != byte(499%256) {
		t.Fatalf("snapshot queue is not bounded/coalesced: %d pending=%d", queued, len(pending))
	}
}
