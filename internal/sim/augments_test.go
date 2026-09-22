package sim

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"testing"
)

func augmented(t *testing.T, names ...string) (*World, *Player, *Player, *Player) {
	t.Helper()
	cat := mustCatalog(t)
	w := arena(30, 30)
	w.Catalog = cat
	ids := []int32{}
	for _, name := range names {
		d := cat.Spec("augments", name)
		if !d.Exists() {
			t.Fatal(name)
		}
		ids = append(ids, int32(d.ID))
	}
	p := buildPlayer(cat, w, BuildPlayer{Index: 0, Team: 0, CharacterID: 2, AugmentIDs: ids, DashID: -1, StarPowerID: -1, UltUpgradeID: -1}, cat.CharByName["wolf_character"])
	p.X, p.Y = 600, 600
	p.SpawnShield = false
	ally := fighter(1, 0, 700, 600)
	enemy := fighter(2, 1, 900, 600)
	ally.World = w
	enemy.World = w
	w.Players = []*Player{p, ally, enemy}
	return w, p, ally, enemy
}
func TestEveryCatalogAugmentBuildsAndRunsWithAllHeroes(t *testing.T) {
	cat := mustCatalog(t)
	if len(cat.Definitions["augments"]) != len(SupportedAugments) {
		t.Fatal("augment coverage changed")
	}
	for _, d := range cat.Definitions["augments"] {
		if !SupportedAugments[d.String("name")] {
			t.Fatalf("missing %s", d.String("name"))
		}
		for _, hero := range cat.Heroes() {
			w := arena(30, 30)
			w.Catalog = cat
			p := buildPlayer(cat, w, BuildPlayer{Index: 0, AugmentIDs: []int32{int32(d.ID)}, DashID: -1, StarPowerID: -1, UltUpgradeID: -1}, hero)
			p.X, p.Y = 1000, 1000
			p.SpawnShield = false
			p.IsBot = true
			ally, enemy := fighter(1, 0, 1100, 1100), fighter(2, 1, 1300, 1100)
			ally.World = w
			enemy.World = w
			w.Players = []*Player{p, ally, enemy}
			for tick := 0; tick < 220; tick++ {
				if tick%40 == 0 {
					p.UltCharge = 4000
					TryCast(w, p, p.Skills[1], protocol.PlayerAction{X: 500}, true)
					DealDamage(w, enemy, p, 100)
					DealDamage(w, p, enemy, 100)
					Heal(p, 60)
				}
				w.Advance()
				_ = EncodeSnapshot(w, 0, 0)
			}
			if len(p.AugmentIDs) != 1 || p.MaxHP <= 0 || p.HP < 0 {
				t.Fatalf("invalid %s / %s", d.String("name"), hero.Name)
			}
		}
	}
}
func TestAugmentDefenseDoesNotRecursivelyRedirectOrMultiplyDamage(t *testing.T) {
	w, p, ally, enemy := augmented(t, "sacrificial_shield")
	ApplyAugments(w, ally, p.AugmentIDs)
	enemy.DamageAddPct = 100
	beforeP, beforeAlly := p.HP, ally.HP
	DealDamage(w, enemy, ally, 100)
	if beforeAlly-ally.HP != 120 || beforeP-p.HP != 60 {
		t.Fatalf("redirect damage %d/%d expected 120/60", beforeAlly-ally.HP, beforeP-p.HP)
	}
}
func TestAugmentSecondaryDamageCannotLoopVampireAndHealing(t *testing.T) {
	w, p, _, enemy := augmented(t, "vampire", "death_cycle", "corrosion")
	p.HP = 1000
	DealDamage(w, p, enemy, 1000)
	if p.HP != 1150 || enemy.HP != 2578 {
		t.Fatalf("unexpected proc chain: hp=%d enemy=%d", p.HP, enemy.HP)
	}
	before := enemy.HP
	w.TickNum = 20
	TickAugments(w)
	if enemy.HP != before-500 || p.HP != 1150 {
		t.Fatal("DOT retriggered itself or vampire")
	}
}
func TestAugmentTeamTriggersAndUltimateGates(t *testing.T) {
	w, p, ally, enemy := augmented(t, "back_to_basics", "simultaneous_attack", "crossfire")
	p.UltCharge = 4000
	if TryCast(w, p, p.Skills[1], protocol.PlayerAction{X: 300}, true) {
		t.Fatal("disabled ultimate cast")
	}
	DealDamage(w, ally, enemy, 50)
	w.TickNum = 3
	DealDamage(w, p, enemy, 50)
	TickEffects(w)
	if p.DamageAddPct != 50 || enemy.EffectsSpeedAdd != -40 {
		t.Fatal("team combo did not trigger")
	}
	w, p, ally, _ = augmented(t, "cannot_touch", "troublemaker", "dualism")
	ApplyAugments(w, ally, []int32{int32(w.Catalog.Spec("augments", "echo_of_ultimate").ID)})
	p.HP = 1000
	p.UltCharge = 4000
	if !TryCast(w, p, p.Skills[1], protocol.PlayerAction{X: 300}, true) {
		t.Fatal("ultimate rejected")
	}
	if p.HP != 1600 || ally.UltCharge != 1600 || p.BaseDamageAddPct != 2 {
		t.Fatal("ultimate augment effects missing")
	}
	DealDamage(w, nil, p, 1000)
	if p.HP != 1600 {
		t.Fatal("ultimate immunity missing")
	}
}
func TestSymbiosisDoesNotAmplifySharedBuffsForever(t *testing.T) {
	w, p, ally, _ := augmented(t, "symbiosis", "brute_force")
	ApplyAugments(w, ally, []int32{int32(w.Catalog.Spec("augments", "symbiosis").ID)})
	for i := 0; i < 100; i++ {
		w.TickNum++
		TickEffects(w)
		TickAugments(w)
	}
	if p.DamageAddPct != 20 || ally.DamageAddPct != 7 {
		t.Fatal("shared buffs fed back into each other")
	}
}
