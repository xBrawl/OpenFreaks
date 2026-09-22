package sim

import (
	"bytes"
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

func referenceMapTiles(bw *protocol.BitWriter, w *World) {
	bw.WritePositiveVIntMax65535OftenZero(0)
	for x := 0; x < w.Tiles.W; x++ {
		for y := 0; y < w.Tiles.H; y++ {
			i := y*w.Tiles.W + x
			if w.Tiles.Destructible[i] {
				bw.WriteBool(w.Tiles.Destroyed[i])
			}
		}
	}
}

func TestDestructibleOrderMatchesFullGridScan(t *testing.T) {
	cat := mustCatalog(t)
	checked := 0
	for _, loc := range cat.Locations {
		if loc.Hidden {
			continue
		}
		w, err := BuildWorld(cat, BuildParams{
			BattleID: "tiles", LocationID: loc.Id, IntroTicks: 0,
			Players: []BuildPlayer{
				{PlayerID: 1, Index: 0, Team: 0, Name: "p1", CharacterID: 2, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltUpgradeID: -1},
				{PlayerID: 2, Index: 1, Team: 1, Name: "p2", CharacterID: 3, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltUpgradeID: -1},
			},
		})
		if err != nil {
			t.Fatalf("loc %s: %v", loc.Name, err)
		}

		broken := 0
		for i := range w.Tiles.Destructible {
			if w.Tiles.Destructible[i] && i%3 == 0 {
				w.Tiles.Destroyed[i] = true
				broken++
			}
		}

		want := protocol.NewBitWriter(8192)
		referenceMapTiles(want, w)
		got := protocol.NewBitWriter(8192)
		encodeMapTiles(got, w)
		if !bytes.Equal(want.Bytes(), got.Bytes()) {
			t.Fatalf("loc %s: tile bits differ (destroyed=%d)", loc.Name, broken)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no locations checked")
	}
}

func TestCharactersCacheFollowsMinionSet(t *testing.T) {
	w := arena(20, 20)
	hero := fighter(0, 0, 600, 600)
	w.Players = []*Player{hero}
	if len(w.Characters()) != 1 {
		t.Fatalf("expected the lone hero, got %d", len(w.Characters()))
	}

	minion := fighter(1, 0, 900, 900)
	w.Minions = append(w.Minions, minion)
	w.TouchMinions()
	if got := w.Characters(); len(got) != 2 || got[1] != minion {
		t.Fatalf("minion not visible through the cache: %d", len(got))
	}

	first := w.Characters()
	second := w.Characters()
	if &first[0] != &second[0] {
		t.Fatal("cache rebuilt without a change to the minion set")
	}

	w.Minions = w.Minions[:0]
	w.TouchMinions()
	if got := w.Characters(); len(got) != 1 {
		t.Fatalf("removed minion still visible: %d", len(got))
	}
}
