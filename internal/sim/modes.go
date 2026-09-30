package sim

import (
	"crypto/rand"
	"encoding/binary"
	"hash/fnv"
	mrand "math/rand"

	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
)

// Режимы 26.09: Кошмар в катакомбах (nightmare.go), Заражение (infection.go)
// и Нокаут (knockout.go). Здесь — общий слой: какие точки движка они
// перехватывают и что уходит в итоги боя.
const (
	ModeNightmare = "nightmare"
	ModeInfection = "infection"
	ModeKnockout  = "knockout"
	// Захват карита (karitgrab.go) — пришёл на место Кошмара 28.09.
	ModeKaritGrab = "karit_grab"
)

// Маркеры карт режимов — строчные буквы, которых нет в map_tiles.yaml: для
// движения это пол, а режим раскладывает по ним предметы.
const (
	markVent       = 'v'
	markPanelLight = 'q'
	markPanelGate  = 'r'
	markFuse       = 'f'
	markFuseBox    = 'b'
	markGate       = 'g'
	markSpawn      = 's'
)

// Динамическая плитка решётки: fast_platform_fence (dynamic_code 2) — клиент
// сам умеет подменять плитку по кадру и перестаёт в неё ходить.
const gateDynamicCode = 2

// modeState — то, что режим держит в мире. Ровно один из указателей не nil.
type modeState struct {
	nightmare *nightmareState
	infection *infectionState
	knockout  *knockoutState
	karitGrab *karitGrabState
	rng       *mrand.Rand
}

func (w *World) IsNightmare() bool { return w.modes.nightmare != nil }
func (w *World) IsInfection() bool { return w.modes.infection != nil }
func (w *World) IsKnockout() bool  { return w.modes.knockout != nil }

// modeHero — персонаж режима: для клиента он миньон (не выбирается, бандл
// не обязателен), а в бою ведётся и пишется в снимок как герой.
func modeHero(ch *content.Character, spec content.Spec) bool {
	return ch != nil && spec.Bool("mode_hero")
}

// setupModes запускается в конце BuildWorld, когда игроки уже стоят.
func setupModes(w *World, p BuildParams, gm *content.GameMap) {
	seed := p.Seed
	if seed == 0 {
		// Роли в Кошмаре не должны угадываться: сид — из криптослучайности.
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err == nil {
			seed = int64(binary.LittleEndian.Uint64(buf[:]))
		} else {
			h := fnv.New64a()
			h.Write([]byte(p.BattleID))
			seed = int64(h.Sum64())
		}
	}
	w.modes.rng = mrand.New(mrand.NewSource(seed))
	switch w.ModeName {
	case ModeNightmare:
		setupNightmare(w, gm)
	case ModeInfection:
		setupInfection(w)
	case ModeKnockout:
		setupKnockout(w)
	case ModeKaritGrab:
		setupKaritGrab(w)
	}
}

// mapMarkers — клетки карты с маркером (в осях сервера: y снизу вверх).
func mapMarkers(gm *content.GameMap, mark byte) [][2]int {
	var out [][2]int
	for y, row := range gm.Rows {
		for x := 0; x < gm.W && x < len(row); x++ {
			if row[x] == mark {
				out = append(out, [2]int{x, gm.H - 1 - y})
			}
		}
	}
	return out
}

func tileCentre(t [2]int) (float64, float64) {
	return float64(t[0])*PixelsPerTile + PixelsPerTile/2, float64(t[1])*PixelsPerTile + PixelsPerTile/2
}

// spawnProp — предмет карты без хозяина: area-эффект, который режим держит
// живым сам (время жизни подкручивается каждый тик).
func spawnProp(w *World, name string, x, y float64) *Area {
	if w.Catalog == nil || len(w.Areas) >= 256 {
		return nil
	}
	d := w.Catalog.Spec("area_effects", name)
	if !d.Exists() {
		return nil
	}
	index := w.AllocEffectIndex()
	if index == 0 {
		return nil
	}
	a := &Area{Definition: d, LocalIndex: index, OwnerIndex: -1, RosterIndex: -1, Team: -1, X: x, Y: y,
		TimeMsLeft: max(50, d.Int("time_ms", 50)), NextPulseTick: 1 << 30}
	w.Areas = append(w.Areas, a)
	return a
}

// keepProp держит предмет на карте, пока режим его не уберёт.
func keepProp(a *Area) {
	if a != nil && a.TimeMsLeft > 0 {
		a.TimeMsLeft = max(a.TimeMsLeft, 2000)
	}
}

func removeProp(a *Area) {
	if a != nil {
		a.TimeMsLeft = 0
	}
}

// tickModes — ход режима в начале тика, до ботов и движения.
func tickModes(w *World) {
	switch {
	case w.modes.nightmare != nil:
		tickNightmare(w)
	case w.modes.infection != nil:
		tickInfection(w)
	case w.modes.knockout != nil:
		tickKnockout(w)
	case w.modes.karitGrab != nil:
		tickKaritGrab(w)
	}
}

// applyModeModifiers — постоянные поправки режима. TickEffects каждый тик
// обнуляет скорость и перезарядку от эффектов, поэтому поправки идут после него.
func applyModeModifiers(w *World) {
	switch {
	case w.modes.nightmare != nil:
		nightmareModifiers(w)
	case w.modes.infection != nil:
		infectionModifiers(w)
	}
}

// modeCast перехватывает навык. handled=true — режим решил сам, ok — итог.
func modeCast(w *World, p *Player, s *Skill, a protocol.PlayerAction, ultimate bool) (handled, ok bool) {
	switch {
	case w.modes.nightmare != nil && p.Owner == nil && p.HeroType == "hero":
		return true, nightmareCast(w, p, a, ultimate)
	case w.modes.infection != nil:
		if !infectionCanCast(w, p) {
			return true, false
		}
	case w.modes.knockout != nil:
		if !knockoutFighting(w) {
			return true, false
		}
	}
	return false, false
}

// modeDash: можно ли рывок (гаджет) и учёт использований.
func modeDashAllowed(w *World, p *Player) bool {
	switch {
	case w.modes.nightmare != nil:
		return false
	case w.modes.infection != nil:
		return infectionDashAllowed(w, p)
	case w.modes.knockout != nil:
		return knockoutFighting(w) && p.DashUses < knockoutGadgets
	}
	return true
}

// modeUltToggle — кнопка «ульта вкл/выкл». В Кошмаре это фонарь.
func ModeUltToggle(w *World, p *Player, on bool) bool {
	if w.modes.nightmare != nil {
		nightmareFlashlight(w, p, on)
		return true
	}
	return false
}

// onModeKill — после смерти героя (Kill уже всё сбросил).
func onModeKill(w *World, killer, victim *Player) {
	switch {
	case w.modes.nightmare != nil:
		nightmareOnKill(w, killer, victim)
	case w.modes.infection != nil:
		infectionOnKill(w, killer, victim)
	case w.modes.knockout != nil:
		knockoutOnKill(w, killer, victim)
	case w.modes.karitGrab != nil:
		karitGrabOnKill(w, victim)
	}
}

// onModeRespawn — герой только что ожил в общем цикле возрождения.
func onModeRespawn(w *World, p *Player) {
	if w.modes.infection != nil {
		infectionOnRespawn(w, p)
	}
}

// modeHidesKills: в Кошмаре лента убийств выдала бы убийцу.
func modeHidesKills(w *World) bool { return w.modes.nightmare != nil }

// modeFinishByTime решает исход по таймеру. true — режим решил сам.
func modeFinishByTime(w *World) bool {
	switch {
	case w.modes.nightmare != nil:
		nightmareFinish(w, nightmareSurvivorsSide, "time")
		return true
	case w.modes.infection != nil:
		infectionFinishByTime(w)
		return true
	case w.modes.knockout != nil:
		knockoutFinishByTime(w)
		return true
	case w.modes.karitGrab != nil:
		karitGrabFinishByTime(w)
		return true
	}
	return false
}

// modeFrozen — режим держит паузу (нокаут между раундами): мир стоит.
func modeFrozen(w *World) bool {
	return w.modes.knockout != nil && w.modes.knockout.pause > 0
}

// SetPlayerRanks — место каждого игрока (по индексу в составе) для итогов.
// Режимы, где победа не делится по командам, заполняют его сами.
func (w *World) setRank(p *Player, rank int) {
	if w.PlayerRanks == nil {
		w.PlayerRanks = map[int]int{}
	}
	w.PlayerRanks[p.Index] = rank
}

// humans — живые игроки, не мяч и не вышедшие.
func livePlayers(w *World) []*Player {
	out := make([]*Player, 0, len(w.Players))
	for _, p := range w.Players {
		if p.Alive && !p.Abandoned && !p.IsBall() {
			out = append(out, p)
		}
	}
	return out
}

// randomOpenPoint — случайная свободная клетка карты подальше от avoid.
func randomOpenPoint(w *World, radius int, avoid []*Player, minDist float64) (float64, float64) {
	rng := w.modes.rng
	bestX, bestY, bestD := w.Tiles.PixW/2, w.Tiles.PixH/2, -1.0
	for attempt := 0; attempt < 60; attempt++ {
		tx := 1 + rng.Intn(max(1, w.Tiles.W-2))
		ty := 1 + rng.Intn(max(1, w.Tiles.H-2))
		x, y := tileCentre([2]int{tx, ty})
		if !w.Tiles.CanOccupy(x, y, radius) || w.PoisonedAt(x, y, w.TickNum+100) {
			continue
		}
		near := 1e18
		for _, o := range avoid {
			if o.Alive && !o.Abandoned {
				near = min(near, Dist(x, y, o.X, o.Y))
			}
		}
		if near >= minDist {
			return x, y
		}
		if near > bestD {
			bestX, bestY, bestD = x, y, near
		}
	}
	return w.Tiles.FindOpen(bestX, bestY, radius)
}

// swapCharacter переодевает героя в другого персонажа прямо в бою (заражение).
// Навыки, здоровье, скорость и радиус — от нового персонажа; прокачка героя
// на классы режима не действует.
func swapCharacter(w *World, p *Player, name string) bool {
	if w.Catalog == nil {
		return false
	}
	ch := w.Catalog.CharByName[name]
	if ch == nil {
		return false
	}
	CancelCast(p)
	p.ModelID = int32(ch.Id)
	p.Definition = w.Catalog.SpecByID("characters", ch.Id)
	p.Skills = buildSkills(w.Catalog, ch, BuildPlayer{DashID: -1, UltUpgradeID: -1})
	p.MaxHP = ch.HitPoints
	p.HP = p.MaxHP
	p.BaseSpeed = ch.Speed
	p.SpeedAdd = 0
	p.DamageAddPct = 0
	p.BaseDamageAddPct = 0
	p.DamageScaleBP = 0
	p.UltCharge = 0
	p.UltChargeMult = ch.UltChargeMult
	p.UltUltMult = ch.UltUltMult
	p.ChargeAuto = ch.ChargeAuto
	p.CollisionRadius = ch.CollisionRadius
	p.SpawnHeight = float64(p.Definition.Int("projectile_start_z", 450))
	p.Upgrade = content.Spec{}
	p.StarPower = content.Spec{}
	p.Augments = nil
	p.Effects = nil
	p.ConsumableShield = 0
	p.OneShotShield = false
	p.HeroType = "hero"
	p.WireHero = true
	return true
}

// resultTeam/ResultModel — команда и герой из состава матча: режимы меняют
// их в бою (заражённые уходят в команду 1), а итоги сверяются с составом.
// Результаты, записанные до этих полей, их не несут — тогда берём текущие.
func (p *Player) ResultTeam() int {
	if p.RosterModelID == 0 {
		return p.Team
	}
	return p.RosterTeam
}

func (p *Player) ResultModel() int32 {
	if p.RosterModelID == 0 {
		return p.ModelID
	}
	return p.RosterModelID
}
