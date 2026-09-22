package protocol

import (
	"bytes"
	"testing"
)

func TestByteStreamRoundtrip(t *testing.T) {
	w := NewWriter()
	w.WriteInt(123)
	w.WriteLong(-9876543210)
	w.WriteString("hello")
	w.WriteBool(true)
	w.WriteShort(-300)
	w.WriteIntCollection([]int32{1, -2, 3})
	b := w.Bytes()
	r := NewReader(b)
	if v, _ := r.ReadInt(); v != 123 {
		t.Fatalf("int %d", v)
	}
	if v, _ := r.ReadLong(); v != -9876543210 {
		t.Fatalf("long %d", v)
	}
	if s, isNull, _ := r.ReadString(); s != "hello" || isNull {
		t.Fatalf("string %q", s)
	}
	if v, _ := r.ReadBool(); !v {
		t.Fatalf("bool")
	}
	if v, _ := r.ReadShort(); v != -300 {
		t.Fatalf("short %d", v)
	}
	n, err := r.ReadCollectionCount()
	if err != nil || n != 3 {
		t.Fatalf("count %d %v", n, err)
	}
	for i, want := range []int32{1, -2, 3} {
		v, err := r.ReadInt()
		if err != nil || v != want {
			t.Fatalf("item %d: %d %v", i, v, err)
		}
	}
	if !r.Ended() {
		t.Fatalf("trailing %d", r.Left())
	}
}

func TestTCPFrame(t *testing.T) {
	pkt := EncodeTCPFrame(5, []byte{1, 2, 3})
	id, n, err := DecodeTCPHeader(pkt[:6])
	if err != nil || id != 5 || n != 3 {
		t.Fatalf("header %d %d %v", id, n, err)
	}
	if !bytes.Equal(pkt[6:], []byte{1, 2, 3}) {
		t.Fatalf("payload")
	}
}

func TestPlayerActions31(t *testing.T) {
	acts := make([]PlayerAction, 31)
	for i := range acts {
		acts[i] = PlayerAction{Type: i % 6, X: 1000 - i, Y: -2000 + i, TargetObjectID: 2097151, AutoAttack: true}
	}
	enc := EncodePlayerActions(acts)
	if len(enc) > 236 {
		t.Fatalf("too big %d", len(enc))
	}
	dec, err := DecodePlayerActions(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for i := range acts {
		if dec[i] != acts[i] {
			t.Fatalf("mismatch %d: %+v vs %+v", i, dec[i], acts[i])
		}
	}
}

func TestBitSized(t *testing.T) {
	w := NewBitWriter(16)
	w.WritePositiveVIntMax65535(70000 % 65536)
	w.WritePositiveVIntMax255OftenZero(0)
	w.WritePositiveVIntMax255OftenZero(200)
	w.WritePositiveIntMax2097152OftenZero(0)
	w.WritePositiveIntMax2097152OftenZero(123456)
	b := w.Bytes()
	r := NewBitReader(b)
	if v, _ := r.ReadPositiveVIntMax65535(); v != 70000%65536 {
		t.Fatalf("sized %d", v)
	}
	if v, _ := r.ReadPositiveVIntMax255OftenZero(); v != 0 {
		t.Fatalf("often0 %d", v)
	}
	if v, _ := r.ReadPositiveVIntMax255OftenZero(); v != 200 {
		t.Fatalf("often200 %d", v)
	}
	if v, _ := r.ReadPositiveIntMax2097152OftenZero(); v != 0 {
		t.Fatalf("o2 %d", v)
	}
	if v, _ := r.ReadPositiveIntMax2097152OftenZero(); v != 123456 {
		t.Fatalf("o2b %d", v)
	}
}

func TestBattleFrames(t *testing.T) {
	srv := EncodeBattleServerFrame(UdpBattleState, 42, []byte{9, 9})
	if len(srv) != 12 || srv[0] != 0 || srv[1] != 2 {
		t.Fatalf("server frame %v", srv[:10])
	}
	cli := EncodeBattleClientFrame(UdpPlayerActions, []byte{1})
	id, ln, ok := TryReadBattleClientFrame(cli)
	if !ok || id != 2 || ln != 1 {
		t.Fatalf("client frame %d %d %v", id, ln, ok)
	}
}
