package mainserver

import (
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"testing"
)

func TestBalanceOwnershipBoundsAndBattleLock(t *testing.T) {
	s, c, a := metaServer(t)
	s.cfg.Testing.EnableBalanceOverrides = true
	send := func(id, hp, dmg, spd int32) {
		w := protocol.NewWriter()
		for _, v := range []int32{id, hp, dmg, spd} {
			w.WriteInt(v)
		}
		if !s.onTestBalance(c, w.Bytes()) {
			t.Fatal("request rejected at framing")
		}
	}
	id := a.SelectedChar
	send(id, 150, 175, 110)
	want := content.TestBalance{HP: 150, Damage: 175, Speed: 110}
	if s.balanceFor(a.PlayerID, id) != want {
		t.Fatal("valid stats not saved")
	}
	for _, v := range [][3]int32{{201, 100, 100}, {100, 49, 100}, {100, 100, 126}, {0, 100, 100}} {
		send(id, v[0], v[1], v[2])
		if s.balanceFor(a.PlayerID, id) != want {
			t.Fatal("out of bounds changed balance")
		}
	}
	send(999999, 100, 100, 100)
	if _, ok := s.store.Meta.Player(a.PlayerID).TestBalance[999999]; ok {
		t.Fatal("unowned hero modified")
	}
	c.BattleID = "active"
	send(id, 100, 100, 100)
	if s.balanceFor(a.PlayerID, id) != want {
		t.Fatal("mid-battle edit")
	}
	c.BattleID = ""
	s.cfg.Testing.EnableBalanceOverrides = false
	if s.balanceFor(a.PlayerID, id) != (content.TestBalance{}) {
		t.Fatal("disabled feature still applied")
	}
	s.cfg.Testing.EnableBalanceOverrides = true
	send(id, 0, 0, 0)
	if s.balanceFor(a.PlayerID, id) != (content.TestBalance{}) {
		t.Fatal("reset failed")
	}
}

func TestBalanceProbeAnswersInBasisPoints(t *testing.T) {
	s, c, a := metaServer(t)
	s.cfg.Testing.EnableBalanceOverrides = true
	id := a.SelectedChar
	ask := func(values ...int32) (bool, [3]int32) {
		for len(c.SendCh) > 0 {
			<-c.SendCh
		}
		w := protocol.NewWriter()
		w.WriteInt(id)
		for _, v := range values {
			w.WriteInt(v)
		}
		if !s.onTestBalance(c, w.Bytes()) {
			t.Fatal("request rejected at framing")
		}
		r := protocol.NewReader((<-c.SendCh)[6:])
		ok, _ := r.ReadBool()
		_, _ = r.ReadInt()
		var got [3]int32
		for i := range got {
			got[i], _ = r.ReadInt()
		}
		return ok, got
	}
	if ok, got := ask(-1, -1, -2); !ok || got != [3]int32{10000, 10000, 10000} {
		t.Fatalf("fresh probe: ok=%v got=%v", ok, got)
	}
	if ok, _ := ask(25000, 12345, 10000, content.BalanceUnit); !ok {
		t.Fatal("basis-point edit rejected")
	}
	if ok, got := ask(-1, -1, -2); !ok || got != [3]int32{25000, 12345, 10000} {
		t.Fatalf("probe after edit: ok=%v got=%v", ok, got)
	}
	if ok, got := ask(-1, -1, -1); !ok || got != [3]int32{250, 123, 100} {
		t.Fatalf("legacy query: ok=%v got=%v", ok, got)
	}
}
