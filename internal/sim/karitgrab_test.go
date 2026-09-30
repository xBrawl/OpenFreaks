package sim

import "testing"

// ── Захват карита ────────────────────────────────────────────────────────

func TestKaritGrabMineDropPickupAndScatter(t *testing.T) {
	w := modeWorld(t, "karit_grab_mine", 6, halves, 7)
	if !w.IsKaritGrab() {
		t.Fatal("not karity grab")
	}
	s := w.kg()
	if s.mine == nil {
		t.Fatal("mine prop missing")
	}
	for _, p := range w.Players {
		p.IsBot = false // стоят на месте
	}
	advanceTicks(w, 20+kgSpawnTicks+1)
	if kgOnFloor(w) != 1 {
		t.Fatalf("after 7s floor=%d, want 1", kgOnFloor(w))
	}
	var gem *Item
	for _, it := range w.Items {
		if it.Definition.String("name") == kgItem {
			gem = it
		}
	}
	p := w.Players[0]
	p.X, p.Y, p.ExactX, p.ExactY = gem.X, gem.Y, gem.X, gem.Y
	advanceTicks(w, 2)
	if p.GamePoints != 1 || kgOnFloor(w) != 0 || w.TeamPoints[p.Team] != 1 {
		t.Fatalf("pickup: carried=%d floor=%d team=%d", p.GamePoints, kgOnFloor(w), w.TeamPoints[p.Team])
	}
	p.GamePoints = 6
	Kill(w, w.Players[1], p)
	if p.GamePoints != 0 || kgOnFloor(w) != 6 {
		t.Fatalf("scatter: carried=%d floor=%d", p.GamePoints, kgOnFloor(w))
	}
	snapshotAll(t, w)
}

func TestKaritGrabCountdownWinsAndResets(t *testing.T) {
	w := modeWorld(t, "karit_grab_crossroads", 6, halves, 3)
	for _, p := range w.Players {
		p.IsBot = false
	}
	advanceTicks(w, 25)
	a, b := w.Players[0], w.Players[1]
	a.GamePoints = 10
	advanceTicks(w, 100)
	s := w.kg()
	if s.countdownTeam != a.Team || s.countdownLeft >= kgCountdownTicks {
		t.Fatalf("countdown team=%d left=%d", s.countdownTeam, s.countdownLeft)
	}
	b.GamePoints = 10 // поровну — отсчёт замер
	left := s.countdownLeft
	advanceTicks(w, 50)
	if !s.paused || s.countdownLeft != left {
		t.Fatalf("tie should pause: paused=%v left=%d was %d", s.paused, s.countdownLeft, left)
	}
	b.GamePoints = 0
	a.GamePoints = 9 // потеряли десятку — сброс
	advanceTicks(w, 2)
	if s.countdownTeam != -1 {
		t.Fatalf("countdown should reset, team=%d", s.countdownTeam)
	}
	a.GamePoints = 12
	advanceTicks(w, kgCountdownTicks+5)
	if !w.Finished || w.WinnerTeam != a.Team || w.WinReason != "karity" {
		t.Fatalf("finished=%v winner=%d reason=%s", w.Finished, w.WinnerTeam, w.WinReason)
	}
}

func TestKaritGrabSpawnsBottomAndTop(t *testing.T) {
	w := modeWorld(t, "karit_grab_mine", 6, halves, 1)
	for _, p := range w.Players {
		if p.Team == 0 && p.Y > w.Tiles.PixH/4 || p.Team == 1 && p.Y < w.Tiles.PixH*3/4 {
			t.Fatalf("team %d spawned at y=%.0f of %.0f", p.Team, p.Y, w.Tiles.PixH)
		}
	}
}

func TestKaritGrabBotsFinishTheMatch(t *testing.T) {
	for seed := int64(1); seed <= 3; seed++ {
		w := modeWorld(t, "karit_grab_mine", 6, halves, seed)
		advanceTicks(w, 4200+40)
		if !w.Finished {
			t.Fatalf("seed %d: not finished", seed)
		}
		t.Logf("seed %d: winner=%d reason=%s tick=%d points=%v mined=%d", seed, w.WinnerTeam, w.WinReason, w.TickNum, w.TeamPoints, w.kg().mined)
		snapshotAll(t, w)
	}
}
