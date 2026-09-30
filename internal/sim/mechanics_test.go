package sim

import (
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"math"
	"testing"
)

func spec(v map[string]any) content.Spec { return content.Spec{ID: 1, Value: v} }
func TestFastProjectileSweepsTargetsBeforeWalls(t *testing.T) {
	for _, wall := range []bool{false, true} {
		w := arena(20, 20)
		a, b := fighter(0, 0, 600, 750), fighter(1, 1, 1700, 750)
		w.Players = []*Player{a, b}
		if wall {
			w.Tiles.BlocksProj[2*20+4] = true
		}
		spawnProjectile(w, a, spec(map[string]any{"speed": 30000, "radius": 30}), 0, 400, 3000, 3000, 750, false, 0)
		TickProjectiles(w)
		want := 3200
		if wall {
			want = 3600
		}
		if b.HP != want {
			t.Fatalf("wall=%v hp=%d want=%d", wall, b.HP, want)
		}
	}
}
func TestProjectilePiercesOnlyDestructibleWalls(t *testing.T) {
	w := arena(20, 20)
	a, b := fighter(0, 0, 600, 750), fighter(1, 1, 1700, 750)
	w.Players = []*Player{a, b}
	w.Tiles.BlocksProj[44] = true
	w.Tiles.Blocked[44] = true
	w.Tiles.Destructible[44] = true
	spawnProjectile(w, a, spec(map[string]any{"speed": 30000, "pierces_environment": true}), 0, 400, 3000, 3000, 750, false, 0)
	TickProjectiles(w)
	if !w.Tiles.Destroyed[44] || b.HP != 3200 {
		t.Fatal("destructible wall or victim not hit")
	}
}
func TestDashMovesOverTimeAndConsecutiveUltimatesExpire(t *testing.T) {
	cat := mustCatalog(t)
	w := arena(30, 30)
	w.Catalog = cat
	p := buildPlayer(cat, w, BuildPlayer{Index: 0, DashID: -1, UltUpgradeID: -1, StarPowerID: -1}, cat.CharByName["wolf_character"])
	p.X, p.Y, p.ExactX, p.ExactY = 600, 600, 600, 600
	w.Players = []*Player{p}
	p.UltCharge = 4000
	if !TryCast(w, p, p.Skills[1], protocol.PlayerAction{X: 1200}, true) {
		t.Fatal("cast rejected")
	}
	if p.X != 600 || p.ConsecutiveLeft != 1 || p.UltCharge != 4000 {
		t.Fatal("charge teleported or second usage missing")
	}
	w.Advance()
	if math.Abs(p.X-705) > 1 {
		t.Fatalf("first step %f; want speed/20", p.X)
	}
	for i := 0; i < 110; i++ {
		w.Advance()
	}
	if p.ConsecutiveLeft != 0 || p.UltCharge != 0 {
		t.Fatal("consecutive window did not expire")
	}
}
func TestCastDelayStunAndSharedVolley(t *testing.T) {
	w := arena(20, 20)
	a, b := fighter(0, 0, 600, 750), fighter(1, 1, 950, 750)
	w.Players = []*Player{a, b}
	s := &Skill{Behaviour: "attack", AttackType: "direct", Damage: 100, RangePx: 600, ActiveMs: 50, MsBetween: 100, Definition: spec(map[string]any{"casting_time": 100, "execute_first_attack_immediately": true})}
	a.Skills = []*Skill{s}
	if !TryCast(w, a, s, protocol.PlayerAction{X: 100}, false) {
		t.Fatal("cast rejected")
	}
	w.Advance()
	if b.HP != 3600 {
		t.Fatal("damage before casting time")
	}
	w.Advance()
	if b.HP != 3500 {
		t.Fatalf("delayed attack did not fire: %d", b.HP)
	}
	AddStatus(w, a, "stun", 1, 10, b.ObjectIndex)
	w.Advance()
	if TryCast(w, a, s, protocol.PlayerAction{X: 100}, false) {
		t.Fatal("stun allowed attack")
	}
	shared := map[*Player]bool{}
	for i := 0; i < 3; i++ {
		pr := spawnProjectile(w, a, spec(map[string]any{"speed": 10000}), 0, 100, 2000, 2000, 750, false, 0)
		pr.SharedHits = shared
	}
	TickProjectiles(w)
	if b.HP != 3400 {
		t.Fatalf("one-volley damage applied more than once: %d", b.HP)
	}
}
func TestMinionsCreditOwnerWithoutScoringForMinionDeath(t *testing.T) {
	cat := mustCatalog(t)
	w := arena(30, 30)
	w.Catalog = cat
	a, b := fighter(0, 0, 600, 600), fighter(1, 1, 950, 600)
	w.Players = []*Player{a, b}
	m := SpawnMinion(w, a, "jellyfish_pet_character", 600, 600)
	if m == nil {
		t.Fatal("summon missing")
	}
	DealDamage(w, m, b, 9999)
	if a.DamageDealt != 3600 || a.Kills != 1 {
		t.Fatal("summon kill not credited to owner")
	}
	points := w.TeamPoints[1]
	DealDamage(w, b, m, 9999)
	if w.TeamPoints[1] != points {
		t.Fatal("summon death gave team points")
	}
}
func TestPassiveSpawnAndEffectsExpire(t *testing.T) {
	cat := mustCatalog(t)
	w := arena(30, 30)
	w.Catalog = cat
	a := fighter(0, 0, 600, 600)
	w.Players = []*Player{a}
	a.StarPower = cat.Spec("star_powers", "jellyfish_star_power_2").At("type")
	for i := 0; i < 251; i++ {
		w.Advance()
	}
	if a.SegmentTicks <= 0 || a.SegmentPercent <= 0 {
		t.Fatal("friendly passive summons failed to attach and grant their shield")
	}

	AddStatus(w, a, "speed", -20, 2, 1)
	w.Advance()
	if DisplayedSpeed(a) >= float64(a.BaseSpeed) {
		t.Fatal("slow missing")
	}
	w.Advance()
	w.Advance()
	if a.Slowdown {
		t.Fatal("slow never expired")
	}
}
