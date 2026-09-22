package sim

import (
	"testing"

	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
)

const realRoot = `../../deploy/content-real`

func mustCatalog(t *testing.T) *content.CatalogData {
	t.Helper()
	c, err := content.Parse(realRoot)
	if err != nil {
		t.Skipf("no catalog: %v", err)
	}
	return c
}

func TestBuildAllLocations(t *testing.T) {
	cat := mustCatalog(t)
	built := 0
	for _, loc := range cat.Locations {
		if loc.Hidden {
			continue
		}
		w, err := BuildWorld(cat, BuildParams{
			BattleID: "test", LocationID: loc.Id, IntroTicks: 20,
			Players: []BuildPlayer{
				{PlayerID: 1, Index: 0, Team: 0, Name: "p1", CharacterID: 2, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltUpgradeID: -1},
				{PlayerID: 2, Index: 1, Team: 1, Name: "p2", CharacterID: 3, HPLevel: 2, DamageLevel: 2, SpeedLevel: 2, DashID: -1, UltUpgradeID: -1},
			},
		})
		if err != nil {
			t.Fatalf("loc %s: %v", loc.Name, err)
		}
		if len(w.Players) != 2 {
			t.Fatalf("loc %s: players=%d", loc.Name, len(w.Players))
		}

		for i := 0; i < 60; i++ {
			w.Players[0].ApplyWalk(4000, 4000)
			if i == 10 {
				TryCast(w, w.Players[0], w.Players[0].Skills[0], protocol.PlayerAction{Type: protocol.ActionWeapon, X: 1000}, false)
			}
			if i == 20 && len(w.Players[0].Skills) > 1 {
				w.Players[0].UltCharge = 4000
				TryCast(w, w.Players[0], w.Players[0].Skills[1], protocol.PlayerAction{Type: protocol.ActionUlt, X: 1000}, true)
			}
			BotStep(w, int(w.TickNum))
			w.Advance()
		}
		for _, idx := range []int{0, 1} {
			snap := EncodeSnapshot(w, idx, protocol.ParticipantPlayer)
			if len(snap) == 0 {
				t.Fatalf("loc %s: empty snapshot", loc.Name)
			}
		}
		built++
	}
	t.Logf("built+simulated %d locations", built)
}

func TestBuildBallMode(t *testing.T) {
	cat := mustCatalog(t)

	locID := -1
	for _, loc := range cat.Locations {
		if !loc.Hidden {
			locID = loc.Id
			break
		}
	}
	w, err := BuildWorld(cat, BuildParams{
		BattleID: "balltest", LocationID: locID, IntroTicks: 5,
		Players: []BuildPlayer{
			{PlayerID: 1, Index: 0, Team: 0, Name: "p1", CharacterID: 2, DashID: -1},
			{PlayerID: 2, Index: 1, Team: 1, Name: "p2", CharacterID: 2, DashID: -1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.HasCarryable = true
	w.SpawnBall()
	if w.Ball == nil {
		t.Fatal("no ball entity")
	}
	for i := 0; i < 120; i++ {
		w.Players[0].ApplyWalk(int(w.Ball.X), int(w.Ball.Y))
		w.Advance()
		if w.Finished {
			break
		}
	}
	snap := EncodeSnapshot(w, 0, protocol.ParticipantPlayer)
	if len(snap) == 0 {
		t.Fatal("empty snapshot")
	}
}
