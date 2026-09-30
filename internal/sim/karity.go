package sim

import (
	"math"

	"github.com/freaks/openfreaks/internal/content"
)

// Мегабосс Карити в две фазы (режим mega_boss).
//
// Фаза 0 — шар karity_boss_character под общим ИИ ботов (плевок, прыжок) плюс
// «перекат» отсюда. Когда шар падает до нуля, бой не кончается: шар ныряет под
// арену (фаза 1, переход 5 с), и из-под пола встаёт титан karity_titan_character
// (фаза 2). У фазы 2 нет таймера: бой идёт до смерти титана или пока вся
// команда не ляжет разом.
//
// Титан не ходит. Каждое его действие начинается с зоны-предупреждения
// (type effect, только картинка): по её имени клиент понимает, что играть.
// karity_titan_sweep_front/back — рука проходит по полосе арены,
// karity_titan_slam — ладонь бьёт в точку (серией по одному бойцу),
// karity_titan_crush — титан валится телом на полосу вдоль себя.
// Урон считается здесь в момент удара.
// Ещё титан выпускает хватальщиков: те цепляются за бойца и тащат его ко рту.
//
// На половине здоровья титан закрывается щитом эмодзи: урон не проходит, пока
// команда не накидает эмоутов из боевой кнопки (общий счётчик, KarityEmote).
// Фаза и щит уходят клиенту отдельным блоком снимка (snapshot.go).
//
// Эмодзи-атаки: в фазе 0 шар замирает и выпускает кольцо летящих смайлов в две
// волны (karity_emoji_burst — предупреждение, karity_emoji_projectile — снаряды),
// в фазе 2 титан роняет с неба гигантские смайлы в круги на полу
// (karity_emoji_meteor, каждый круг падает через meteorWarnTicks после появления).

const (
	BossPhaseBall       = 0
	BossPhaseTransition = 1
	BossPhaseTitan      = 2

	karityBossName    = "karity_boss_character"
	karityTitanName   = "karity_titan_character"
	karityGrabberName = "karity_grabber_character"
	karityMinionName  = "karity_minion_character"

	// Переход: шар ныряет, через 2 с встаёт титан, ещё через 3 с он в бою.
	titanSpawnDelay = 40
	transitionTicks = 100
	// Фаза 2 без таймера, но сервер не держит бой дольше 10 минут.
	titanMaxTicks = 12000
	// Респаун в фазе 2 вдвое дольше: иначе команду не положить разом.
	titanRespawnTicks = 200

	emojisPerPlayer  = 5
	staggerTicks     = 80
	staggerDamagePct = 150
)

// Геометрия титана — в пикселях сервера от его позиции (центр арены). Совпадает
// с моделью (1 единица = 1 клетка = 300 px): голова у −x, руки по обе стороны
// у головы, ноги у +x; −y — ближе к камере.
var titanHitCircles = [][3]float64{{-2880, -420, 560}, {-1560, 0, 520}, {-360, 0, 480}, {660, 0, 450}}

var (
	titanMouth = [2]float64{-2700, -800}
	titanHands = [2][2]float64{{-3800, -1950}, {-3800, 1950}}
)

const (
	// Взмах: ладонь проходит по полосе |y| ∈ [1200, 2400] от левого края к ногам.
	sweepBandNear  = 1200.0
	sweepBandFar   = 2400.0
	sweepFromX     = -3900.0
	sweepToX       = 1500.0
	sweepWarnTicks = 26
	sweepMoveTicks = 12
	sweepDamage    = 1800
	sweepReach     = 450.0

	// Хлопки идут серией: три ладони подряд за одним бойцом, каждая — в точку,
	// где он стоит в момент замаха (клиент: KarityTitanRig.SlamWarnSeconds).
	slamWarnTicks = 18
	slamSeries    = 3
	slamGap       = 16
	slamRadius    = 480.0
	slamDamage    = 1300
	slamStunTicks = 20

	// Придавливание: титан валится телом на полосу вдоль себя. Без него у тела
	// было безопасно — взмахи бьют только дальние полосы.
	crushWarnTicks = 22
	crushFromX     = -3500.0
	crushToX       = 1200.0
	crushHalfWidth = 1100.0
	crushDamage    = 1500
	crushStunTicks = 16
	crushPush      = 650.0

	// Пауза между действиями титана (под щитом эмодзи — короче).
	titanPause         = 30
	titanShieldedPause = 24

	summonTicks    = 30
	summonCooldown = 240
	maxGrabbers    = 4

	grabRange           = 280.0
	grabDragSlow        = -45 // хватальщик с добычей идёт медленнее
	grabMaxTicks        = 160 // не дотащил за 8 с — кусает где есть
	grabDamagePerSecond = 80
	grabChompDamage     = 900

	// Перекат шара (фаза 0).
	rollEvery     = 180
	rollWarnTicks = 18
	rollLength    = 2600.0
	rollStep      = 90.0
	rollDamage    = 1100
	rollPush      = 500.0

	// Эмодзи-залп шара (фаза 0): замах, затем кольцо смайлов в две волны,
	// вторая сдвинута на полшага — между снарядами можно проскочить.
	burstEvery     = 250
	burstWarnTicks = 22
	burstWaveGap   = 9
	burstWaves     = 2
	burstBullets   = 12
	burstDamage    = 420
	burstRange     = 2700
	burstName      = "karity_emoji_projectile"

	// Эмодзи-дождь титана (фаза 2): круги появляются по очереди у бойцов,
	// смайл падает в круг через meteorWarnTicks.
	rainCooldown    = 300
	rainMeteors     = 7
	rainStagger     = 6
	meteorWarnTicks = 24
	meteorRadius    = 430.0
	meteorDamage    = 1000
	meteorPush      = 380.0
)

type karityState struct {
	titan        *Player
	action       string
	actionTick   int32
	targetX      float64
	targetY      float64
	hit          map[*Player]bool
	victim       *Player
	slams        [][2]float64
	nextAction   int32
	lastSummon   int32
	shieldDone   bool
	staggerUntil int32
	count        int
	nextRoll     int32
	nextBurst    int32
	burstTick    int32
	burstAim     int
	lastRain     int32
	meteors      [][2]float64
	// Куда хватальщики тащат добычу: ближайшая ко рту свободная точка.
	mouthX, mouthY float64
}

type karityRoll struct {
	startTick  int32
	dirX, dirY float64
	left       float64
	hit        map[*Player]bool
}

func isKarityMode(w *World) bool { return w.ModeName == "mega_boss" }

func isTitan(p *Player) bool { return p != nil && p.Definition.String("name") == karityTitanName }

func isGrabber(p *Player) bool {
	return p != nil && p.Definition.String("name") == karityGrabberName
}

// pick — детерминированный выбор 0..n-1: у симуляции нет генератора случайных чисел.
func pick(w *World, salt, n int) int {
	if n <= 1 {
		return 0
	}
	h := uint32(w.TickNum)*2654435761 + uint32(salt)*40503
	h ^= h >> 15
	return int(h % uint32(n))
}

func TickKarity(w *World) {
	if !isKarityMode(w) {
		return
	}
	if w.karity == nil {
		w.karity = &karityState{nextRoll: w.TickNum + rollEvery, nextBurst: w.TickNum + burstEvery/2 + rollEvery, lastRain: -rainCooldown / 2}
	}
	k := w.karity
	switch w.BossPhase {
	case BossPhaseBall:
		if !tickKarityBurst(w, k) {
			tickKarityRoll(w, k)
		}
	case BossPhaseTransition:
		tickKarityTransition(w, k)
	case BossPhaseTitan:
		tickTitan(w, k)
	}
	tickGrabbers(w, k)
}

// karityBallDown — шар упал: вместо победы начинается переход ко второй фазе.
// Возвращает true, если бой продолжается.
func karityBallDown(w *World, ball *Player) bool {
	if !isKarityMode(w) || w.BossPhase != BossPhaseBall {
		return false
	}
	w.BossPhase = BossPhaseTransition
	w.BossPhaseTick = w.TickNum
	ball.KarityRoll = nil
	ball.KarityCasting = false
	k := w.karity
	if k != nil {
		k.burstTick = 0
	}
	for _, m := range w.Minions {
		if m.Alive && m.Definition.String("name") == karityMinionName {
			Kill(w, nil, m)
		}
	}
	return true
}

func tickKarityTransition(w *World, k *karityState) {
	elapsed := w.TickNum - w.BossPhaseTick
	if elapsed == titanSpawnDelay && k.titan == nil {
		k.titan = spawnTitan(w)
		if t := k.titan; t != nil {
			k.mouthX, k.mouthY = w.Tiles.FindOpen(t.X+titanMouth[0], t.Y+titanMouth[1], 200)
		}
	}
	if elapsed >= transitionTicks {
		w.BossPhase = BossPhaseTitan
		w.BossPhaseTick = w.TickNum
		k.nextAction = w.TickNum + 20
		k.lastSummon = w.TickNum - summonCooldown/2
	}
}

func spawnTitan(w *World) *Player {
	var ball *Player
	for _, p := range w.Players {
		if p.Definition.String("name") == karityBossName {
			ball = p
			break
		}
	}
	if ball == nil {
		return nil
	}
	cx, cy := w.Tiles.PixW/2, w.Tiles.PixH/2
	t := SpawnMinion(w, ball, karityTitanName, cx, cy)
	if t == nil {
		return nil
	}
	// Тело лежит над всей ареной: ставим ровно в центр, мимо FindOpen.
	t.X, t.Y, t.ExactX, t.ExactY = cx, cy, cx, cy
	t.SpawnX, t.SpawnY = cx, cy
	t.MaxHP = t.MaxHP * max(3, w.MegaBossHumans) / 3
	t.HP = t.MaxHP
	// Пока встаёт — неуязвим.
	t.SpawnShield = true
	t.SpawnShieldTicks = transitionTicks - titanSpawnDelay
	t.AttackAngle, t.MoveAngle = 0, 0
	return t
}

// ───────────── Фаза 2: титан ─────────────

func tickTitan(w *World, k *karityState) {
	t := k.titan
	if t == nil || !t.Alive {
		return
	}
	if int(w.TickNum-w.BossPhaseTick) >= titanMaxTicks {
		w.Finished = true
		w.WinnerTeam = t.Team
		w.WinReason = "boss_survived"
		return
	}
	if !k.shieldDone && t.HP*2 <= t.MaxHP {
		k.shieldDone = true
		raiseEmojiShield(w)
	}
	if k.action != "" {
		tickTitanAction(w, k, t)
		return
	}
	if w.TickNum < k.staggerUntil || w.TickNum < k.nextAction {
		return
	}
	startTitanAction(w, k, t)
}

func titanTargets(w *World, t *Player) []*Player {
	var out []*Player
	for _, p := range w.Players {
		if p.Alive && !p.Abandoned && p.Team != t.Team && !p.IsBall() {
			out = append(out, p)
		}
	}
	return out
}

func inSweepBand(t, p *Player, back bool) bool {
	y := p.Y - t.Y
	if !back {
		y = -y
	}
	x := p.X - t.X
	return y >= sweepBandNear-200 && y <= sweepBandFar+200 && x >= sweepFromX-300 && x <= sweepToX+300
}

// inCrush — боец в полосе вдоль тела, куда титан валится придавливанием.
func inCrush(t, p *Player) bool {
	x := p.X - t.X
	return math.Abs(p.Y-t.Y) <= crushHalfWidth+float64(p.CollisionRadius) && x >= crushFromX-100 && x <= crushToX+100
}

// slamVictim — за кем идёт серия хлопков: обычно тот, кто жмётся к телу.
func slamVictim(w *World, k *karityState, t *Player, targets []*Player) *Player {
	if pick(w, k.count+11, 10) >= 7 {
		return targets[pick(w, k.count+7, len(targets))]
	}
	var best *Player
	for _, p := range targets {
		if best == nil || bodyDist(t, p.X, p.Y) < bodyDist(t, best.X, best.Y) {
			best = p
		}
	}
	return best
}

func startTitanAction(w *World, k *karityState, t *Player) {
	targets := titanTargets(w, t)
	if len(targets) == 0 {
		return
	}
	var front, back, near bool
	for _, p := range targets {
		front = front || inSweepBand(t, p, false)
		back = back || inSweepBand(t, p, true)
		near = near || inCrush(t, p)
	}
	grabbers := 0
	for _, m := range w.Minions {
		if m.Alive && isGrabber(m) {
			grabbers++
		}
	}
	options := []string{"slam", "slam"}
	if near {
		options = append(options, "crush", "crush", "crush")
	}
	if front {
		options = append(options, "sweep_front", "sweep_front")
	}
	if back {
		options = append(options, "sweep_back", "sweep_back")
	}
	if grabbers < maxGrabbers-1 && w.TickNum-k.lastSummon >= summonCooldown {
		options = append(options, "summon", "summon")
	}
	if w.TickNum-k.lastRain >= rainCooldown {
		options = append(options, "rain", "rain", "rain")
	}
	k.count++
	k.action = options[pick(w, k.count, len(options))]
	k.actionTick = w.TickNum
	k.hit = map[*Player]bool{}
	switch k.action {
	case "sweep_front", "sweep_back":
		sign := -1.0
		if k.action == "sweep_back" {
			sign = 1
		}
		k.targetX = t.X + (sweepFromX+sweepToX)/2
		k.targetY = t.Y + sign*(sweepBandNear+sweepBandFar)/2
		SpawnArea(w, t, "karity_titan_"+k.action, k.targetX, k.targetY, 0, 0, false, false)
	case "slam":
		k.victim = slamVictim(w, k, t, targets)
		k.slams = k.slams[:0]
	case "crush":
		k.targetX, k.targetY = t.X+(crushFromX+crushToX)/2, t.Y
		SpawnArea(w, t, "karity_titan_crush", k.targetX, k.targetY, 0, 0, false, false)
	case "summon":
		k.lastSummon = w.TickNum
		SpawnArea(w, t, "karity_titan_summon", t.X, t.Y, 0, 0, false, false)
	case "rain":
		k.lastRain = w.TickNum
		k.meteors = k.meteors[:0]
	}
}

func tickTitanAction(w *World, k *karityState, t *Player) {
	elapsed := int(w.TickNum - k.actionTick)
	done := false
	switch k.action {
	case "sweep_front", "sweep_back":
		if elapsed < sweepWarnTicks {
			break
		}
		move := elapsed - sweepWarnTicks
		handX := t.X + sweepFromX + (sweepToX-sweepFromX)*float64(move)/float64(sweepMoveTicks)
		back := k.action == "sweep_back"
		for _, p := range titanTargets(w, t) {
			if k.hit[p] || !inSweepBand(t, p, back) || math.Abs(p.X-handX) > sweepReach {
				continue
			}
			k.hit[p] = true
			applyDamage(w, t, p, sweepDamage, false)
			if p.Alive {
				out := -1.0
				if back {
					out = 1
				}
				movePlayer(w, p, rollPush, out*400)
			}
		}
		done = move >= sweepMoveTicks
	case "slam":
		// Ладонь i замахивается на тике i*slamGap туда, где сейчас боец, и бьёт
		// через slamWarnTicks. Если боец пал, серия добивает по последней точке.
		if elapsed%slamGap == 0 && elapsed/slamGap < slamSeries {
			x, y := k.targetX, k.targetY
			if v := k.victim; v != nil && v.Alive && !v.Abandoned {
				x, y = v.X, v.Y
			}
			k.targetX, k.targetY = x, y
			k.slams = append(k.slams, [2]float64{x, y})
			SpawnArea(w, t, "karity_titan_slam", x, y, 0, 0, false, false)
		}
		for i, at := range k.slams {
			if elapsed != i*slamGap+slamWarnTicks {
				continue
			}
			for _, p := range titanTargets(w, t) {
				d := Dist(p.X, p.Y, at[0], at[1])
				if d > slamRadius {
					continue
				}
				applyDamage(w, t, p, slamDamage, false)
				if p.Alive {
					AddStatus(w, p, "stun", 1, slamStunTicks, t.ObjectIndex)
					movePlayer(w, p, (p.X-at[0])/math.Max(d, 1)*300, (p.Y-at[1])/math.Max(d, 1)*300)
				}
			}
		}
		done = elapsed >= (slamSeries-1)*slamGap+slamWarnTicks
	case "crush":
		if elapsed < crushWarnTicks {
			break
		}
		for _, p := range titanTargets(w, t) {
			if !inCrush(t, p) {
				continue
			}
			applyDamage(w, t, p, crushDamage, false)
			if p.Alive {
				AddStatus(w, p, "stun", 1, crushStunTicks, t.ObjectIndex)
				out := 1.0
				if p.Y < t.Y {
					out = -1
				}
				movePlayer(w, p, 0, out*crushPush)
			}
		}
		done = true
	case "rain":
		// Круг i появляется на тике i*rainStagger у одного из бойцов (через раз —
		// с упреждением в сторону), смайл падает в него через meteorWarnTicks.
		if elapsed%rainStagger == 0 && elapsed/rainStagger < rainMeteors {
			i := elapsed / rainStagger
			if targets := titanTargets(w, t); len(targets) > 0 {
				v := targets[pick(w, k.count*7+i, len(targets))]
				x, y := v.X, v.Y
				if i%2 == 1 {
					a := float64(pick(w, k.count*13+i, 360)) * math.Pi / 180
					x += math.Cos(a) * 360
					y += math.Sin(a) * 360
				}
				x = clampF(x, 300, w.Tiles.PixW-300)
				y = clampF(y, 300, w.Tiles.PixH-300)
				k.meteors = append(k.meteors, [2]float64{x, y})
				SpawnArea(w, t, "karity_emoji_meteor", x, y, 0, 0, false, false)
			}
		}
		for i, m := range k.meteors {
			if elapsed != i*rainStagger+meteorWarnTicks {
				continue
			}
			for _, p := range titanTargets(w, t) {
				d := Dist(p.X, p.Y, m[0], m[1])
				if d > meteorRadius {
					continue
				}
				applyDamage(w, t, p, meteorDamage, false)
				if p.Alive {
					movePlayer(w, p, (p.X-m[0])/math.Max(d, 1)*meteorPush, (p.Y-m[1])/math.Max(d, 1)*meteorPush)
				}
			}
		}
		done = elapsed >= (rainMeteors-1)*rainStagger+meteorWarnTicks
	case "summon":
		if elapsed == summonTicks/2 {
			for _, hand := range titanHands {
				SpawnMinion(w, t, karityGrabberName, t.X+hand[0], t.Y+hand[1])
			}
		}
		done = elapsed >= summonTicks
	default:
		done = true
	}
	if done {
		k.action = ""
		k.victim = nil
		pause := int32(titanPause)
		if w.EmojiShieldLeft > 0 {
			pause = titanShieldedPause
		}
		k.nextAction = w.TickNum + pause
	}
}

// ───────────── Щит эмодзи ─────────────

func raiseEmojiShield(w *World) {
	humans := 0
	for _, p := range w.Players {
		if !p.IsBot && !p.Abandoned && !p.IsBall() && p.Team == 0 {
			humans++
		}
	}
	if humans == 0 {
		return
	}
	w.EmojiShieldTotal = min(1023, emojisPerPlayer*humans)
	w.EmojiShieldLeft = w.EmojiShieldTotal
}

// KarityEmote — игрок index кинул эмоут в бою: трескает щит титана.
func (w *World) KarityEmote(index int) {
	if w.BossPhase != BossPhaseTitan || w.EmojiShieldLeft <= 0 || w.karity == nil {
		return
	}
	for _, p := range w.Players {
		if p.Index != index || p.IsBot || p.Abandoned || p.Team != 0 {
			continue
		}
		w.EmojiShieldLeft--
		if w.EmojiShieldLeft == 0 {
			// Щит лопнул: титан оглушён и получает больше урона.
			w.karity.staggerUntil = w.TickNum + staggerTicks
			w.karity.action = ""
			if t := w.karity.titan; t != nil && t.Alive {
				AddStatus(w, t, "stun", 1, staggerTicks, t.ObjectIndex)
			}
		}
		return
	}
}

// titanDamage — урон по титану: под щитом эмодзи не проходит, в оглушении больше.
func titanDamage(w *World, victim *Player, amount int) int {
	if !isTitan(victim) {
		return amount
	}
	if w.EmojiShieldLeft > 0 {
		return 0
	}
	if w.karity != nil && w.TickNum < w.karity.staggerUntil {
		return amount * staggerDamagePct / 100
	}
	return amount
}

// ───────────── Хватальщики ─────────────

func tickGrabbers(w *World, k *karityState) {
	for _, g := range w.Minions {
		if !g.Alive || !isGrabber(g) {
			continue
		}
		v := g.KarityGrab
		if v != nil && (!v.Alive || v.Abandoned) {
			g.KarityGrab = nil
			v = nil
		}
		if v == nil {
			for _, p := range w.Players {
				if p.Alive && !p.Abandoned && p.Team != g.Team && !p.IsBall() && !grabbed(w, p) &&
					Dist(g.X, g.Y, p.X, p.Y) <= grabRange {
					g.KarityGrab = p
					g.KarityGrabTick = w.TickNum
					v = p
					break
				}
			}
			if v == nil {
				continue
			}
		}
		// Тащит ко рту титана (в обход стен); боец едет следом и почти не может идти сам.
		mx, my := g.X, g.Y
		if t := k.titan; t != nil && t.Alive {
			mx, my = k.mouthX, k.mouthY
		}
		if g.Brain == nil {
			g.Brain = &BotBrain{EnemyIndex: -1}
		}
		botNavigate(w, g, navPoint{mx, my}, int(w.TickNum))
		AddStatus(w, g, "speed", grabDragSlow, 3, g.ObjectIndex)
		d := Dist(g.X, g.Y, mx, my)
		if !g.HasMoveTarget && d > 1 {
			// Пути нет (зажат у стены) — хоть напрямую.
			movePlayer(w, g, (mx-g.X)/d*20, (my-g.Y)/d*20)
		}
		ox, oy := v.X-g.X, v.Y-g.Y
		od := math.Max(1, math.Hypot(ox, oy))
		movePlayer(w, v, g.X+ox/od*160-v.X, g.Y+oy/od*160-v.Y)
		AddStatus(w, v, "speed", -85, 3, g.ObjectIndex)
		if w.TickNum%20 == 0 {
			applyDamage(w, g, v, grabDamagePerSecond, false)
		}
		if (d <= 350 || w.TickNum-g.KarityGrabTick >= grabMaxTicks) && v.Alive {
			applyDamage(w, g, v, grabChompDamage, false)
			g.KarityGrab = nil
			Kill(w, nil, g)
		}
	}
}

func grabbed(w *World, p *Player) bool {
	for _, g := range w.Minions {
		if g.Alive && g.KarityGrab == p {
			return true
		}
	}
	return false
}

// ───────────── Фаза 0: перекат шара ─────────────

func tickKarityRoll(w *World, k *karityState) {
	var ball *Player
	for _, p := range w.Players {
		if p.Alive && p.Definition.String("name") == karityBossName {
			ball = p
			break
		}
	}
	if ball == nil {
		return
	}
	if r := ball.KarityRoll; r != nil {
		elapsed := int(w.TickNum - r.startTick)
		if elapsed < rollWarnTicks {
			ball.HasMoveTarget = false
			return
		}
		step := math.Min(rollStep, r.left)
		px, py := ball.X, ball.Y
		movePlayer(w, ball, r.dirX*step, r.dirY*step)
		moved := Dist(px, py, ball.X, ball.Y)
		r.left -= step
		for _, p := range titanTargets(w, ball) {
			if r.hit[p] || Dist(p.X, p.Y, ball.X, ball.Y) > float64(ball.CollisionRadius+p.CollisionRadius) {
				continue
			}
			r.hit[p] = true
			applyDamage(w, ball, p, rollDamage, false)
			if p.Alive {
				// Сбивает в сторону от линии качения.
				side := 1.0
				if (p.X-ball.X)*r.dirY-(p.Y-ball.Y)*r.dirX > 0 {
					side = -1
				}
				movePlayer(w, p, -r.dirY*side*rollPush, r.dirX*side*rollPush)
			}
		}
		if r.left <= 0 || moved < step*.5 {
			ball.KarityRoll = nil
			k.nextRoll = w.TickNum + rollEvery
		}
		return
	}
	if w.TickNum < k.nextRoll || len(ball.Executions) > 0 || ball.Charged {
		return
	}
	targets := titanTargets(w, ball)
	if len(targets) == 0 {
		return
	}
	k.count++
	victim := targets[pick(w, k.count, len(targets))]
	dx, dy := victim.X-ball.X, victim.Y-ball.Y
	d := math.Hypot(dx, dy)
	if d < 300 {
		k.nextRoll = w.TickNum + 20
		return
	}
	r := &karityRoll{startTick: w.TickNum, dirX: dx / d, dirY: dy / d, left: rollLength, hit: map[*Player]bool{}}
	ball.KarityRoll = r
	ball.HasMoveTarget = false
	endX := clampF(ball.X+r.dirX*rollLength, 0, w.Tiles.PixW)
	endY := clampF(ball.Y+r.dirY*rollLength, 0, w.Tiles.PixH)
	SpawnArea(w, ball, "karity_roll_lane", endX, endY, 0, 0, false, false)
}

// KarityTimeless — таймер боя больше не решает исход: шар уже пал, дальше титан.
// Не Endless: тот ещё меняет, как бой обходится с отключившимися игроками.
func KarityTimeless(w *World) bool { return isKarityMode(w) && w.BossPhase != BossPhaseBall }

// karityBusy — шар или титан заняты своим действием: общий ИИ ботов их не трогает.
func karityBusy(p *Player) bool { return p.KarityRoll != nil || p.KarityCasting || isTitan(p) }

// tickKarityBurst — эмодзи-залп шара. true, пока залп идёт (перекат ждёт).
func tickKarityBurst(w *World, k *karityState) bool {
	var ball *Player
	for _, p := range w.Players {
		if p.Alive && p.Definition.String("name") == karityBossName {
			ball = p
			break
		}
	}
	if ball == nil {
		return false
	}
	if k.burstTick > 0 {
		ball.HasMoveTarget = false
		elapsed := int(w.TickNum - k.burstTick)
		for wave := 0; wave < burstWaves; wave++ {
			if elapsed != burstWarnTicks+wave*burstWaveGap {
				continue
			}
			var d content.Spec
			if w.Catalog != nil {
				d = w.Catalog.Spec("projectiles", burstName)
			}
			step := 360 / burstBullets
			for i := 0; i < burstBullets; i++ {
				angle := k.burstAim + i*step + wave*step/2
				rad := float64(angle) * math.Pi / 180
				spawnProjectile(w, ball, d, angle, burstDamage, burstRange,
					ball.X+math.Cos(rad)*burstRange, ball.Y+math.Sin(rad)*burstRange, false, 0)
			}
		}
		if elapsed >= burstWarnTicks+(burstWaves-1)*burstWaveGap {
			k.burstTick = 0
			ball.KarityCasting = false
			k.nextBurst = w.TickNum + burstEvery
			k.nextRoll = max(k.nextRoll, w.TickNum+60)
		}
		return true
	}
	if w.TickNum < k.nextBurst || ball.KarityRoll != nil || len(ball.Executions) > 0 || ball.Charged {
		return false
	}
	var aim *Player
	best := math.MaxFloat64
	for _, p := range titanTargets(w, ball) {
		if d := Dist(p.X, p.Y, ball.X, ball.Y); d < best {
			best, aim = d, p
		}
	}
	if aim == nil || best > burstRange {
		k.nextBurst = w.TickNum + 20
		return false
	}
	// Первый снаряд первой волны летит точно в ближайшего бойца.
	k.burstAim = angleTo(aim.X-ball.X, aim.Y-ball.Y)
	k.burstTick = w.TickNum
	ball.KarityCasting = true
	ball.HasMoveTarget = false
	SpawnArea(w, ball, "karity_emoji_burst", ball.X, ball.Y, 0, 0, true, false)
	return true
}

// ───────────── Попадания по титану ─────────────

// hitCircles — чем персонаж ловит попадания. У титана — несколько кругов вдоль тела.
func hitCircles(v *Player) [][3]float64 {
	if !isTitan(v) {
		return [][3]float64{{v.X, v.Y, float64(v.CollisionRadius)}}
	}
	out := make([][3]float64, len(titanHitCircles))
	for i, c := range titanHitCircles {
		out[i] = [3]float64{v.X + c[0], v.Y + c[1], c[2]}
	}
	return out
}

// titanAimPoint — куда стрелять в титана из точки (x, y): в ближний хитбокс,
// на полрадиуса внутрь от края.
func titanAimPoint(v *Player, x, y float64) (float64, float64) {
	best, bestD := [3]float64{v.X, v.Y, 0}, math.MaxFloat64
	for _, c := range hitCircles(v) {
		if d := Dist(x, y, c[0], c[1]) - c[2]; d < bestD {
			best, bestD = c, d
		}
	}
	dx, dy := x-best[0], y-best[1]
	d := math.Max(1, math.Hypot(dx, dy))
	return best[0] + dx/d*best[2]*.5, best[1] + dy/d*best[2]*.5
}

// bodyDist — расстояние от точки до края тела (0 — внутри).
func bodyDist(v *Player, x, y float64) float64 {
	best := math.MaxFloat64
	for _, c := range hitCircles(v) {
		best = math.Min(best, math.Max(0, Dist(x, y, c[0], c[1])-c[2]))
	}
	return best
}

// titanTeamWiped — фаза 2: команда легла разом, титан победил.
func titanTeamWiped(w *World) bool {
	if w.BossPhase != BossPhaseTitan {
		return false
	}
	anyone := false
	for _, p := range w.Players {
		if p.Team != 0 || p.Abandoned || p.IsBall() {
			continue
		}
		anyone = true
		if p.Alive {
			return false
		}
	}
	return anyone
}
