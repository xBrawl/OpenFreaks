package sim

import (
	"math"
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

// heroDuel — герой с рывком (команда 0) против Вульфа (команда 1) посреди тестовой арены.
func heroDuel(t *testing.T, hero, dash string) (*World, *Player, *Player) {
	t.Helper()
	cat := mustCatalog(t)
	dashID := -1
	if dash != "" {
		spec := cat.Spec("dashes", dash)
		if !spec.Exists() {
			t.Fatalf("нет рывка %s", dash)
		}
		dashID = spec.ID
	}
	w, err := BuildWorld(cat, BuildParams{
		LocationID: cat.LocByName["experimental_test_arena"].Id,
		Players: []BuildPlayer{
			{PlayerID: 1, Index: 0, Team: 0, CharacterID: cat.CharByName[hero].Id, DashID: dashID, UltUpgradeID: -1, StarPowerID: -1},
			{PlayerID: 2, Index: 1, Team: 1, CharacterID: cat.CharByName["wolf_character"].Id, DashID: -1, UltUpgradeID: -1, StarPowerID: -1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.IntroTicks = 0
	p, enemy := w.Players[0], w.Players[1]
	cx, cy := w.Tiles.PixW/2, w.Tiles.PixH/2
	place(p, cx, cy)
	place(enemy, cx+3000, cy)
	p.SpawnShield, enemy.SpawnShield = false, false
	return w, p, enemy
}

func advanceUntil(w *World, ticks int, done func() bool) {
	for i := 0; i < ticks && !done(); i++ {
		w.Advance()
	}
}

// Телепорт Медузы приземляется туда, куда целились, а не всегда на максимум,
// и приземление бьёт врагов рядом.
func TestJellyfishTeleportLandsWhereAimedAndSlams(t *testing.T) {
	w, medusa, wolf := heroDuel(t, "jellyfish_character", "jellyfish_dash")
	startX, startY := medusa.X, medusa.Y
	aim := wolf.X - medusa.X - 400
	if !TryDash(w, medusa, protocol.PlayerAction{X: int(aim), Y: 0}) {
		t.Fatal("телепорт не кастуется")
	}
	hp := wolf.HP
	advanceUntil(w, 20, func() bool { return Dist(startX, startY, medusa.X, medusa.Y) > 100 })
	moved := Dist(startX, startY, medusa.X, medusa.Y)
	if math.Abs(moved-aim) > 200 {
		t.Fatalf("телепорт на %.0f вместо %.0f, куда целились", moved, aim)
	}
	w.Advance()
	if wolf.HP >= hp {
		t.Fatalf("приземление рядом с Вульфом не нанесло урона: %d → %d", hp, wolf.HP)
	}

	// Прицел дальше дальности — ровно на дальность (20 единиц = 2000).
	w2, medusa2, _ := heroDuel(t, "jellyfish_character", "jellyfish_dash")
	sx, sy := medusa2.X, medusa2.Y
	if !TryDash(w2, medusa2, protocol.PlayerAction{X: 0, Y: -20000}) {
		t.Fatal("телепорт не кастуется")
	}
	advanceUntil(w2, 20, func() bool { return Dist(sx, sy, medusa2.X, medusa2.Y) > 100 })
	if moved := Dist(sx, sy, medusa2.X, medusa2.Y); moved > 2000+150 {
		t.Fatalf("телепорт на %.0f — дальше дальности 2000", moved)
	}
}

// Автоприцел по бегущей цели берёт упреждение, по стоящей — нет.
func TestAutoAimLeadsMovingTarget(t *testing.T) {
	w, wolfAttacker, target := heroDuel(t, "angel_wind_character", "")
	weapon := wolfAttacker.Skills[0]
	place(target, wolfAttacker.X+1200, wolfAttacker.Y)
	a := protocol.PlayerAction{X: 1, Y: 0, AutoAttack: true, TargetFound: true, TargetObjectID: 1000000 + target.ObjectIndex}
	lead := float64(weapon.ProjSpeed)
	if lead <= 0 {
		t.Fatal("у оружия нет скорости снаряда")
	}
	_, sy := aimTarget(w, wolfAttacker, a, weapon.RangePx, targetAim{absolute: true, leadSpeed: lead})
	if math.Abs(sy-target.Y) > 1 {
		t.Fatalf("по стоящей цели упреждение не нужно: y %.0f вместо %.0f", sy, target.Y)
	}
	target.TargetX, target.TargetY, target.HasMoveTarget = target.X, target.Y+3000, true
	_, my := aimTarget(w, wolfAttacker, a, weapon.RangePx, targetAim{absolute: true, leadSpeed: lead})
	if my <= target.Y+50 || my > target.Y+leadMaxPx+1 {
		t.Fatalf("по бегущей вниз цели прицел должен уйти вниз не дальше %d: %.0f → %.0f", leadMaxPx, target.Y, my)
	}
}

// Автоприцел по титану бьёт в ближний хитбокс, а не в центр под ящиком.
func TestAutoAimTitanTargetsNearestHitCircle(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titan := titanPhase(t, w, ball)
	shooter := w.Players[0]
	shooter.X, shooter.Y = titan.X-2880, titan.Y-2000
	a := protocol.PlayerAction{X: 1, Y: 0, AutoAttack: true, TargetFound: true, TargetObjectID: 1000000 + titan.ObjectIndex}
	x, y := aimTarget(w, shooter, a, 9000, targetAim{absolute: true})
	if bodyDist(titan, x, y) > 0 {
		t.Fatalf("прицел (%.0f, %.0f) мимо тела титана", x-titan.X, y-titan.Y)
	}
	if Dist(x, y, titan.X-2880, titan.Y-420) > 560 {
		t.Fatalf("прицел (%.0f, %.0f) не в голове, хотя боец стоит у головы", x-titan.X, y-titan.Y)
	}
}
