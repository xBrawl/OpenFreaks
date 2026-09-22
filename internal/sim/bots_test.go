package sim

import "testing"

func block(w *World, x, y int) {
	i := y*w.Tiles.W + x
	w.Tiles.Blocked[i] = true
	w.Tiles.BlocksProj[i] = true
}

func TestBotNavigatesAroundWall(t *testing.T) {
	w := arena(20, 20)
	for y := 1; y < 17; y++ {
		block(w, 9, y)
	}
	p := fighter(0, 0, 2250, 2250)
	p.IsBot = true
	p.Brain = &BotBrain{}
	w.Players = []*Player{p}
	goal := navPoint{3450, 2250}
	for tick := 0; tick < 500; tick++ {
		botNavigate(w, p, goal, tick)

		p.IsBot = false
		w.Advance()
		p.IsBot = true
		if !w.Tiles.CanOccupy(p.X, p.Y, p.CollisionRadius) {
			t.Fatal("bot overlaps wall")
		}
		if Dist(p.X, p.Y, goal.x, goal.y) < 80 {
			return
		}
	}
	t.Fatalf("bot stuck at %.0f,%.0f", p.X, p.Y)
}

func TestPathDoesNotCutCornersOrCrossSealedWall(t *testing.T) {
	w := arena(10, 10)
	for y := 0; y < 10; y++ {
		block(w, 5, y)
	}
	a := navPoint{450, 450}
	path := findPath(&w.Tiles, a, navPoint{2250, 2250}, 120)
	for _, p := range path {
		if p.x >= 1500 || !walkClear(&w.Tiles, a, p, 120) {
			t.Fatal("invalid path segment")
		}
		a = p
	}
}

func TestBotWaitsForSightThenDamagesEnemy(t *testing.T) {
	w := arena(20, 20)
	p, e := fighter(0, 0, 1500, 1800), fighter(1, 1, 3000, 1800)
	p.IsBot = true
	p.Skills = []*Skill{{Behaviour: "attack", AttackType: "projectile", Projectile: "test", ProjectileID: 1, ProjSpeed: 3000, ProjRadius: 60, RangePx: 2000, Damage: 300, MaxCharge: 3, Charges: 3, RechargeMs: 700, CoolMs: 250, ActiveMs: 50, MsBetween: 100}}
	w.Players = []*Player{p, e}
	for y := 0; y < 20; y++ {
		block(w, 7, y)
	}
	for i := 0; i < 40; i++ {
		w.Advance()
	}
	if e.HP != e.MaxHP || len(w.Projectiles) != 0 {
		t.Fatal("bot fired through sealed wall")
	}
	for y := 0; y < 20; y++ {
		w.Tiles.Blocked[y*20+7] = false
		w.Tiles.BlocksProj[y*20+7] = false
	}
	for i := 0; i < 160; i++ {
		w.Advance()
	}
	if p.DamageDealt == 0 {
		t.Fatal("bot never attacked a visible enemy")
	}
	if p.Skills[0].Charges < 0 {
		t.Fatal("bot bypassed ammo validation")
	}
}

func TestBotPrioritizesCaptureZone(t *testing.T) {
	w := arena(20, 20)
	w.IsKOTH = true
	p := fighter(0, 0, 600, 600)
	p.IsBot = true
	w.Players = []*Player{p}
	for i := 0; i < 160; i++ {
		w.Advance()
	}
	if Dist(p.X, p.Y, 3000, 3000) > 1000 || w.GetPoint(0) == 0 {
		t.Fatal("bot failed to capture zone")
	}
}

func BenchmarkBotsTenPlayers(b *testing.B) {
	w := arena(21, 33)
	for i := 0; i < 10; i++ {
		p := fighter(i, i%2, float64(600+i*450), 600)
		p.IsBot = true
		w.Players = append(w.Players, p)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BotStep(w, i)
	}
}
