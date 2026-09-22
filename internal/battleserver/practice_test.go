package battleserver

import (
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/sim"
	"testing"
	"time"
)

func TestPracticeJoinsReusesSlotsAndNeverTimesOut(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServerWithCatalog(Config{TickMs: 50, MaxMatches: 16, ReadinessTimeout: time.Second}, nil, nil, cat)
	defer func() { s.Close(); s.Wait() }()
	loc := cat.LocByName["experimental_test_arena"]
	hero := cat.Heroes()[0]
	join := func(id int64) *BattleGrant {
		g, e := s.JoinPractice(&PracticeRequest{LocationID: loc.Id, Player: Participant{PlayerID: id, Name: "Tester", ModelID: int32(hero.Id), HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltID: -1, StarPowerID: -1}})
		if e != nil {
			t.Fatal(e)
		}
		return g
	}
	first := join(1)
	second := join(2)
	if first.BattleID != second.BattleID || first.OwnTeam == second.OwnTeam {
		t.Fatal("not opponents in same room")
	}
	for i := int64(3); i <= 20; i++ {
		join(i)
	}
	if s.MatchCount() != 2 {
		t.Fatal("expected two rooms")
	}
	if !s.Leave(first.BattleID, 1) {
		t.Fatal("leave failed")
	}
	replacement := join(21)
	if replacement.OwnIndex != first.OwnIndex || replacement.SessionID == first.SessionID {
		t.Fatal("slot/session not replaced")
	}
	s.mu.Lock()
	m := s.matches[first.BattleID]
	s.mu.Unlock()
	m.worldMu.Lock()
	m.World.TickNum = int32(m.World.TotalTicks() + 1)
	m.World.Advance()
	finished := m.World.Finished
	m.worldMu.Unlock()
	if finished {
		t.Fatal("practice timed out")
	}
	if _, e := s.Resume(first.BattleID, 1); e == nil {
		t.Fatal("evicted session resumed")
	}
}

func TestPracticeSeatsKarityBoss(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServerWithCatalog(Config{TickMs: 50, MaxMatches: 16, ReadinessTimeout: time.Second}, nil, nil, cat)
	defer func() { s.Close(); s.Wait() }()
	loc := cat.LocByName["experimental_test_arena"]
	hero := cat.Heroes()[0]
	grant, e := s.JoinPractice(&PracticeRequest{LocationID: loc.Id, Player: Participant{PlayerID: 1, Name: "Tester", ModelID: int32(hero.Id), HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltID: -1, StarPowerID: -1}})
	if e != nil {
		t.Fatal(e)
	}
	boss := cat.CharByName[practiceBossModel]
	if boss == nil {
		t.Fatal("в каталоге нет босса")
	}
	if len(grant.Players) != practiceSlots {
		t.Fatalf("в ростере %d участников, ждали %d", len(grant.Players), practiceSlots)
	}
	seated := grant.Players[practiceBossSlot]
	if seated.ModelID != int32(boss.Id) || !seated.IsBot {
		t.Fatalf("на слоте босса %+v", seated)
	}
	if grant.OwnIndex >= practiceBossSlot {
		t.Fatalf("игрок сел на слот босса: %d", grant.OwnIndex)
	}
	s.mu.Lock()
	match := s.matches[grant.BattleID]
	s.mu.Unlock()
	match.worldMu.Lock()
	placed := match.World.Players[practiceBossSlot]
	cx, cy := match.World.Tiles.PixW/2, match.World.Tiles.PixH/2
	dx, dy := placed.X-cx, placed.Y-cy
	match.worldMu.Unlock()
	if !placed.Alive || placed.Abandoned || placed.HP <= 0 {
		t.Fatalf("Karity создан неактивным: alive=%v abandoned=%v hp=%d",
			placed.Alive, placed.Abandoned, placed.HP)
	}
	if dx*dx+dy*dy > float64(3*sim.PixelsPerTile*3*sim.PixelsPerTile) {
		t.Fatalf("Karity появился не в центре: (%.0f, %.0f), центр (%.0f, %.0f)",
			placed.X, placed.Y, cx, cy)
	}
}
