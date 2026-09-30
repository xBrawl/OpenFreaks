package sim

import "testing"

// Телевизор Кролика неуязвим, не теряет здоровье и бьёт всех врагов на карте.
func TestRabbitTVHitsWholeMapAndNeverDies(t *testing.T) {
	cat := mustCatalog(t)
	w := arena(60, 60)
	w.Catalog = cat
	rabbit := buildPlayer(cat, w, BuildPlayer{Index: 0, Team: 0, DashID: -1, UltUpgradeID: -1, StarPowerID: -1}, cat.CharByName["default_character"])
	near := buildPlayer(cat, w, BuildPlayer{Index: 1, Team: 1, DashID: -1, UltUpgradeID: -1, StarPowerID: -1}, cat.CharByName["wolf_character"])
	far := buildPlayer(cat, w, BuildPlayer{Index: 2, Team: 1, DashID: -1, UltUpgradeID: -1, StarPowerID: -1}, cat.CharByName["wolf_character"])
	rabbit.ObjectIndex, near.ObjectIndex, far.ObjectIndex = 1, 2, 3
	rabbit.X, rabbit.Y = 600, 600
	near.X, near.Y = 1200, 600
	far.X, far.Y = 17000, 17000
	for _, p := range []*Player{rabbit, near, far} {
		p.ExactX, p.ExactY = p.X, p.Y
		p.SpawnShield, p.SpawnShieldTicks = false, 0
	}
	w.Players = []*Player{rabbit, near, far}
	tv := SpawnMinion(w, rabbit, "rabbit_tv_character", 700, 700)
	if tv == nil {
		t.Fatal("tv not spawned")
	}
	start := tv.HP
	DealDamage(w, near, tv, 5000)
	for i := 0; i < 60; i++ {
		w.Advance()
	}
	if !tv.Alive || tv.HP != start {
		t.Fatalf("tv must not lose hp: alive=%v hp=%d/%d", tv.Alive, tv.HP, start)
	}
	if lost := far.MaxHP - far.HP; lost < 2*550 {
		t.Fatalf("far enemy lost only %d hp in 3 s", lost)
	}
}
