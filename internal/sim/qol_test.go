package sim

import "testing"

func TestRegenWaitsThreeSecondsThenHeals(t *testing.T) {
	w := footballFixture(t)
	w.TickNum = int32(w.IntroTicks)
	p := w.Players[0]
	p.X, p.Y = w.Tiles.PixW/2, w.Tiles.PixH/2
	p.SpawnShield = false
	DealDamage(w, w.Players[1], p, p.MaxHP/2)
	hurt := p.HP
	for i := 0; i < RegenDelayTicks-1; i++ {
		tickRegen(w, p)
		w.TickNum++
	}
	if p.HP != hurt {
		t.Fatalf("лечение до конца паузы: %d -> %d", hurt, p.HP)
	}
	w.TickNum++
	tickRegen(w, p)
	if want := hurt + p.MaxHP*RegenPercentOfMax/100; p.HP != want {
		t.Fatalf("hp=%d, want %d", p.HP, want)
	}
	DealDamage(w, w.Players[1], p, 1)
	before := p.HP
	w.TickNum += RegenPeriodTicks
	tickRegen(w, p)
	if p.HP != before {
		t.Fatal("попадание не сбросило паузу автохила")
	}
}

func TestFootballCarryPastPostScores(t *testing.T) {
	w := footballFixture(t)
	p := w.Players[0]
	// У самой штанги: раньше эта полоса проёма была вне зоны гола.
	p.X, p.Y = footballSpot(w, 0, goalLine+carriedGoalMargin-10, -goalHalfWidth+60)
	p.CarryableObjectID = w.Ball.ObjectIndex
	BallTick(w)
	if w.GetPoint(0) != 1 {
		t.Fatal("мяч, внесённый у штанги, не засчитан")
	}
}

func TestFootballFieldHasNoObstaclesAndClosedGoals(t *testing.T) {
	w := footballFixture(t)
	g := w.Tiles
	// along — клетка по оси «ворота — ворота», across — поперёк.
	lengthTiles, widthTiles := g.H, g.W
	if w.BallHorizontal() {
		lengthTiles, widthTiles = g.W, g.H
	}
	blocked := func(along, across int) bool {
		if w.BallHorizontal() {
			return g.Blocked[across*g.W+along]
		}
		return g.Blocked[along*g.W+across]
	}
	for a := 4; a < lengthTiles-4; a++ {
		for c := 1; c < widthTiles-1; c++ {
			if blocked(a, c) {
				t.Fatalf("препятствие в поле на клетке %d,%d", a, c)
			}
		}
	}
	// За сеткой прохода нет: по бокам кармана ворот стены.
	mid := widthTiles / 2
	if !blocked(2, mid-3) || !blocked(2, mid+3) || blocked(2, mid) || !blocked(1, mid) ||
		!blocked(lengthTiles-3, mid-3) || blocked(lengthTiles-3, mid) || !blocked(lengthTiles-2, mid) {
		t.Fatal("карман ворот не совпадает с рамой")
	}
}
