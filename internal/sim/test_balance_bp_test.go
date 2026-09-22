package sim

import (
	"testing"

	"github.com/freaks/openfreaks/internal/content"
)

func TestTestBalanceBasisPointsHitTypedNumbers(t *testing.T) {
	cases := []struct{ shown, typed int }{{3600, 5000}, {3600, 100}, {200, 333}, {650, 820}}
	for _, c := range cases {
		bp := (c.typed*content.BalanceUnit + c.shown/2) / c.shown
		if got := content.ScaleBP(c.shown, bp); got != c.typed {
			t.Fatalf("shown %d typed %d: got %d (bp %d)", c.shown, c.typed, got, bp)
		}
	}
}

func TestTestBalanceLegacyPercentsStillWork(t *testing.T) {
	legacy := content.TestBalance{HP: 150, Damage: 50, Speed: 110}
	s := legacy.Scaled()
	if !s.BasisPoints || s.HP != 15000 || s.Damage != 5000 || s.Speed != 11000 {
		t.Fatalf("legacy scaled wrong: %+v", s)
	}
	if n := legacy.Normalized(); n.HP != 150 || n.Damage != 50 || n.Speed != 110 {
		t.Fatalf("legacy normalized wrong: %+v", n)
	}
	if !(content.TestBalance{HP: 2778, Damage: 15000, Speed: 12615, BasisPoints: true}).Valid() {
		t.Fatal("basis-point balance rejected")
	}
}
