package battleserver

import (
	"bytes"
	"testing"
)

func TestReliableFragmentsOrderedExactlyOnce(t *testing.T) {
	p := newPeer(1, 2, nil, nil)
	var got [][]byte
	p.onApp = func(_ *Peer, b []byte, _ bool) { got = append(got, append([]byte(nil), b...)) }
	feedFragment(p, 2, 0, []byte("world"))
	feedFragment(p, 1, 1, []byte{hdrData, 'h', 'e', 'l', 'l', 'o', ' '})
	if len(got) != 0 {
		t.Fatal("delivered fragments before previous message")
	}
	feedFragment(p, 0, 0, []byte{hdrHello})
	if len(got) != 1 || !bytes.Equal(got[0], []byte("hello world")) {
		t.Fatalf("invalid reassembly: %q", got)
	}
	feedFragment(p, 1, 1, []byte{hdrData, 'x'})
	feedFragment(p, 2, 0, []byte("bad"))
	if len(got) != 1 {
		t.Fatal("retransmission applied twice")
	}
	feedFragment(p, 3, 0, []byte{hdrData, '!'})
	if len(got) != 2 || string(got[1]) != "!" {
		t.Fatal("next message stalled")
	}
}

func TestReliableReceiveWindowIsBounded(t *testing.T) {
	p := newPeer(1, 2, nil, nil)
	for sn := uint32(1000); sn < 3000; sn++ {
		feedFragment(p, sn, 1, []byte{hdrData})
	}
	if p.engine.PeekSize() >= 0 {
		t.Fatal("unbounded out-of-window buffer")
	}
}

func feedFragment(p *Peer, sn uint32, frg byte, payload []byte) {
	p.handleReliable(buildSegment(p.Conv, kcpPUSH, frg, 512, 0, sn, 0, payload))
}
