package sim

import (
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

// rabbitFixture — Кролик (команда 0) против героя (команда 1) на тестовой арене.
func rabbitFixture(t *testing.T, star, ult string) (*World, *Player, *Player) {
	t.Helper()
	cat := mustCatalog(t)
	rabbit := cat.CharByName["default_character"]
	if rabbit == nil || !rabbit.IsHero() || rabbit.HeroType != "hero" {
		t.Fatal("Кролик (default_character) должен быть героем")
	}
	id := func(kind, name string) int {
		if name == "" {
			return -1
		}
		spec := cat.Spec(kind, name)
		if !spec.Exists() {
			t.Fatalf("нет %s %s", kind, name)
		}
		return spec.ID
	}
	w, err := BuildWorld(cat, BuildParams{
		LocationID: cat.LocByName["experimental_test_arena"].Id,
		Players: []BuildPlayer{
			{PlayerID: 1, Index: 0, Team: 0, CharacterID: rabbit.Id, DashID: id("dashes", "rabbit_dash"), StarPowerID: id("star_powers", star), UltUpgradeID: id("ultimate_upgrades", ult)},
			{PlayerID: 2, Index: 1, Team: 1, CharacterID: cat.CharByName["wolf_character"].Id, DashID: -1, UltUpgradeID: -1, StarPowerID: -1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.IntroTicks = 0
	r, enemy := w.Players[0], w.Players[1]
	cx, cy := w.Tiles.PixW/2, w.Tiles.PixH/2
	place(r, cx, cy)
	place(enemy, cx+1200, cy)
	r.SpawnShield, enemy.SpawnShield = false, false
	return w, r, enemy
}

func place(p *Player, x, y float64) {
	x, y = p.World.Tiles.FindOpen(x, y, p.CollisionRadius)
	p.X, p.Y, p.ExactX, p.ExactY = x, y, x, y
}

func aimAt(from, to *Player) protocol.PlayerAction {
	return protocol.PlayerAction{X: int(to.X - from.X), Y: int(to.Y - from.Y)}
}

// deployTV бросает телевизор под ноги врагу и ждёт, пока тот встанет.
func deployTV(t *testing.T, w *World, r, at *Player) *Player {
	t.Helper()
	r.UltCharge = 4000
	ult := skillNamed(r, "rabbit_tv")
	if ult == nil {
		t.Fatal("у Кролика нет ульты rabbit_tv")
	}
	if !TryCast(w, r, ult, protocol.PlayerAction{X: int(at.X - r.X - 400), Y: int(at.Y - r.Y)}, true) {
		t.Fatal("ульта не кастуется")
	}
	for i := 0; i < 80; i++ {
		w.Advance()
		if tv := rabbitTV(w, r); tv != nil {
			return tv
		}
	}
	t.Fatal("телевизор так и не появился")
	return nil
}

func TestRabbitLaserSlowsButNotBosses(t *testing.T) {
	w, r, enemy := rabbitFixture(t, "", "")
	laser := skillNamed(r, "rabbit_laser")
	if laser == nil {
		t.Fatal("у Кролика нет лазера")
	}
	hp := enemy.HP
	TryCast(w, r, laser, aimAt(r, enemy), false)
	for i := 0; i < 10; i++ {
		w.Advance()
	}
	if enemy.HP >= hp {
		t.Fatal("лазер не попал")
	}
	if !enemy.Slowdown {
		t.Fatal("лазер не замедлил врага")
	}

	boss := &Player{Definition: w.Catalog.Spec("characters", "karity_boss_character")}
	if !isBoss(boss) {
		t.Fatal("Карити не считается боссом")
	}
}

func TestRabbitTVShootsNumbersAndIsSingle(t *testing.T) {
	w, r, enemy := rabbitFixture(t, "", "")
	tv := deployTV(t, w, r, enemy)
	if tv.BaseSpeed != 0 {
		t.Fatalf("телевизор должен стоять, скорость %d", tv.BaseSpeed)
	}
	hp := enemy.HP
	for i := 0; i < 60; i++ {
		enemy.HP = max(enemy.HP, 1)
		w.Advance()
	}
	if enemy.HP >= hp {
		t.Fatal("телевизор не стреляет")
	}

	// Второй телевизор выгоняет первый.
	second := deployTV2(t, w, r, enemy, tv)
	count := 0
	for _, m := range w.Minions {
		if m.Alive && isRabbitTV(m) {
			count++
		}
	}
	if count != 1 || tv.Alive || !second.Alive {
		t.Fatalf("живых телевизоров %d, старый жив=%v", count, tv.Alive)
	}
}

func deployTV2(t *testing.T, w *World, r, at, old *Player) *Player {
	t.Helper()
	r.UltCharge = 4000
	for _, s := range r.Skills {
		s.CoolLeft = 0
	}
	if !TryCast(w, r, skillNamed(r, "rabbit_tv"), protocol.PlayerAction{X: int(at.X - r.X - 400), Y: int(at.Y - r.Y)}, true) {
		t.Fatal("вторая ульта не кастуется")
	}
	for i := 0; i < 80; i++ {
		w.Advance()
		if tv := rabbitTV(w, r); tv != nil && tv != old {
			return tv
		}
	}
	t.Fatal("второй телевизор не появился")
	return nil
}

func TestRabbitDashNeedsTVAndSwitchesBroadcasts(t *testing.T) {
	w, r, enemy := rabbitFixture(t, "", "")
	if TryDash(w, r, protocol.PlayerAction{}) {
		t.Fatal("«Ускорение» сработало без телевизора")
	}
	tv := deployTV(t, w, r, enemy)

	// Передача 1: автохил врага не работает.
	if !TryDash(w, r, protocol.PlayerAction{}) {
		t.Fatal("«Ускорение» не сработало при живом телевизоре")
	}
	if tv.TvChannel != 1 || !hasStatus(w, enemy, "no_regen") {
		t.Fatalf("передача %d, no_regen=%v", tv.TvChannel, hasStatus(w, enemy, "no_regen"))
	}
	enemy.HP = enemy.MaxHP / 2
	enemy.LastCombatTick = w.TickNum - RegenDelayTicks - 1
	hp := enemy.HP
	place(enemy, 200, 200) // подальше от телевизора
	for i := 0; i < 40; i++ {
		w.Advance()
	}
	if enemy.HP > hp {
		t.Fatal("автохил работает на передаче 1")
	}

	// Передача 2: числа замедляют.
	r.DashCooldown = 0
	if !TryDash(w, r, protocol.PlayerAction{}) || tv.TvChannel != 2 {
		t.Fatalf("вторая передача не включилась: %d", tv.TvChannel)
	}
	victim := &Player{Alive: true, Team: 1, Definition: w.Catalog.Spec("characters", "wolf_character")}
	rabbitShotHit(w, tv, victim)
	if len(victim.Effects) != 1 || victim.Effects[0].Value != -RabbitBroadcastSlow {
		t.Fatalf("число на передаче 2 не замедлило: %+v", victim.Effects)
	}

	// Эфир кончается — экран снова по умолчанию.
	for i := 0; i < RabbitBroadcastTicks+1; i++ {
		w.Advance()
	}
	if tv.Alive && tv.TvChannel != 0 {
		t.Fatalf("передача %d не закончилась", tv.TvChannel)
	}
}

func TestRabbitOverclockSpeedsUpTV(t *testing.T) {
	w, r, enemy := rabbitFixture(t, "rabbit_star_power_1", "")
	tv := deployTV(t, w, r, enemy)
	for i := 0; i < 4; i++ {
		rabbitOnHit(w, r)
	}
	w.Advance()
	if tv.ReloadAddPct != 75 {
		t.Fatalf("разгон %d%%, ждали 75%%", tv.ReloadAddPct)
	}
}

func TestRabbitPullDragsEnemiesToTV(t *testing.T) {
	w, r, enemy := rabbitFixture(t, "rabbit_star_power_2", "")
	tv := deployTV(t, w, r, enemy)
	place(enemy, tv.X+900, tv.Y)
	before := Dist(tv.X, tv.Y, enemy.X, enemy.Y)
	enemy.HasMoveTarget = false
	for i := 0; i < 10; i++ {
		enemy.HP = enemy.MaxHP
		w.Advance()
	}
	if after := Dist(tv.X, tv.Y, enemy.X, enemy.Y); after >= before-30 {
		t.Fatalf("не притянуло: было %.0f, стало %.0f", before, after)
	}
}

func TestRabbitBroadcastUpgradeExplodes(t *testing.T) {
	w, r, enemy := rabbitFixture(t, "", "rabbit_ultimate_upgrade_1")
	tv := deployTV(t, w, r, enemy)
	place(enemy, tv.X+300, tv.Y)
	hp := enemy.HP
	charge := r.UltCharge
	Kill(w, nil, tv)
	for i := 0; i < 3; i++ {
		w.Advance()
	}
	if hp-enemy.HP < 1500 {
		t.Fatalf("взрыв телевизора снял %d", hp-enemy.HP)
	}
	// Урон телевизора заряжает следующий телевизор в полную силу: 1600 × 100%.
	if gained := r.UltCharge - charge; gained < 1500 {
		t.Fatalf("взрыв зарядил ульту только на %d", gained)
	}
}

func TestRabbitIsLastHero(t *testing.T) {
	heroes := mustCatalog(t).Heroes()
	if heroes[len(heroes)-1].Name != "default_character" || heroes[0].Name == "default_character" {
		t.Fatal("новые аккаунты не должны начинать Кроликом")
	}
}

func TestRabbitStartsWithTVReadyAndTVHitsChargeFully(t *testing.T) {
	w, r, enemy := rabbitFixture(t, "", "")
	if r.UltCharge != 4000 {
		t.Fatalf("ульта Кролика на старте %d, ждали 4000", r.UltCharge)
	}
	if enemy.UltCharge != 0 {
		t.Fatalf("у волка ульта на старте %d", enemy.UltCharge)
	}
	r.UltCharge = 0
	tv := &Player{Owner: r, Alive: true}
	applyDamage(w, tv, enemy, 400, true)
	if r.UltCharge != 400 {
		t.Fatalf("400 урона телевизора дали %d заряда, ждали 400", r.UltCharge)
	}
}
