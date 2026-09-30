package sim

import (
	"math"

	"github.com/freaks/openfreaks/internal/protocol"
)

// НОКАУТ 3 на 3. До двух побед в раундах (максимум три раунда). Внутри
// раунда никто не возрождается. Раунд выигрывает команда, которая выбила
// всех соперников; затянулся — по урону за раунд. С 30-й секунды раунда с
// краёв ползёт ядовитый дым. Ульта в начале раунда сгорает, гаджеты —
// три на весь матч.

const (
	knockoutRoundTicks  = 1800 // 90 секунд на раунд
	knockoutPoisonDelay = 600  // дым с 30-й секунды
	knockoutPoisonSpeed = 4    // пикселей за тик, как в клиенте (PoisonContainer)
	knockoutPoisonMin   = 450  // последний пятачок в центре
	knockoutIntroPause  = 60   // «Раунд N» — 3 секунды
	knockoutOverPause   = 80   // итог раунда — 4 секунды
	knockoutRoundsToWin = 2
	knockoutMaxRounds   = 3
	knockoutGadgets     = 3
	// Тестеры: «все друг друга очень быстро убивают». Карты стали длиннее
	// (21x45), а бой — чуть медленнее и живучее: скорость -12%, здоровье +30%.
	knockoutSpeedCut = 12
	knockoutHPBonus  = 30
)

const (
	knockoutStateIntro = 0
	knockoutStateFight = 1
	knockoutStateOver  = 2
)

type knockoutState struct {
	round       int
	wins        [2]int
	state       int
	pause       int
	roundStart  int32
	roundEnd    int32
	lastWinner  int
	damageBase  [2]int
	totalDamage [2]int
	// Для лучших моментов: убийства раунда по игрокам и последний удар.
	roundKills   map[int]int
	lastKiller   int
	lastKillTick int32
}

func (w *World) ko() *knockoutState { return w.modes.knockout }

func setupKnockout(w *World) {
	w.modes.knockout = &knockoutState{round: 1, state: knockoutStateIntro, pause: knockoutIntroPause, lastWinner: -1, roundKills: map[int]int{}, lastKiller: -1}
	for _, p := range w.Players {
		if p.IsBall() {
			continue
		}
		// SpeedAdd уходит клиенту в снимке — предсказание движения не разойдётся.
		p.SpeedAdd -= knockoutSpeedCut
		p.MaxHP += p.MaxHP * knockoutHPBonus / 100
		p.HP = p.MaxHP
	}
}

func knockoutFighting(w *World) bool {
	s := w.ko()
	return s != nil && s.state == knockoutStateFight && s.pause == 0
}

// knockoutPauseTick — мир стоит (между раундами); идёт только отсчёт.
func knockoutPauseTick(w *World) {
	s := w.ko()
	if int(w.TickNum) < w.IntroTicks {
		return
	}
	s.pause--
	if s.pause > 0 {
		return
	}
	switch s.state {
	case knockoutStateOver:
		s.round++
		resetKnockoutRound(w)
		s.state = knockoutStateIntro
		s.pause = knockoutIntroPause
	case knockoutStateIntro:
		s.state = knockoutStateFight
		s.roundStart = w.TickNum
		s.roundEnd = w.TickNum + knockoutRoundTicks
		s.damageBase = teamDamage(w)
		s.roundKills = map[int]int{}
		s.lastKiller = -1
		for _, p := range w.Players {
			p.HasMoveTarget = false
		}
	}
}

func teamDamage(w *World) [2]int {
	var out [2]int
	for _, p := range w.Players {
		if p.Team >= 0 && p.Team < 2 {
			out[p.Team] += p.DamageDealt
		}
	}
	return out
}

func tickKnockout(w *World) {
	s := w.ko()
	if w.Finished || s.state != knockoutStateFight {
		return
	}
	var alive [2]int
	for _, p := range w.Players {
		if p.Alive && !p.Abandoned && p.Team >= 0 && p.Team < 2 {
			alive[p.Team]++
		}
	}
	winner := -2
	switch {
	case alive[0] == 0 && alive[1] == 0:
		winner = knockoutByDamage(w)
	case alive[0] == 0:
		winner = 1
	case alive[1] == 0:
		winner = 0
	case w.TickNum >= s.roundEnd:
		winner = knockoutByDamage(w)
	}
	if winner == -2 {
		return
	}
	now := teamDamage(w)
	for t := 0; t < 2; t++ {
		s.totalDamage[t] += now[t] - s.damageBase[t]
	}
	s.lastWinner = winner
	if winner >= 0 {
		s.wins[winner]++
		knockoutRoundHighlight(w, winner)
	}
	if (winner >= 0 && s.wins[winner] >= knockoutRoundsToWin) || s.round >= knockoutMaxRounds {
		knockoutFinish(w)
		return
	}
	s.state = knockoutStateOver
	s.pause = knockoutOverPause
}

// knockoutByDamage — ничья по живым решается уроном за раунд (-1 — ничья).
func knockoutByDamage(w *World) int {
	s := w.ko()
	now := teamDamage(w)
	d0, d1 := now[0]-s.damageBase[0], now[1]-s.damageBase[1]
	switch {
	case d0 > d1:
		return 0
	case d1 > d0:
		return 1
	}
	return -1
}

func knockoutFinish(w *World) {
	s := w.ko()
	if w.Finished {
		return
	}
	w.Finished = true
	w.WinReason = "knockout"
	switch {
	case s.wins[0] > s.wins[1]:
		w.WinnerTeam = 0
	case s.wins[1] > s.wins[0]:
		w.WinnerTeam = 1
	case s.totalDamage[0] > s.totalDamage[1]:
		w.WinnerTeam = 0
	case s.totalDamage[1] > s.totalDamage[0]:
		w.WinnerTeam = 1
	default:
		w.WinnerTeam = -1
	}
	w.TeamPoints[0], w.TeamPoints[1] = s.wins[0], s.wins[1]
}

func knockoutFinishByTime(w *World) {
	s := w.ko()
	if s.state == knockoutStateFight {
		now := teamDamage(w)
		for t := 0; t < 2; t++ {
			s.totalDamage[t] += now[t] - s.damageBase[t]
		}
		s.state = knockoutStateOver
	}
	knockoutFinish(w)
}

func knockoutOnKill(w *World, killer, victim *Player) {
	// Внутри раунда не возрождаются: RespawnTicks у режима -1.
}

// resetKnockoutRound — все живы на своих местах, ульта сгорает, стены целы.
func resetKnockoutRound(w *World) {
	for _, p := range w.Players {
		if p.Abandoned || p.IsBall() {
			continue
		}
		p.Alive = true
		p.HP = p.MaxHP
		p.State = StateRespawn
		p.Executions = nil
		p.Effects = nil
		p.ControlRemoved = false
		p.Charged = false
		p.Teleporting = false
		p.SpawnShield = true
		p.SpawnShieldTicks = 40
		p.UltCharge = 0
		p.UltActive = false
		p.ConsecutiveLeft = 0
		p.ConsumableShield = 0
		p.RespawnLeft = 0
		p.HasMoveTarget = false
		p.PendingKills = nil
		for _, sk := range p.Skills {
			sk.Charges = sk.MaxCharge
			sk.CoolLeft = 0
			sk.ActiveLeft = 0
			sk.CastPending = false
		}
		nx, ny := w.Tiles.FindOpen(p.SpawnX, p.SpawnY, p.CollisionRadius)
		p.X, p.Y, p.ExactX, p.ExactY = nx, ny, nx, ny
	}
	for _, m := range w.Minions {
		m.Alive = false
		m.DeathTick = w.TickNum - 10
	}
	w.Minions = w.Minions[:0]
	w.TouchMinions()
	w.Projectiles = w.Projectiles[:0]
	w.Areas = w.Areas[:0]
	for i := range w.Tiles.Destroyed {
		w.Tiles.Destroyed[i] = false
	}
}

// knockoutPoisoned — дым раунда: скруглённый квадрат, как в одиночке, но
// отсчёт от начала раунда и быстрее. Клиент рисует его по тем же числам.
func knockoutPoisoned(w *World, x, y float64, tick int32) bool {
	s := w.ko()
	if s.state != knockoutStateFight {
		return false
	}
	start := s.roundStart + knockoutPoisonDelay
	if tick <= start {
		return false
	}
	// От большей стороны: на вытянутой карте дым сначала ползёт с торцов, где
	// стоят команды, и сгоняет их к середине. Клиент (PoisonContainer) — так же.
	radius := math.Max(w.Tiles.PixW, w.Tiles.PixH)/2 - float64(tick-start)*knockoutPoisonSpeed
	radius = math.Max(radius, knockoutPoisonMin)
	qx := math.Max(0, 2*math.Abs(x-w.Tiles.PixW/2)-radius)
	qy := math.Max(0, 2*math.Abs(y-w.Tiles.PixH/2)-radius)
	return qx*qx+qy*qy > radius*radius
}

func encodeKnockoutBlock(bw *protocol.BitWriter, w *World, o *Player) {
	s := w.ko()
	bw.WritePositiveIntMax3(min(3, s.round))
	bw.WritePositiveIntMax3(min(3, s.wins[0]))
	bw.WritePositiveIntMax3(min(3, s.wins[1]))
	bw.WritePositiveIntMax3(s.state)
	bw.WritePositiveIntMax255(clamp255(s.pause))
	bw.WritePositiveVIntMax65535(min(65535, max(0, int(s.roundStart))))
	left := 0
	if s.state == knockoutStateFight {
		left = int(s.roundEnd - w.TickNum)
	}
	bw.WritePositiveVIntMax65535(min(65535, max(0, left)))
	bw.WriteIntMax1(max(-1, min(1, s.lastWinner)))
	gadgets := 0
	if o != nil {
		gadgets = max(0, knockoutGadgets-o.DashUses)
	}
	bw.WritePositiveIntMax3(min(3, gadgets))
}

// knockoutRoundHighlight — раунд решён: эйс (один выбил всю команду) или
// решающий удар раунда. Момент ставится на тик последнего убийства.
func knockoutRoundHighlight(w *World, winner int) {
	s := w.ko()
	if s.lastKiller < 0 || s.lastKiller >= len(w.Players) || w.Players[s.lastKiller].Team != winner {
		return
	}
	enemies := 0
	for _, p := range w.Players {
		if p.Team == 1-winner && !p.IsBall() {
			enemies++
		}
	}
	killer := w.Players[s.lastKiller]
	h := w.hl()
	kind, count, score := HighlightRoundWin, s.round, 14
	if s.wins[winner] >= knockoutRoundsToWin {
		score = 20
	}
	if enemies >= 2 && s.roundKills[s.lastKiller] >= enemies {
		kind, count, score = HighlightAce, enemies, 32
	}
	if ref := w.addHighlight(kind, killer, count, score); ref >= 0 {
		h.list[ref].Tick = s.lastKillTick
	}
}
