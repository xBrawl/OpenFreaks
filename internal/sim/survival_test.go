package sim

import "testing"

func TestPoisonMatchesRoundedClientZoneAndDamagesOnCadence(t *testing.T) {
	w := arena(30, 30)
	w.IsShowdown = true
	p := fighter(0, 0, 4500, 4500)
	enemy := fighter(1, 1, 100, 100)
	w.Players = []*Player{p, enemy}
	if w.PoisonedAt(100, 100, 430) || w.PoisonedAt(4500, 4500, 700) || !w.PoisonedAt(100, 100, 700) {
		t.Fatal("zone timing or center mismatch")
	}

	if w.PoisonedAt(8000, 4500, 600) || !w.PoisonedAt(8000, 8000, 600) {
		t.Fatal("zone must have rounded corners")
	}
	w.TickNum = 700
	w.TickPoison()
	hp := enemy.HP
	if hp != enemy.MaxHP*9/10 || p.HP != p.MaxHP {
		t.Fatal("poison did not damage only exposed player")
	}
	w.TickNum++
	w.TickPoison()
	if enemy.HP != hp {
		t.Fatal("poison applied on every frame")
	}
	w.IsArenaLobby = true
	if w.PoisonedAt(0, 0, 16000) {
		t.Fatal("practice lobby poisoned")
	}
}
func TestNeutralPickupsApplyAuthoredStatInsteadOfShield(t *testing.T) {
	cat := mustCatalog(t)
	w := arena(20, 20)
	p := fighter(0, 1, 600, 600)
	p.HP = 1000
	w.Players = []*Player{p}
	for _, name := range []string{"hit_points_level_up", "damage_level_up", "speed_level_up", "game_point"} {
		w.Items = append(w.Items, &Item{Definition: cat.Spec("items", name), Team: -1, X: 600, Y: 600, TimeMsLeft: 5000})
	}
	TickEffects(w)
	if len(w.Items) != 0 || p.MaxHP != 4140 || p.HP != 1540 || p.BaseDamageAddPct != 10 || p.SpeedAdd != 2 || p.GamePoints != 1 || p.ConsumableShield != 0 {
		t.Fatalf("pickups wrong: %+v", p)
	}
	TickEffects(w)
	if p.MaxHP != 4140 {
		t.Fatal("pickup applied twice")
	}
}
