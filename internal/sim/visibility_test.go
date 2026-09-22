package sim

import "testing"

func TestBushHidingUsesTeamVisionAndRevealsOnCombat(t *testing.T) {
	w := arena(20, 20)
	w.TickNum = 100
	w.Tiles.HidesHero = make([]bool, 400)
	a, b, ally := fighter(0, 0, 600, 600), fighter(1, 1, 2400, 600), fighter(2, 0, 500, 600)
	w.Players = []*Player{a, b, ally}
	w.Tiles.HidesHero[2*20+8] = true
	if w.CanSee(a, b) || !w.CanSee(b, b) {
		t.Fatal("bush did not hide enemy or hid self")
	}
	a.IsBot = true
	if nearestEnemy(w, a) != nil {
		t.Fatal("bot targets hidden player")
	}
	ally.X = 2200
	if !w.CanSee(a, b) {
		t.Fatal("teammate did not reveal nearby enemy")
	}
	ally.X = 500
	DealDamage(w, a, b, 100)
	if !w.CanSee(a, b) {
		t.Fatal("damage did not reveal enemy")
	}
	w.TickNum += 41
	if w.CanSee(a, b) {
		t.Fatal("combat reveal never expires")
	}
	w.Tiles.Destroyed[48] = true
	if !w.CanSee(a, b) {
		t.Fatal("destroyed bush still hides enemy")
	}
}
func TestSpotUpgradeDoesNotConsumeNewMarkOrExplodeRecursively(t *testing.T) {
	cat := mustCatalog(t)
	w := arena(20, 20)
	w.Catalog = cat
	a, b := fighter(0, 0, 600, 600), fighter(1, 1, 900, 600)
	w.Players = []*Player{a, b}
	a.StarPower = cat.Spec("star_powers", "singer_star_power_2").At("type")
	a.Upgrade = cat.Spec("ultimate_upgrades", "singer_ultimate_upgrade_1").At("type")
	DealDamage(w, a, b, 100)
	if len(w.Areas) != 0 || len(b.Effects) == 0 {
		t.Fatal("new mark detonated on the same hit")
	}
	DealDamage(w, a, b, 100)
	if len(w.Areas) != 1 {
		t.Fatal("existing mark did not detonate")
	}
	TickEffects(w)
	if len(w.Areas) != 1 {
		t.Fatal("explosion spawned another explosion")
	}
}
