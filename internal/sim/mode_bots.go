package sim

import (
	"math"

	"github.com/freaks/openfreaks/internal/protocol"
)

// Боты режимов 26.09. В Кошмаре общий ИИ бесполезен: врагов «не видно», а
// роль решает всё. Выжившие носят предохранители и берегут фонарь, детектив
// стреляет только в того, кого видел с ножом, убийца охотится за отбившимися
// и бьёт, только когда никто не смотрит.

// modeBot — true, если режим повёл бота сам.
func modeBot(w *World, bot *Player, tick int) bool {
	switch {
	case w.modes.nightmare != nil:
		nightmareBot(w, bot, tick)
		return true
	case w.modes.infection != nil:
		return infectionBot(w, bot, tick)
	}
	return false
}

func botWander(w *World, bot *Player, tick int, every int) navPoint {
	b := bot.Brain
	if b.Goal == (navPoint{}) || tick >= b.ReplanAt+every || Dist(bot.X, bot.Y, b.Goal.x, b.Goal.y) < 250 {
		x, y := randomOpenPoint(w, bot.CollisionRadius, nil, 0)
		b.Goal = navPoint{x, y}
		b.ReplanAt = tick
	}
	return b.Goal
}

func nightmareBotLight(st *nmPlayer, moving bool) {
	switch {
	case st.lightLocked || st.battery < nmBatteryMs/8:
		st.lightWanted = false
	case moving && st.battery > nmBatteryMs*2/3:
		st.lightWanted = true
	}
}

func nightmareBot(w *World, bot *Player, tick int) {
	s := w.nm()
	st := s.of(bot)
	if st == nil {
		return
	}
	nightmareBotLight(st, true)
	if st.windup > 0 {
		return
	}
	var goal navPoint
	if st.role == nmKiller {
		goal = nightmareKillerGoal(w, bot, st, tick)
	} else {
		goal = nightmareSurvivorGoal(w, bot, st, tick)
	}
	botNavigate(w, bot, goal, tick)
}

func nightmareSurvivorGoal(w *World, bot *Player, st *nmPlayer, tick int) navPoint {
	s := w.nm()
	killer := s.killerPlayer(w)
	if st.knowsKiller && killer != nil && killer.Alive {
		d := Dist(bot.X, bot.Y, killer.X, killer.Y)
		seen := w.nightmareVisibility(bot, killer) == 15 || d <= 900
		if st.role == nmDetective && s.detective == bot.Index && s.hasBullet && seen && d <= nmBulletRange*0.85 &&
			shotClear(w, bot.X, bot.Y, killer.X, killer.Y, &Skill{}) && (tick+bot.Index)%8 == 0 {
			TryCast(w, bot, nil, protocol.PlayerAction{X: int(killer.X - bot.X), Y: int(killer.Y - bot.Y)}, false)
			return navPoint{bot.X, bot.Y}
		}
		if seen && d < 1800 && (st.role != nmDetective || !s.hasBullet) {
			return navPoint{bot.X + (bot.X-killer.X)/math.Max(1, d)*900, bot.Y + (bot.Y-killer.Y)/math.Max(1, d)*900}
		}
		if st.role == nmDetective && s.hasBullet {
			return navPoint{killer.X, killer.Y}
		}
	}
	if st.carried > 0 {
		return navPoint{s.boxX, s.boxY}
	}
	if s.revolver != nil && s.revolver.TimeMsLeft > 0 && s.detective < 0 {
		return navPoint{s.revolver.X, s.revolver.Y}
	}
	var best *Area
	bestD := math.MaxFloat64
	for _, f := range s.fuses {
		if f != nil && f.TimeMsLeft > 0 {
			if d := Dist(bot.X, bot.Y, f.X, f.Y); d < bestD {
				best, bestD = f, d
			}
		}
	}
	// Не все бегут за одним и тем же: половина бродит.
	if best != nil && (bot.Index%2 == 0 || bestD < 2400) {
		return navPoint{best.X, best.Y}
	}
	return botWander(w, bot, tick, 160)
}

func nightmareKillerGoal(w *World, bot *Player, st *nmPlayer, tick int) navPoint {
	s := w.nm()
	if s.lightsOn() {
		return botWander(w, bot, tick, 60)
	}
	// Цель — ближний отбившийся (рядом с ним никого).
	var target *Player
	best := math.MaxFloat64
	for _, v := range w.Players {
		if v == bot || !v.Alive || v.Abandoned {
			continue
		}
		company := 0
		for _, o := range w.Players {
			if o != v && o != bot && o.Alive && !o.Abandoned && Dist(o.X, o.Y, v.X, v.Y) < 1500 {
				company++
			}
		}
		d := Dist(bot.X, bot.Y, v.X, v.Y) + float64(company)*1800
		if d < best {
			target, best = v, d
		}
	}
	if target == nil {
		return botWander(w, bot, tick, 120)
	}
	d := Dist(bot.X, bot.Y, target.X, target.Y)
	if tick < w.IntroTicks+400 {
		return botWander(w, bot, tick, 160)
	}
	// Первые 20 секунд убийца «свой»: ходит с толпой и не бьёт.
	patient := tick < w.IntroTicks+400
	if !patient && d <= nmKnifeReach+float64(bot.CollisionRadius+target.CollisionRadius) && st.knifeCool == 0 {
		witnesses := 0
		for _, o := range w.Players {
			if o != bot && o != target && o.Alive && !o.Abandoned &&
				(w.nightmareVisibility(o, bot) == 15 || Dist(o.X, o.Y, bot.X, bot.Y) < 1500) {
				witnesses++
			}
		}
		if witnesses == 0 {
			TryCast(w, bot, nil, protocol.PlayerAction{X: int(target.X - bot.X), Y: int(target.Y - bot.Y)}, false)
			return navPoint{target.X, target.Y}
		}
	}
	// Далеко — люк; рядом со щитком и целью в группе — саботаж.
	if s.ventCool == 0 && d > 4500 {
		TryCast(w, bot, nil, protocol.PlayerAction{X: int(target.X - bot.X), Y: int(target.Y - bot.Y)}, true)
	}
	if s.sabotageCool == 0 && tick%40 == 0 {
		TryCast(w, bot, nil, protocol.PlayerAction{}, true)
	}
	return navPoint{target.X, target.Y}
}

// infectionBot: в карантине все бродят (врагов ещё нет), потом общий ИИ; а
// выживший без заражённых рядом бежит к ящику с припасами.
func infectionBot(w *World, bot *Player, tick int) bool {
	s := w.inf()
	st := s.of(bot)
	if st == nil {
		return false
	}
	if s.phase == infPhaseQuarantine {
		botNavigate(w, bot, botWander(w, bot, tick, 100), tick)
		return true
	}
	if st.zombie {
		return false
	}
	nearest := math.MaxFloat64
	for _, z := range w.Players {
		if zs := s.of(z); zs != nil && zs.zombie && z.Alive {
			nearest = min(nearest, Dist(bot.X, bot.Y, z.X, z.Y))
		}
	}
	if nearest < 1800 {
		return false
	}
	for _, c := range s.crates {
		if c != nil && c.TimeMsLeft > 0 && Dist(bot.X, bot.Y, c.X, c.Y) < 3000 {
			botNavigate(w, bot, navPoint{c.X, c.Y}, tick)
			return true
		}
	}
	return false
}
