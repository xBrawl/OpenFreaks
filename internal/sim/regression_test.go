package sim

import (
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

func arena(w, h int) *World {
	n := w * h
	return &World{Tiles: TileGrid{W: w, H: h, PixW: float64(w * 300), PixH: float64(h * 300),
		Blocked: make([]bool, n), BlocksProj: make([]bool, n), Destructible: make([]bool, n), Destroyed: make([]bool, n)},
		DurationTicks: 10000, RespawnTicks: 60, TeamCount: 2, MaxPlayers: 2, TeamPoints: make([]int, 2)}
}

func fighter(index, team int, x, y float64) *Player {
	return &Player{Index: index, ObjectIndex: index, Team: team, X: x, Y: y, ExactX: x, ExactY: y,
		SpawnX: x, SpawnY: y, HP: 3600, MaxHP: 3600, Alive: true, BaseSpeed: 700, CollisionRadius: 100,
		HeroType: "hero", Visibility: 15, DashModelID: -1, UltChargeMult: 100}
}

func TestSpawnShieldExpiresAndRespawnIsProtected(t *testing.T) {
	w := arena(20, 20)
	p := fighter(0, 0, 600, 600)
	p.SpawnShield = true
	w.Players = []*Player{p}
	for i := 0; i < 50; i++ {
		w.Advance()
	}
	DealDamage(w, nil, p, 4000)
	if p.Alive {
		t.Fatal("spawn shield made player permanently invulnerable")
	}
	for i := 0; i < 60; i++ {
		w.Advance()
	}
	if !p.Alive || !p.SpawnShield {
		t.Fatal("respawn must restore temporary protection")
	}
	for i := 0; i < 50; i++ {
		w.Advance()
	}
	if p.SpawnShield {
		t.Fatal("respawn protection never expires")
	}
}

func TestDamageDoesNotStopWalkingOrCountOverkill(t *testing.T) {
	w := arena(20, 20)
	a, v := fighter(0, 0, 600, 600), fighter(1, 1, 900, 600)
	v.ApplyWalk(1500, 600)
	DealDamage(w, a, v, 100)
	if !v.HasMoveTarget {
		t.Fatal("ordinary damage cancelled walking")
	}
	DealDamage(w, a, v, 10000)
	if a.DamageDealt != 3600 {
		t.Fatalf("damage includes overkill: %d", a.DamageDealt)
	}
}

func TestCollisionUsesCharacterRadius(t *testing.T) {
	w := arena(10, 10)
	w.Tiles.Blocked[3*10+3] = true
	if w.Tiles.CanOccupy(850, 1000, 100) {
		t.Fatal("character radius overlaps wall")
	}
	if !w.Tiles.CanOccupy(799, 1000, 100) {
		t.Fatal("open space rejected")
	}
}

func TestProjectileUsesProjectileCatalogID(t *testing.T) {
	cat := mustCatalog(t)
	for _, def := range cat.Skills {
		if def.Attack == nil || def.Attack.Projectile == "" {
			continue
		}
		projectile := cat.Projectiles[def.Attack.Projectile]
		if projectile == nil {
			continue
		}
		w := arena(20, 20)
		p := fighter(0, 0, 600, 600)
		sk := toRuntimeSkill(cat, def)
		fireProjectiles(w, p, &Execution{Skill: sk, Angle: 0}, sk)
		if got := w.Projectiles[0].ModelID; int(got) != projectile.Id {
			t.Errorf("%s: projectile ID=%d, want %d", def.Name, got, projectile.Id)
		}
	}
}

func TestAutomaticChargeDoesNotAllowEmptyUltimate(t *testing.T) {
	w := arena(20, 20)
	p := fighter(0, 0, 600, 600)
	p.ChargeAuto = 10
	sk := &Skill{Behaviour: "shield"}
	if TryCast(w, p, sk, protocol.PlayerAction{}, true) {
		t.Fatal("auto-charge allowed an uncharged ultimate")
	}
}

func TestExpiredProjectilesAreReclaimed(t *testing.T) {
	w := arena(20, 20)
	w.Projectiles = []*Projectile{{Alive: false, TerminalTicks: 1}}
	TickProjectiles(w)
	TickProjectiles(w)
	if len(w.Projectiles) != 0 {
		t.Fatal("expired projectiles retained in tick loop")
	}
}
