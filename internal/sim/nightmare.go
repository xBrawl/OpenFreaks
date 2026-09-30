package sim

import (
	"math"

	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
)

// КОШМАР В КАТАКОМБАХ. Восемь «Исследователей» в темноте; один из них —
// убийца, ещё один — детектив с револьвером на один патрон. Роли знает
// только сервер: каждому уходит лишь его собственная (блок режима в снимке).
//
// Свет здесь враг выживших. Сервер сам решает, кого кому видно
// (nightmareVisibility): свой фонарь — узкий конус, чужой фонарь виден издалека
// как луч, у убийцы — тусклое зрение вокруг себя и «пульс» бегущих рядом.
// Невидимого клиент не получает вовсе (только последнюю известную точку).

const (
	nmInnocent  = 0
	nmDetective = 1
	nmKiller    = 2
)

const (
	nmExplorerName = "nightmare_explorer_character"

	nmTouchRange      = 330.0  // вплотную видно и в темноте
	nmNightVision     = 1350.0 // зрение убийцы, 4.5 клетки
	nmBeamRange       = 1950.0 // свой фонарь, 6.5 клетки
	nmBeamHalfAngle   = 25     // конус 50 градусов
	nmBeamSeenFrom    = 6000.0 // чужой луч виден за 20 клеток
	nmGlowRange       = 600.0  // включённый фонарь подсвечивает и самого хозяина
	nmPulseRange      = 2400.0 // убийца чует бегущих в 8 клетках
	nmBatteryMs       = 15000  // 15 секунд непрерывного света
	nmBatteryRecharge = 150    // выключенный фонарь заряжается за 5 секунд
	nmBatteryUnlock   = 4500   // севший фонарь включается снова с 30%
	nmPanicRange      = 450.0
	nmPanicTicks      = 100 // 5 секунд толпой — помехи
	nmKnifeWindup     = 10  // 0.5 секунды замаха
	nmKnifeCooldown   = 60
	nmKnifeReach      = 260.0
	nmBulletRange     = 2400
	nmGunReload       = 200 // промах — 10 секунд на перезарядку
	nmVentReach       = 260.0
	nmVentCooldown    = 240
	nmPanelReach      = 300.0
	nmSabotageCool    = 700 // 35 секунд между саботажами
	nmBlackoutTicks   = 120 // «короткое замыкание»: 6 секунд без света
	nmGateTicks       = 160 // решётки на 8 секунд
	nmPickupReach     = 220.0
	nmBoxReach        = 380.0
	nmFusesNeeded     = 4
	nmInsertTicks     = 40     // вставить предохранитель — 2 секунды стоять у щитка
	nmFusesAfter      = 1200   // предохранители выпадают через минуту после старта
	nmFuseSparkRange  = 3600.0 // искрящий предохранитель убийца чует за 12 клеток
	nmLightsWinTicks  = 120    // после аварийного света убийцу ловят за 6 секунд
	nmVoiceRange      = 2400.0
)

const (
	nightmareSurvivorsSide = 0
	nightmareKillerSide    = 1
)

type nmPlayer struct {
	role        int
	battery     int
	lightWanted bool
	lightLocked bool
	panicTicks  int
	panic       bool
	carried     int
	knifeCool   int
	windup      int
	windupAngle int
	knowsKiller bool
	inserting   int
	lastMove    int32
	prevX       float64
	prevY       float64
	// deathCause — от чего погиб (nmDeath*): жертве показывается, что случилось.
	deathCause int
}

// Причина смерти в личном блоке снимка.
const (
	nmDeathNone   = 0
	nmDeathKnife  = 1 // убийца
	nmDeathBullet = 2 // пуля детектива
	nmDeathShock  = 3 // детектив застрелил невиновного и упал сам
)

type nmPanel struct {
	x, y float64
	gate bool
	area *Area
}

type nightmareState struct {
	players       map[int]*nmPlayer
	killer        int
	detective     int // -1 — револьвер лежит на земле
	vents         [][2]float64
	ventAreas     []*Area
	panels        []*nmPanel
	fuses         []*Area
	box           *Area
	boxX, boxY    float64
	gateTiles     []int
	revolver      *Area
	hasBullet     bool
	gunReload     int
	bullet        *Projectile
	bulletHit     bool
	ventCool      int
	sabotageCool  int
	blackoutUntil int32
	gatesUntil    int32
	delivered     int
	lightsOnTick  int32
	fuseSpots     [][2]int
	fusesAt       int32
	killerKills   int
	// Убийства в Кошмаре не идут в ленту (выдали бы убийцу); в счёт игрока
	// они попадают в конце боя.
	hiddenKills map[int]int
}

func (w *World) nm() *nightmareState { return w.modes.nightmare }

func (s *nightmareState) of(p *Player) *nmPlayer {
	if p == nil {
		return nil
	}
	return s.players[p.Index]
}

// explorerFor — в Кошмаре каждый выходит Исследователем без гаджетов и
// прокачки, что бы ни выбрал в меню (BuildWorld).
func explorerFor(cat *content.CatalogData, bp *BuildPlayer) *content.Character {
	ch := cat.CharByName[nmExplorerName]
	if ch == nil {
		return nil
	}
	bp.DashID, bp.UltUpgradeID, bp.StarPowerID = -1, -1, -1
	bp.AugmentIDs = nil
	bp.TestBalance = content.TestBalance{}
	bp.HPLevel, bp.DamageLevel, bp.SpeedLevel = 1, 1, 1
	return ch
}

func setupNightmare(w *World, gm *content.GameMap) {
	s := &nightmareState{players: map[int]*nmPlayer{}, killer: -1, detective: -1, hasBullet: true}
	w.modes.nightmare = s
	rng := w.modes.rng

	// Все стартуют вместе в зале со щитком: убийца идёт рядом, как все.
	spawns := mapMarkers(gm, markSpawn)
	rng.Shuffle(len(spawns), func(i, j int) { spawns[i], spawns[j] = spawns[j], spawns[i] })
	for i, p := range w.Players {
		if len(spawns) > 0 {
			x, y := tileCentre(spawns[i%len(spawns)])
			x, y = w.Tiles.FindOpen(x, y, p.CollisionRadius)
			p.X, p.Y, p.ExactX, p.ExactY = x, y, x, y
			p.SpawnX, p.SpawnY = x, y
		}
		s.players[p.Index] = &nmPlayer{battery: nmBatteryMs, prevX: p.X, prevY: p.Y}
		p.SpawnShield = false
		p.SpawnShieldTicks = 0
	}

	// Роли: один убийца, один детектив, остальные — обычные выжившие.
	order := rng.Perm(len(w.Players))
	if len(order) > 0 {
		s.killer = w.Players[order[0]].Index
		s.players[s.killer].role = nmKiller
	}
	if len(order) > 1 {
		s.detective = w.Players[order[1]].Index
		s.players[s.detective].role = nmDetective
	}

	for _, t := range mapMarkers(gm, markVent) {
		x, y := tileCentre(t)
		s.vents = append(s.vents, [2]float64{x, y})
		s.ventAreas = append(s.ventAreas, spawnProp(w, "nightmare_vent", x, y))
	}
	for _, t := range mapMarkers(gm, markPanelLight) {
		x, y := tileCentre(t)
		s.panels = append(s.panels, &nmPanel{x: x, y: y, area: spawnProp(w, "nightmare_panel_light", x, y)})
	}
	for _, t := range mapMarkers(gm, markPanelGate) {
		x, y := tileCentre(t)
		s.panels = append(s.panels, &nmPanel{x: x, y: y, gate: true, area: spawnProp(w, "nightmare_panel_gate", x, y)})
	}
	// Мест под предохранители восемь, выпадают четыре — каждый бой в других
	// и только через минуту: первую минуту убийца охотится без помех.
	s.fuseSpots = mapMarkers(gm, markFuse)
	rng.Shuffle(len(s.fuseSpots), func(i, j int) { s.fuseSpots[i], s.fuseSpots[j] = s.fuseSpots[j], s.fuseSpots[i] })
	s.fusesAt = int32(w.IntroTicks + nmFusesAfter)
	if boxes := mapMarkers(gm, markFuseBox); len(boxes) > 0 {
		s.boxX, s.boxY = tileCentre(boxes[0])
		s.box = spawnProp(w, "nightmare_fuse_box", s.boxX, s.boxY)
	} else {
		s.boxX, s.boxY = w.Tiles.PixW/2, w.Tiles.PixH/2
	}
	for _, t := range mapMarkers(gm, markGate) {
		s.gateTiles = append(s.gateTiles, t[1]*w.Tiles.W+t[0])
	}
}

func (s *nightmareState) lightsOn() bool { return s.lightsOnTick > 0 }

func (s *nightmareState) killerPlayer(w *World) *Player {
	for _, p := range w.Players {
		if p.Index == s.killer {
			return p
		}
	}
	return nil
}

func tickNightmare(w *World) {
	s := w.nm()
	if w.Finished || int(w.TickNum) < w.IntroTicks {
		return
	}
	if s.fusesAt > 0 && w.TickNum >= s.fusesAt {
		s.fusesAt = 0
		for i, t := range s.fuseSpots {
			if i >= nmFusesNeeded {
				break
			}
			x, y := tileCentre(t)
			s.fuses = append(s.fuses, spawnProp(w, "nightmare_fuse", x, y))
		}
	}
	if s.lightsOn() && w.TickNum-s.lightsOnTick >= nmLightsWinTicks {
		nightmareFinish(w, nightmareSurvivorsSide, "lights")
		return
	}
	for _, a := range s.ventAreas {
		keepProp(a)
	}
	for _, panel := range s.panels {
		keepProp(panel.area)
	}
	keepProp(s.box)
	keepProp(s.revolver)
	kept := s.fuses[:0]
	for _, f := range s.fuses {
		if f != nil && f.TimeMsLeft > 0 {
			keepProp(f)
			kept = append(kept, f)
		}
	}
	s.fuses = kept

	if s.ventCool > 0 {
		s.ventCool--
	}
	if s.sabotageCool > 0 {
		s.sabotageCool--
	}
	if s.gunReload > 0 {
		s.gunReload--
		if s.gunReload == 0 {
			s.hasBullet = true
		}
	}
	if s.bullet != nil && !s.bullet.Alive {
		if !s.bulletHit && !s.hasBullet {
			s.gunReload = nmGunReload
		}
		s.bullet = nil
		s.bulletHit = false
	}
	if s.gatesUntil > 0 && w.TickNum >= s.gatesUntil {
		s.gatesUntil = 0
		setGates(w, false)
	}

	blackout := w.TickNum < s.blackoutUntil
	for _, p := range w.Players {
		st := s.of(p)
		if st == nil {
			continue
		}
		if !p.Alive || p.Abandoned {
			p.UltActive = false
			continue
		}
		if Dist(p.X, p.Y, st.prevX, st.prevY) > 4 {
			st.lastMove = w.TickNum
		}
		st.prevX, st.prevY = p.X, p.Y

		// Фонарь: садится за 15 секунд, заряжается выключенным.
		on := st.lightWanted && !st.lightLocked && !blackout
		if on {
			st.battery -= TickMs
			if st.battery <= 0 {
				st.battery = 0
				st.lightLocked = true
				st.lightWanted = false
				on = false
			}
		} else {
			st.battery = min(nmBatteryMs, st.battery+nmBatteryRecharge)
			if st.lightLocked && st.battery >= nmBatteryUnlock {
				st.lightLocked = false
			}
		}
		p.UltActive = on
		p.UltCharge = 0

		if st.knifeCool > 0 {
			st.knifeCool--
		}
		if st.windup > 0 {
			st.windup--
			p.State = StateAttack
			if st.windup == 0 {
				nightmareStrike(w, p, st)
			}
		}

		if st.role != nmKiller {
			// Предохранитель несут по одному.
			for i, f := range s.fuses {
				if st.carried == 0 && f != nil && f.TimeMsLeft > 0 && Dist(p.X, p.Y, f.X, f.Y) <= nmPickupReach {
					st.carried++
					removeProp(f)
					s.fuses[i] = nil
				}
			}
			// Вставить — постоять у щитка две секунды: в это время ты открыт.
			if st.carried > 0 && Dist(p.X, p.Y, s.boxX, s.boxY) <= nmBoxReach {
				st.inserting++
				if st.inserting >= nmInsertTicks {
					st.inserting = 0
					s.delivered += st.carried
					st.carried = 0
					if s.delivered >= nmFusesNeeded && !s.lightsOn() {
						s.lightsOnTick = w.TickNum
						w.addHighlight(HighlightLightsOn, p, s.delivered, 20)
					}
				}
			} else {
				st.inserting = 0
			}
			if s.revolver != nil && s.revolver.TimeMsLeft > 0 && s.detective < 0 && Dist(p.X, p.Y, s.revolver.X, s.revolver.Y) <= nmPickupReach {
				removeProp(s.revolver)
				s.revolver = nil
				s.detective = p.Index
				st.role = nmDetective
				s.hasBullet = s.gunReload == 0
			}
		}
	}

	// Паника: трое и больше вплотную дольше 5 секунд — помехи на фонарях.
	for _, p := range w.Players {
		st := s.of(p)
		if st == nil || !p.Alive || p.Abandoned {
			continue
		}
		near := 0
		for _, q := range w.Players {
			if q.Alive && !q.Abandoned && Dist(p.X, p.Y, q.X, q.Y) <= nmPanicRange {
				near++
			}
		}
		if near >= 3 {
			st.panicTicks++
		} else {
			st.panicTicks = 0
		}
		st.panic = st.panicTicks >= nmPanicTicks
	}

	nightmareCheckWin(w)
}

func nightmareCheckWin(w *World) {
	if w.Finished {
		return
	}
	s := w.nm()
	killer := s.killerPlayer(w)
	if killer == nil || !killer.Alive || killer.Abandoned {
		nightmareFinish(w, nightmareSurvivorsSide, "killer_down")
		return
	}
	for _, p := range w.Players {
		if p.Index != s.killer && p.Alive && !p.Abandoned {
			return
		}
	}
	nightmareFinish(w, nightmareKillerSide, "all_dead")
}

func nightmareFinish(w *World, side int, reason string) {
	if w.Finished {
		return
	}
	s := w.nm()
	w.Finished = true
	w.WinReason = reason
	killer := s.killerPlayer(w)
	if side == nightmareKillerSide && killer != nil {
		w.WinnerTeam = killer.Team
	} else {
		w.WinnerTeam = -1
	}
	for index, n := range s.hiddenKills {
		for _, p := range w.Players {
			if p.Index == index {
				p.Kills += n
			}
		}
	}
	s.hiddenKills = nil
	for _, p := range w.Players {
		isKiller := p.Index == s.killer
		switch st := s.of(p); {
		case isKiller:
			w.setRole(p, RoleKiller)
		case st != nil && st.role == nmDetective:
			w.setRole(p, RoleDetective)
		default:
			w.setRole(p, RoleInnocent)
		}
		if isKiller == (side == nightmareKillerSide) {
			w.setRank(p, 1)
		} else {
			w.setRank(p, 2)
		}
	}
}

// nightmareModifiers: в аварийном свете убийца слепнет — вдвое медленнее.
func nightmareModifiers(w *World) {
	s := w.nm()
	if !s.lightsOn() {
		return
	}
	if k := s.killerPlayer(w); k != nil && k.Alive {
		k.EffectsSpeedAdd = max(-99, k.EffectsSpeedAdd-50)
		k.Slowdown = true
	}
}

func nightmareFlashlight(w *World, p *Player, on bool) {
	if st := w.nm().of(p); st != nil && p.Alive {
		st.lightWanted = on && !st.lightLocked
	}
}

func aimAngle(p *Player, a protocol.PlayerAction) int {
	if a.X == 0 && a.Y == 0 {
		return p.AttackAngle
	}
	return angleTo(float64(a.X), float64(a.Y))
}

func angleGap(a, b int) int {
	return abs((a-b+540)%360 - 180)
}

// nightmareCast — обе кнопки Исследователя. Кнопка атаки: нож убийцы или
// выстрел детектива. Кнопка ульты: люк или щиток рядом (только убийца).
func nightmareCast(w *World, p *Player, a protocol.PlayerAction, ultimate bool) bool {
	s := w.nm()
	st := s.of(p)
	if st == nil || !CanAct(w, p) || w.Finished || int(w.TickNum) < w.IntroTicks {
		return false
	}
	if ultimate {
		if st.role != nmKiller || s.lightsOn() {
			return false
		}
		return nightmareVent(w, p, a) || nightmareSabotage(w, p)
	}
	switch st.role {
	case nmKiller:
		if s.lightsOn() || st.knifeCool > 0 || st.windup > 0 {
			return false
		}
		st.windup = nmKnifeWindup
		st.windupAngle = nightmareKnifeAim(w, p, a)
		p.AttackAngle = st.windupAngle
		p.State = StateAttack
		SpawnArea(w, p, "nightmare_knife_draw", p.X, p.Y, 0, 0, true, false)
		return true
	case nmDetective:
		if !s.hasBullet || s.detective != p.Index {
			return false
		}
		angle := aimAngle(p, a)
		rad := float64(angle) * math.Pi / 180
		tx, ty := p.X+math.Cos(rad)*nmBulletRange, p.Y+math.Sin(rad)*nmBulletRange
		var d content.Spec
		if w.Catalog != nil {
			d = w.Catalog.Spec("projectiles", "nightmare_revolver_bullet")
		}
		pr := spawnProjectile(w, p, d, angle, 30000, nmBulletRange, tx, ty, false, 0)
		if pr == nil {
			return false
		}
		s.hasBullet = false
		s.bullet = pr
		s.bulletHit = false
		p.AttackAngle = angle
		p.State = StateAttack
		p.LastCombatTick = w.TickNum
		return true
	}
	return false
}

// nightmareKnifeAim: без прицела нож смотрит на ближайшего в досягаемости.
func nightmareKnifeAim(w *World, p *Player, a protocol.PlayerAction) int {
	if a.X != 0 || a.Y != 0 {
		return angleTo(float64(a.X), float64(a.Y))
	}
	best, bestD := -1, math.MaxFloat64
	for _, v := range w.Players {
		if v == p || !v.Alive || v.Abandoned {
			continue
		}
		if d := Dist(p.X, p.Y, v.X, v.Y); d < bestD && d <= nmKnifeReach+float64(p.CollisionRadius+v.CollisionRadius)*1.5 {
			best, bestD = angleTo(v.X-p.X, v.Y-p.Y), d
		}
	}
	if best >= 0 {
		return best
	}
	return p.AttackAngle
}

func nightmareStrike(w *World, p *Player, st *nmPlayer) {
	s := w.nm()
	if !p.Alive || s.lightsOn() {
		return
	}
	st.knifeCool = nmKnifeCooldown
	SpawnArea(w, p, "nightmare_knife_slash", p.X, p.Y, 0, 0, false, false)
	var victim *Player
	bestD := math.MaxFloat64
	for _, v := range w.Players {
		if v == p || !v.Alive || v.Abandoned {
			continue
		}
		d := Dist(p.X, p.Y, v.X, v.Y)
		if d > nmKnifeReach+float64(p.CollisionRadius+v.CollisionRadius) || d >= bestD {
			continue
		}
		if d > float64(p.CollisionRadius+v.CollisionRadius) && angleGap(angleTo(v.X-p.X, v.Y-p.Y), st.windupAngle) > 70 {
			continue
		}
		if !shotClear(w, p.X, p.Y, v.X, v.Y, &Skill{}) {
			continue
		}
		victim, bestD = v, d
	}
	// Свидетели — все, кто видел замах своими глазами.
	for _, o := range w.Players {
		if o != p && o.Alive && !o.Abandoned && o != victim && w.nightmareVisibility(o, p) == 15 {
			if ost := s.of(o); ost != nil {
				ost.knowsKiller = true
			}
		}
	}
	if victim != nil {
		p.LastCombatTick = w.TickNum
		Kill(w, p, victim)
	}
}

func nightmareVent(w *World, p *Player, a protocol.PlayerAction) bool {
	s := w.nm()
	if s.ventCool > 0 || len(s.vents) < 2 {
		return false
	}
	from := -1
	for i, v := range s.vents {
		if Dist(p.X, p.Y, v[0], v[1]) <= nmVentReach {
			from = i
			break
		}
	}
	if from < 0 {
		return false
	}
	to := (from + 1) % len(s.vents)
	if a.X != 0 || a.Y != 0 {
		aim := math.Atan2(float64(a.Y), float64(a.X))
		best := -2.0
		for i, v := range s.vents {
			if i == from {
				continue
			}
			dir := math.Atan2(v[1]-s.vents[from][1], v[0]-s.vents[from][0])
			if c := math.Cos(dir - aim); c > best {
				best, to = c, i
			}
		}
	}
	SpawnArea(w, p, "nightmare_vent_puff", s.vents[from][0], s.vents[from][1], 0, 0, false, false)
	teleportPlayer(w, p, s.vents[to][0], s.vents[to][1])
	SpawnArea(w, p, "nightmare_vent_puff", p.X, p.Y, 0, 0, false, false)
	s.ventCool = nmVentCooldown
	return true
}

func nightmareSabotage(w *World, p *Player) bool {
	s := w.nm()
	if s.sabotageCool > 0 {
		return false
	}
	for _, panel := range s.panels {
		if Dist(p.X, p.Y, panel.x, panel.y) > nmPanelReach {
			continue
		}
		if panel.gate {
			if len(s.gateTiles) == 0 {
				return false
			}
			s.gatesUntil = w.TickNum + nmGateTicks
			setGates(w, true)
		} else {
			s.blackoutUntil = w.TickNum + nmBlackoutTicks
			for _, q := range w.Players {
				q.UltActive = false
			}
		}
		s.sabotageCool = nmSabotageCool
		SpawnArea(w, p, "nightmare_sabotage_sparks", panel.x, panel.y, 0, 0, false, false)
		return true
	}
	return false
}

// setGates опускает или поднимает решётки. Кто стоял в проёме — выталкивается.
func setGates(w *World, closed bool) {
	s := w.nm()
	for _, i := range s.gateTiles {
		if i < 0 || i >= len(w.Tiles.Blocked) {
			continue
		}
		w.Tiles.Blocked[i] = closed
	}
	if !closed {
		return
	}
	for _, p := range w.Players {
		if p.Alive && !w.Tiles.CanOccupy(p.X, p.Y, p.CollisionRadius) {
			x, y := w.Tiles.FindOpen(p.X, p.Y, p.CollisionRadius)
			p.X, p.Y, p.ExactX, p.ExactY = x, y, x, y
			p.HasMoveTarget = false
		}
	}
}

// NightmareGates — закрытые решётки для снимка (динамические плитки).
func (w *World) activeGateTiles() []int {
	s := w.modes.nightmare
	if s == nil || s.gatesUntil == 0 {
		return nil
	}
	return s.gateTiles
}

func nightmareOnKill(w *World, killer, victim *Player) {
	s := w.nm()
	st := s.of(victim)
	if st == nil {
		return
	}
	victim.UltActive = false
	SpawnArea(w, victim, "nightmare_body", victim.X, victim.Y, 0, 0, false, false)
	for i := 0; i < st.carried; i++ {
		a := float64(i) * 2.1
		x, y := w.Tiles.FindOpen(victim.X+math.Cos(a)*140, victim.Y+math.Sin(a)*140, 60)
		s.fuses = append(s.fuses, spawnProp(w, "nightmare_fuse", x, y))
	}
	st.carried = 0
	killerSt := s.of(killer)
	if killer != nil && killer != victim {
		if s.hiddenKills == nil {
			s.hiddenKills = map[int]int{}
		}
		s.hiddenKills[killer.Index]++
	}
	switch {
	case killerSt != nil && killerSt.role == nmKiller && killer != victim:
		st.deathCause = nmDeathKnife
		// Жертва видела удар: ей (и только мёртвым) убийца раскрыт.
		st.knowsKiller = true
		s.killerKills++
		w.addHighlight(HighlightNightmareKill, killer, s.killerKills, 6+2*s.killerKills)
	case killer != nil && killer.Index == s.detective && killer != victim:
		st.deathCause = nmDeathBullet
	case killer == nil && victim.Index == s.detective && s.bulletHit:
		st.deathCause = nmDeathShock
	}
	if st.role == nmKiller && killer != nil && killer != victim {
		w.addHighlight(HighlightKillerDown, killer, 1, 30)
	}
	if victim.Index == s.detective {
		s.detective = -1
		s.revolver = spawnProp(w, "nightmare_revolver", victim.X, victim.Y)
	}
	if killer != nil && killer.Index == s.detective && killer != victim {
		s.bulletHit = true
		if st.role != nmKiller {
			// Детектив застрелил невиновного — и падает сам, от шока.
			Kill(w, nil, killer)
		}
	}
	if st.role == nmKiller {
		nightmareFinish(w, nightmareSurvivorsSide, "killer_down")
	}
}

// nightmareVisibility — видит ли observer цель: 15 — целиком, 1 — только луч
// её фонаря, 2 — только «пульс» бегущего (для убийцы), 0 — ничего.
func (w *World) nightmareVisibility(o, t *Player) int {
	s := w.nm()
	if o == nil || o == t || !o.Alive || s.lightsOn() || w.Finished {
		return 15
	}
	d := Dist(o.X, o.Y, t.X, t.Y)
	los := func() bool { return shotClear(w, o.X, o.Y, t.X, t.Y, &Skill{}) }
	ost := s.of(o)
	if d <= nmTouchRange && los() {
		return 15
	}
	if ost != nil && ost.role == nmKiller && d <= nmNightVision && los() {
		return 15
	}
	if o.UltActive && d <= nightmareBeamRange(ost) && angleGap(angleTo(t.X-o.X, t.Y-o.Y), o.AttackAngle) <= nmBeamHalfAngle && los() {
		return 15
	}
	if t.UltActive && d <= nmGlowRange && los() {
		return 15
	}
	if t.UltActive && d <= nmBeamSeenFrom {
		return 1
	}
	if ost != nil && ost.role == nmKiller {
		tst := s.of(t)
		if tst != nil && d <= nmPulseRange && w.TickNum-tst.lastMove <= 2 {
			return 2
		}
		// Предохранитель в руках искрит — убийца чует носильщика издалека.
		if tst != nil && tst.carried > 0 && d <= nmFuseSparkRange {
			return 2
		}
	}
	return 0
}

func nightmareBeamRange(st *nmPlayer) float64 {
	if st != nil && st.panic {
		return nmBeamRange / 2
	}
	return nmBeamRange
}

// nightmarePointVisible — видна ли точка (для предметов и вспышек).
func (w *World) nightmarePointVisible(o *Player, x, y float64) bool {
	s := w.nm()
	if o == nil || !o.Alive || s.lightsOn() || w.Finished {
		return true
	}
	d := Dist(o.X, o.Y, x, y)
	los := func() bool { return shotClear(w, o.X, o.Y, x, y, &Skill{}) }
	ost := s.of(o)
	if d <= nmTouchRange*1.5 && los() {
		return true
	}
	if ost != nil && ost.role == nmKiller && d <= nmNightVision && los() {
		return true
	}
	if o.UltActive && d <= nightmareBeamRange(ost) && angleGap(angleTo(x-o.X, y-o.Y), o.AttackAngle) <= nmBeamHalfAngle+10 && los() {
		return true
	}
	return false
}

// nightmareAreaVisible: мебель карты видна всем (это часть уровня), а следы
// событий — нож, дым из люка, тела, предохранители, револьвер — только тем,
// кто их сейчас видит. Люки и щитки убийца видит всегда.
func (w *World) nightmareAreaVisible(o *Player, a *Area) bool {
	switch a.Definition.String("name") {
	case "nightmare_vent", "nightmare_panel_light", "nightmare_panel_gate", "nightmare_fuse_box":
		return true
	}
	if a.Following && o != nil && a.OwnerIndex == o.ObjectIndex {
		return true
	}
	return w.nightmarePointVisible(o, a.X, a.Y)
}

// encodeNightmareBlock — личная часть снимка: своя роль и свои запасы.
func encodeNightmareBlock(bw *protocol.BitWriter, w *World, o *Player) {
	s := w.nm()
	st := s.of(o)
	role, battery, carried, knife := 0, nmBatteryMs, 0, 0
	locked, panic, knows := false, false, false
	if st != nil {
		role, battery, carried, knife = st.role, st.battery, st.carried, st.knifeCool
		locked, panic, knows = st.lightLocked, st.panic, st.knowsKiller
	}
	alive := 0
	for _, p := range w.Players {
		if p.Alive && !p.Abandoned {
			alive++
		}
	}
	bw.WritePositiveIntMax3(role)
	bw.WritePositiveIntMax127(min(100, battery*100/nmBatteryMs))
	bw.WriteBool(locked)
	bw.WriteBool(panic)
	bw.WritePositiveIntMax7(min(7, carried))
	bw.WritePositiveIntMax7(min(7, s.delivered))
	bw.WritePositiveIntMax15(min(15, alive))
	bw.WriteBool(s.lightsOn())
	bw.WritePositiveIntMax255(clamp255(int(s.blackoutUntil - w.TickNum)))
	bw.WritePositiveIntMax255(clamp255(int(s.gatesUntil - w.TickNum)))
	// Детективу — патрон и перезарядка; убийце — нож, люк, саботаж и что рядом.
	isDetective := st != nil && role == nmDetective && s.detective == o.Index
	bw.WriteBool(isDetective && s.hasBullet)
	bw.WritePositiveIntMax255(clamp255(boolInt(isDetective) * s.gunReload))
	isKiller := role == nmKiller
	bw.WritePositiveIntMax127(min(127, boolInt(isKiller)*knife))
	bw.WritePositiveIntMax255(clamp255(boolInt(isKiller) * s.ventCool))
	bw.WritePositiveIntMax1023(min(1023, boolInt(isKiller)*s.sabotageCool))
	context := 0
	if isKiller && o != nil && o.Alive {
		for _, v := range s.vents {
			if Dist(o.X, o.Y, v[0], v[1]) <= nmVentReach {
				context = 1
			}
		}
		for _, panel := range s.panels {
			if Dist(o.X, o.Y, panel.x, panel.y) <= nmPanelReach {
				context = 2 + boolInt(panel.gate)
			}
		}
	}
	bw.WritePositiveIntMax3(context)
	// Убийца раскрыт: в аварийном свете, после конца боя — и свидетелю, который видел удар.
	killerShown := 15
	// Без наблюдателя — это запись реплея: зрителю повтора убийца виден сразу.
	if s.lightsOn() || w.Finished || knows || o == nil {
		killerShown = min(15, max(0, s.killer))
	}
	bw.WritePositiveIntMax15(killerShown)
	inserting := 0
	if st != nil {
		inserting = st.inserting
	}
	bw.WritePositiveIntMax63(min(63, inserting))
	fusesIn := 0
	if s.fusesAt > 0 {
		fusesIn = int(s.fusesAt - w.TickNum)
	}
	bw.WritePositiveVIntMax65535(min(65535, max(0, fusesIn)))
	cause := 0
	if st != nil && o != nil && !o.Alive {
		cause = st.deathCause
	}
	bw.WritePositiveIntMax3(cause)
	// Убийца в зоне его ночного зрения — выживший слышит сердцебиение
	// (направления и имени это не выдаёт).
	near := false
	if k := s.killerPlayer(w); st != nil && st.role != nmKiller && o != nil && o.Alive && k != nil && k.Alive && !s.lightsOn() {
		near = Dist(o.X, o.Y, k.X, k.Y) <= nmNightVision
	}
	bw.WriteBool(near)
	// Цель выжившего: клетка ближайшего лежащего предохранителя. Тестеры не
	// понимали, что делать; стрелка у ног ведёт к нему, а убийца по-прежнему
	// прячется в темноте. Несущему — ничего: щиток клиент видит сам.
	fx, fy, has := 0, 0, false
	if st != nil && st.role != nmKiller && o != nil && o.Alive && st.carried == 0 && !s.lightsOn() {
		best := math.MaxFloat64
		for _, f := range s.fuses {
			if f == nil || f.TimeMsLeft <= 0 {
				continue
			}
			if d := Dist(o.X, o.Y, f.X, f.Y); d < best {
				best, has = d, true
				fx, fy = int(f.X/PixelsPerTile), int(f.Y/PixelsPerTile)
			}
		}
	}
	bw.WriteBool(has)
	if has {
		bw.WritePositiveIntMax63(min(63, max(0, fx)))
		bw.WritePositiveIntMax63(min(63, max(0, fy)))
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// NightmareHears — голос по близости: живые слышат живых в 8 клетках,
// мёртвые — только мёртвых.
func (w *World) NightmareHears(listener, speaker *Player) bool {
	if listener == nil || speaker == nil {
		return false
	}
	if !listener.Alive && !speaker.Alive {
		return true
	}
	if listener.Alive != speaker.Alive {
		return false
	}
	return Dist(listener.X, listener.Y, speaker.X, speaker.Y) <= nmVoiceRange
}
