package sim

import "math"

// Кролик (default_character) и его телевизор помех (rabbit_tv_character).
//
// Старые APK разбирают модели сервера строго: незнакомый тип навыка или
// звёздной силы роняет их загрузку. Поэтому в YAML у Кролика только прежние
// типы (attack, dummy, skill, attack_heal, override_minion_death_area_effect),
// а своё поведение он получает здесь — по дополнительным полям, которые
// клиент пропускает: rabbit_tv, rabbit_perk, requires_rabbit_tv,
// freeze_ignores_bosses, replaces_previous, is_boss.

const (
	// Передача в эфире 6 с, потом на экране снова «злые глаза».
	RabbitBroadcastTicks = 120
	// Передача 1: у врагов не работает автохил, пока она в эфире.
	RabbitAntihealTicks = RabbitBroadcastTicks
	// Передача 2: каждое число телевизора замедляет на 30% на секунду.
	RabbitBroadcastSlow      = 30
	RabbitBroadcastSlowTicks = 20

	rabbitChannelArea  = "rabbit_tv_channel_"
	rabbitSwitchArea   = "rabbit_tv_channel_switch"
	rabbitAntihealArea = "rabbit_antiheal_mark"
	rabbitOverclockFx  = "rabbit_tv_acceleration"
)

// isBoss — боссов не замедляют и не притягивают.
func isBoss(p *Player) bool {
	return p != nil && (p.Definition.Bool("is_boss") || p.Definition.String("name") == "karity_boss_character")
}

func isRabbitTV(p *Player) bool {
	return p != nil && p.Owner != nil && p.Definition.Bool("rabbit_tv")
}

// rabbitTV — живой телевизор этого Кролика (он всегда один).
func rabbitTV(w *World, owner *Player) *Player {
	for _, m := range w.Minions {
		if m.Alive && m.Owner == owner && m.Definition.Bool("rabbit_tv") {
			return m
		}
	}
	return nil
}

func hasStatus(w *World, p *Player, kind string) bool {
	for _, e := range p.Effects {
		if e.Kind == kind && e.Expires > w.TickNum {
			return true
		}
	}
	return false
}

// followingArea продлевает зону-маркер над персонажем или ставит новую.
func followingArea(w *World, owner *Player, name string, ms int) *Area {
	for _, a := range w.Areas {
		if a.OwnerIndex == owner.ObjectIndex && a.TimeMsLeft > 0 && a.Definition.String("name") == name {
			if ms > 0 {
				a.TimeMsLeft = ms
			}
			return a
		}
	}
	a := SpawnArea(w, owner, name, owner.X, owner.Y, 0, 0, true, false)
	if a != nil && ms > 0 {
		a.TimeMsLeft = ms
	}
	return a
}

func dropFollowingArea(w *World, owner *Player, name string) {
	for _, a := range w.Areas {
		if a.OwnerIndex == owner.ObjectIndex && a.Definition.String("name") == name {
			a.TimeMsLeft = 0
		}
	}
}

// rabbitTVSpawned — телевизор только что встал на карту.
func rabbitTVSpawned(w *World, tv *Player) {
	tv.BaseSpeed = 0
	star := tv.Owner.StarPower
	if star.String("rabbit_perk") == "tv_pull" {
		followingArea(w, tv, star.String("area_effect_name", "rabbit_tv_pull_field"), 0)
	}
}

// rabbitSwitchChannel — рывок «Ускорение»: 1 → 2 → 1 …, каждая передача 6 с.
func rabbitSwitchChannel(w *World, owner *Player) bool {
	tv := rabbitTV(w, owner)
	if tv == nil {
		return false
	}
	tv.TvLastChannel = tv.TvLastChannel%2 + 1
	tv.TvChannel = tv.TvLastChannel
	tv.TvChannelTicks = RabbitBroadcastTicks
	dropFollowingArea(w, tv, rabbitChannelArea+"1")
	dropFollowingArea(w, tv, rabbitChannelArea+"2")
	name := rabbitChannelArea + "1"
	if tv.TvChannel == 2 {
		name = rabbitChannelArea + "2"
	}
	followingArea(w, tv, name, RabbitBroadcastTicks*TickMs)
	SpawnArea(w, tv, rabbitSwitchArea, tv.X, tv.Y, 0, 0, false, false)
	if tv.TvChannel == 1 {
		for _, v := range w.Players {
			if !v.Alive || v.Abandoned || v.IsBall() || v.Team == owner.Team || v.HeroType != "hero" {
				continue
			}
			AddStatus(w, v, "no_regen", 1, RabbitAntihealTicks, tv.ObjectIndex)
			followingArea(w, v, rabbitAntihealArea, RabbitAntihealTicks*TickMs)
		}
	}
	return true
}

// rabbitOnHit — «малая пассивка»: попадание Кролика (по врагу или боссу)
// разгоняет его телевизор: +25% к скорости перезарядки за удар, до +75%, 3 с.
func rabbitOnHit(w *World, rabbit *Player) {
	star := rabbit.StarPower
	if rabbit.Owner != nil || star.String("rabbit_perk") != "tv_overclock" {
		return
	}
	tv := rabbitTV(w, rabbit)
	if tv == nil {
		return
	}
	step := star.Int("reload_percent_per_hit", 25)
	value := step
	for _, e := range tv.Effects {
		if e.Kind == "reload" && e.Source == rabbit.ObjectIndex && e.Expires > w.TickNum {
			value = e.Value + step
		}
	}
	value = min(star.Int("max_reload_percent", 75), value)
	ticks := star.Int("ticks", 60)
	AddStatus(w, tv, "reload", value, ticks, rabbit.ObjectIndex)
	followingArea(w, tv, rabbitOverclockFx, ticks*TickMs)
}

// rabbitShotHit — число телевизора попало: на передаче 2 оно ещё и замедляет.
func rabbitShotHit(w *World, shooter, victim *Player) {
	if !isRabbitTV(shooter) || shooter.TvChannel != 2 || isBoss(victim) {
		return
	}
	AddStatus(w, victim, "speed", -RabbitBroadcastSlow, RabbitBroadcastSlowTicks, shooter.ObjectIndex)
}

// rabbitGlobalTV — телевизор бьёт всю карту сам, без своих чисел-снарядов.
func rabbitGlobalTV(p *Player) bool {
	return isRabbitTV(p) && p.Definition.Int("global_damage") > 0
}

// rabbitGlobalPulse — пока телевизор стоит, раз в интервал все враги на
// карте получают урон, где бы они ни были. Это урон ульты Кролика.
func rabbitGlobalPulse(w *World, tv *Player) {
	damage := tv.Definition.Int("global_damage")
	every := max(1, tv.Definition.Int("global_damage_interval_ms", 2000)/TickMs)
	if damage <= 0 || w.TickNum%int32(every) != 0 {
		return
	}
	for _, v := range w.Characters() {
		if v == tv || !v.Alive || v.Abandoned || v.IsBall() || v.Team == tv.Team {
			continue
		}
		applyDamage(w, tv, v, damage, true)
		rabbitShotHit(w, tv, v)
	}
}

// TickRabbit — эфир передач, урон по всей карте и «пассивка» (притяжение к телевизору).
func TickRabbit(w *World) {
	for _, tv := range w.Minions {
		if !tv.Alive || !isRabbitTV(tv) {
			continue
		}
		rabbitGlobalPulse(w, tv)
		if tv.TvChannelTicks > 0 {
			tv.TvChannelTicks--
			if tv.TvChannelTicks == 0 {
				tv.TvChannel = 0
			}
		}
		star := tv.Owner.StarPower
		if star.String("rabbit_perk") != "tv_pull" {
			continue
		}
		radius := float64(star.Int("radius", 1100))
		strength := float64(star.Int("strength", 7))
		deadZone := float64(star.Int("dead_zone", 220))
		for _, v := range w.Characters() {
			if !v.Alive || v.Abandoned || v.IsBall() || v.Team == tv.Team || isBoss(v) || v.BaseSpeed <= 0 || v.Charged {
				continue
			}
			dx, dy := tv.X-v.X, tv.Y-v.Y
			d := math.Hypot(dx, dy)
			if d > radius || d <= deadZone {
				continue
			}
			step := math.Min(strength, d-deadZone)
			movePlayer(w, v, dx/d*step, dy/d*step)
		}
	}
}
