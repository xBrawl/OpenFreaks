package sim

import (
	"math"

	"github.com/freaks/openfreaks/internal/protocol"
)

// ЗАХВАТ КАРИТИ 3 на 3 — клон «Захвата кристаллов». В центре карты шахта:
// раз в 7 секунд она выбрасывает кристалл карита. Подобранное несёт герой
// (GamePoints — клиент рисует число над головой), очки команды — сумма
// у живых. У кого 10+ и больше, чем у соперника, тому идёт 15-секундный
// отсчёт; при равенстве 10+ отсчёт стоит, потеряли перевес — сброс. Погиб —
// все кристаллы разлетаются вокруг. Кончилось время — побеждает, у кого больше.

const (
	kgSpawnTicks     = 140 // кристалл из шахты раз в 7 секунд
	kgCountdownTicks = 300 // 15 секунд удержания
	kgToWin          = 10
	kgFloorCap       = 40 // больше этого на полу шахта не выбрасывает
	kgItem           = "karit_crystal"
	kgMineProp       = "karit_mine"
)

type karitGrabState struct {
	mineX, mineY  float64
	mine          *Area
	nextSpawn     int32
	countdownTeam int // -1 — отсчёта нет
	countdownLeft int
	paused        bool
	mined         int
}

func (w *World) IsKaritGrab() bool   { return w.modes.karitGrab != nil }
func (w *World) kg() *karitGrabState { return w.modes.karitGrab }

func setupKaritGrab(w *World) {
	x, y := w.Tiles.FindOpen(w.Tiles.PixW/2, w.Tiles.PixH/2, 60)
	s := &karitGrabState{mineX: x, mineY: y, countdownTeam: -1,
		nextSpawn: int32(w.IntroTicks) + kgSpawnTicks}
	w.modes.karitGrab = s
	s.mine = spawnProp(w, kgMineProp, x, y)
	for _, p := range w.Players {
		p.GamePoints = 0
	}
}

func tickKaritGrab(w *World) {
	s := w.kg()
	if w.Finished {
		return
	}
	keepProp(s.mine)
	for _, p := range w.Players {
		// Вышедший из боя роняет всё, что нёс: иначе кристаллы пропадут из игры.
		if p.Abandoned && p.GamePoints > 0 {
			kgScatter(w, p.X, p.Y, p.GamePoints)
			p.GamePoints = 0
		}
	}
	if int(w.TickNum) >= int(s.nextSpawn) {
		s.nextSpawn = w.TickNum + kgSpawnTicks
		if kgOnFloor(w) < kgFloorCap {
			a := w.modes.rng.Float64() * 2 * math.Pi
			r := 90 + w.modes.rng.Float64()*120
			kgDrop(w, s.mineX+math.Cos(a)*r, s.mineY+math.Sin(a)*r)
			s.mined++
		}
	}
	var held [2]int
	for _, p := range w.Players {
		if p.Team >= 0 && p.Team < 2 && !p.Abandoned {
			held[p.Team] += p.GamePoints
		}
	}
	w.TeamPoints[0], w.TeamPoints[1] = held[0], held[1]

	leader := -1
	switch {
	case held[0] >= kgToWin && held[0] > held[1]:
		leader = 0
	case held[1] >= kgToWin && held[1] > held[0]:
		leader = 1
	}
	s.paused = false
	switch {
	case leader >= 0 && s.countdownTeam != leader:
		s.countdownTeam = leader
		s.countdownLeft = kgCountdownTicks
	case leader >= 0:
		s.countdownLeft--
	case s.countdownTeam >= 0 && held[0] >= kgToWin && held[0] == held[1]:
		// Обе по 10+ и поровну — отсчёт замирает, пока кто-то не вырвется.
		s.paused = true
	default:
		s.countdownTeam = -1
		s.countdownLeft = 0
	}
	if s.countdownTeam >= 0 && s.countdownLeft <= 0 {
		kgFinish(w, s.countdownTeam, "karity")
	}
}

func kgOnFloor(w *World) int {
	n := 0
	for _, it := range w.Items {
		if it.TimeMsLeft > 0 && it.Definition.String("name") == kgItem {
			n++
		}
	}
	return n
}

// kgDrop — один кристалл на полу. Без команды: поднять может любой.
func kgDrop(w *World, x, y float64) {
	if w.Catalog == nil || len(w.Items) >= 256 {
		return
	}
	d := w.Catalog.Spec("items", kgItem)
	if !d.Exists() {
		return
	}
	index := w.AllocEffectIndex()
	if index == 0 {
		return
	}
	x = math.Max(60, math.Min(w.Tiles.PixW-60, x))
	y = math.Max(60, math.Min(w.Tiles.PixH-60, y))
	x, y = w.Tiles.FindOpen(x, y, 40)
	w.Items = append(w.Items, &Item{Definition: d, LocalIndex: index, OwnerIndex: -1, RosterIndex: -1, Team: -1,
		X: x, Y: y, TimeMsLeft: 1 << 30})
}

// kgScatter — кристаллы павшего разлетаются кольцом вокруг места смерти.
func kgScatter(w *World, x, y float64, n int) {
	start := w.modes.rng.Float64() * 2 * math.Pi
	for i := 0; i < n; i++ {
		a := start + float64(i)*2*math.Pi/float64(max(1, n))
		r := 160 + float64(i%3)*110
		kgDrop(w, x+math.Cos(a)*r, y+math.Sin(a)*r)
	}
}

func kgPickUp(w *World, p *Player) {
	p.GamePoints = min(255, p.GamePoints+1)
}

func karitGrabOnKill(w *World, victim *Player) {
	if victim.GamePoints <= 0 {
		return
	}
	n := victim.GamePoints
	victim.GamePoints = 0
	kgScatter(w, victim.X, victim.Y, n)
}

func kgFinish(w *World, winner int, reason string) {
	if w.Finished {
		return
	}
	w.Finished = true
	w.WinnerTeam = winner
	w.WinReason = reason
}

func karitGrabFinishByTime(w *World) {
	switch {
	case w.TeamPoints[0] > w.TeamPoints[1]:
		kgFinish(w, 0, "time")
	case w.TeamPoints[1] > w.TeamPoints[0]:
		kgFinish(w, 1, "time")
	default:
		kgFinish(w, -1, "time")
	}
}

func encodeKaritGrabBlock(bw *protocol.BitWriter, w *World) {
	s := w.kg()
	bw.WriteIntMax1(s.countdownTeam)
	bw.WritePositiveIntMax511(min(511, max(0, s.countdownLeft)))
	bw.WriteBool(s.paused)
	next := int(s.nextSpawn - w.TickNum)
	bw.WritePositiveIntMax255(min(255, max(0, next)))
}

// karitGrabGoal — куда идти боту. Несущий много при отсчёте своих уходит к
// спавну, остальные собирают ближайшие кристаллы и держатся у шахты.
func karitGrabGoal(w *World, bot *Player, enemy *Player) (navPoint, bool) {
	s := w.kg()
	home := navPoint{bot.SpawnX, bot.SpawnY}
	if (s.countdownTeam == bot.Team && bot.GamePoints >= 2) || bot.GamePoints >= 7 {
		return home, true
	}
	enemyNear := enemy != nil && Dist(bot.X, bot.Y, enemy.X, enemy.Y) < 900
	if bot.HP*100 < bot.MaxHP*35 && bot.GamePoints > 0 {
		return home, true
	}
	best, bestD := navPoint{}, math.MaxFloat64
	for _, it := range w.Items {
		if it.TimeMsLeft <= 0 || it.Definition.String("name") != kgItem {
			continue
		}
		if d := Dist(bot.X, bot.Y, it.X, it.Y); d < bestD {
			best, bestD = navPoint{it.X, it.Y}, d
		}
	}
	if bestD < 2600 && (!enemyNear || bestD < 500) {
		return best, true
	}
	if enemy == nil || !enemyNear {
		a := float64(bot.Index) * 2.1
		return navPoint{s.mineX + math.Cos(a)*380, s.mineY + math.Sin(a)*380}, true
	}
	return navPoint{}, false
}
