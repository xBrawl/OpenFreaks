package battleserver

import (
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/sim"
	"testing"
	"time"
)

func practiceJoin(t *testing.T, s *Server, cat *content.CatalogData, id int64) *BattleGrant {
	t.Helper()
	loc := cat.LocByName["experimental_test_arena"]
	hero := cat.Heroes()[0]
	g, e := s.JoinPractice(&PracticeRequest{LocationID: loc.Id, Player: Participant{PlayerID: id, Name: "Tester", ModelID: int32(hero.Id), HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltID: -1, StarPowerID: -1}})
	if e != nil {
		t.Fatal(e)
	}
	return g
}

func TestPracticeGivesEveryJoinAFreshPrivateRoom(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServerWithCatalog(Config{TickMs: 50, MaxMatches: 16, ReadinessTimeout: time.Second}, nil, nil, cat)
	defer func() { s.Close(); s.Wait() }()
	first := practiceJoin(t, s, cat, 1)
	second := practiceJoin(t, s, cat, 2)
	if first.BattleID == second.BattleID {
		t.Fatal("двое игроков попали в одну комнату")
	}
	if first.OwnIndex != 0 || first.OwnTeam != 0 {
		t.Fatalf("игрок не на месте 0: %d/%d", first.OwnIndex, first.OwnTeam)
	}
	again := practiceJoin(t, s, cat, 1)
	if again.BattleID == first.BattleID || again.SessionID == first.SessionID {
		t.Fatalf("повторный вход не дал свежую комнату: %s/%d и %s/%d", first.BattleID, first.SessionID, again.BattleID, again.SessionID)
	}
	if _, e := s.Resume(first.BattleID, 1); e == nil {
		t.Fatal("старая комната не отпустила игрока")
	}
	s.mu.Lock()
	m := s.matches[again.BattleID]
	s.mu.Unlock()
	m.worldMu.Lock()
	m.World.TickNum = int32(m.World.TotalTicks() + 1)
	m.World.Advance()
	finished := m.World.Finished
	m.worldMu.Unlock()
	if finished {
		t.Fatal("тестовая арена кончилась по времени")
	}
}

func TestPracticeRoomClosesWithoutResultWhenPlayerLeaves(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan *Result, 4)
	s := NewServerWithCatalog(Config{TickMs: 5, MaxMatches: 16, ReadinessTimeout: 10 * time.Millisecond}, nil, func(r *Result) { results <- r }, cat)
	defer func() { s.Close(); s.Wait() }()
	grant := practiceJoin(t, s, cat, 1)
	if !s.Leave(grant.BattleID, 1) {
		t.Fatal("leave failed")
	}
	select {
	case r := <-results:
		if r.BattleID != grant.BattleID || !r.Aborted || r.AbortReason != "practice_left" {
			t.Fatalf("итог %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("пустая комната тестовой арены не закрылась")
	}
}

func TestPracticeSeatsKarityBoss(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServerWithCatalog(Config{TickMs: 50, MaxMatches: 16, ReadinessTimeout: time.Second}, nil, nil, cat)
	defer func() { s.Close(); s.Wait() }()
	grant := practiceJoin(t, s, cat, 1)
	boss := cat.CharByName[practiceBossModel]
	if boss == nil {
		t.Fatal("в каталоге нет босса")
	}
	if len(grant.Players) != 2 {
		t.Fatalf("в ростере %d участников, ждали 2", len(grant.Players))
	}
	seated := grant.Players[1]
	if seated.ModelID != int32(boss.Id) || !seated.IsBot || seated.Team != 1 {
		t.Fatalf("на месте босса %+v", seated)
	}
	s.mu.Lock()
	match := s.matches[grant.BattleID]
	s.mu.Unlock()
	match.worldMu.Lock()
	placed := match.World.Players[1]
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
