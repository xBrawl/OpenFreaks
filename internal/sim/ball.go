package sim

import (
	"math"

	"github.com/freaks/openfreaks/internal/protocol"
)

const defaultGoalsToWin = 3

// Ворота: проём в стене у края поля (карта experimental_football). Зона гола —
// весь проём целиком: раньше она была на клетку уже проёма, и мяч, пронесённый
// у штанги, гол не давал. Клиент ставит раму по тем же числам (FootballGoals).
const (
	goalHalfWidth     = 750
	goalLine          = 1200
	carriedGoalMargin = 120
)

// Супер-удар мячом (ульта с мячом в руках). Раньше он отличался от паса только
// дальностью, и то если стик дотянут до упора: тестеры тратили ульту впустую.
// Теперь мяч всегда летит на всю дальность, почти вдвое быстрее, ломает
// разрушаемые укрытия и расталкивает соперников на пути.
const (
	passSpeed      = 150
	superKickSpeed = 270
	superKickRange = 3600
	superKickReach = 220
	superKickPush  = 600
)

// BallHorizontal: ворота слева и справа — поле шире, чем выше. Так футбол
// устроен как остальные командные режимы: команда 0 слева, 1 справа, а клиент
// переворачивает поле второй команде, и каждый атакует слева направо. Прежнее
// вертикальное поле ставило обе команды у верхних ворот: атаковать приходилось
// сверху вниз, и тестеры жаловались на однобокие матчи.
func (w *World) BallHorizontal() bool { return w.Tiles.PixW > w.Tiles.PixH }

// ballAxes раскладывает точку на ось «ворота — ворота» и поперечную.
func (w *World) ballAxes(x, y float64) (along, across, length, width float64) {
	if w.BallHorizontal() {
		return x, y, w.Tiles.PixW, w.Tiles.PixH
	}
	return y, x, w.Tiles.PixH, w.Tiles.PixW
}

// scoringTeam — чья команда забила, если мяч у этого конца поля (-1 — ничья).
func (w *World) scoringTeam(along, length, line float64) int {
	low, high := along <= line, along >= length-line
	if !low && !high {
		return -1
	}
	if w.BallHorizontal() {
		// Команда 0 начинает слева и бьёт в правые ворота.
		if high {
			return 0
		}
		return 1
	}
	if high {
		return 1
	}
	return 0
}

func BallTick(w *World) {
	ball := w.Ball
	if ball == nil {
		return
	}
	if w.RoundResetTicks > 0 {
		w.RoundResetTicks--
		if w.RoundResetTicks == 0 {
			resetRound(w)
		}
		return
	}

	carried := false
	for _, p := range w.Players {
		if p.Alive && !p.Abandoned && !p.IsBall() && p.CarryableObjectID == ball.ObjectIndex {
			ball.X, ball.Y = p.X, p.Y
			carried = true
			w.hl().ballTouch = p.Index
			if tackler := findTackler(w, p); tackler != nil {
				tackle(w, p, tackler)
				carried = false
			}
			break
		}
	}
	if !carried && ball.HasMoveTarget {
		super := ball.LastThrowType == 1
		dx, dy := ball.TargetX-ball.X, ball.TargetY-ball.Y
		distance := math.Hypot(dx, dy)
		if distance > 1 {
			speed := float64(passSpeed)
			if super {
				speed = superKickSpeed
			}
			step := math.Min(speed, distance)
			mx, my := dx/distance*step, dy/distance*step
			nx, ny := ball.X+mx, ball.Y+my
			if super && !w.Tiles.CanOccupy(nx, ny, 100) && breakTiles(w, nx, ny, 100) && w.Tiles.CanOccupy(nx, ny, 100) {
				ball.X, ball.Y = nx, ny
			} else if w.Tiles.CanOccupy(nx, ny, 100) {
				ball.X, ball.Y = nx, ny
			} else {

				if !w.Tiles.CanOccupy(nx, ball.Y, 100) {
					dx = -dx
				} else {
					ball.X = nx
				}
				if !w.Tiles.CanOccupy(ball.X, ny, 100) {
					dy = -dy
				} else {
					ball.Y = ny
				}
				ball.TargetX, ball.TargetY = ball.X+dx*.8, ball.Y+dy*.8
			}
		}
		if super {
			superKickPushEnemies(w, ball, dx, dy)
		}
		if distance < 160 {
			ball.HasMoveTarget = false
		}
	}
	// Внёс мяч в ворота — гол, как и после удара. Пока мяч в руках, его
	// центр — это центр героя, а тело героя заходит в створ раньше: даём
	// запас, иначе игрок стоит «в воротах», а гола нет.
	line := float64(goalLine)
	if carried {
		line += carriedGoalMargin
	}
	along, across, length, width := w.ballAxes(ball.X, ball.Y)
	if team := w.scoringTeam(along, length, line); team >= 0 && math.Abs(across-width/2) < goalHalfWidth {
		w.AddPoint(team, 1)
		w.BallWinnerTeam = team
		w.RoundResetTicks = 40
		winning := w.GetPoint(team) >= w.GoalsForWin() || w.BallOvertime
		w.noteGoal(team, winning)
		if winning {
			w.WinnerTeam = team
			w.Finished = true
			w.WinReason = "goals"
		}
		return
	}
	if carried {
		return
	}

	if ball.ThrowCooldown > 0 {
		ball.ThrowCooldown--
		return
	}

	var pickup *Player
	best := math.MaxFloat64
	for _, p := range w.Players {
		if !p.Alive || p.Abandoned || p.IsBall() {
			continue
		}
		d := Dist(p.X, p.Y, ball.X, ball.Y)
		if d < float64(p.CollisionRadius+150) && (d < best || (d == best && p.Index < pickup.Index)) {
			pickup, best = p, d
		}
	}
	if pickup != nil {
		pickup.CarryableObjectID = ball.ObjectIndex
		ball.HasMoveTarget = false
		ball.LastThrowTeam = pickup.Team
	}
}

func ThrowBall(w *World, p *Player, a protocol.PlayerAction, ultimate bool) bool {
	ball := w.Ball
	if ball == nil || !p.Alive || p.Abandoned || p.ControlRemoved || w.RoundResetTicks > 0 || w.Finished || int(w.TickNum) < w.IntroTicks || p.CarryableObjectID != ball.ObjectIndex {
		return false
	}
	if ultimate {
		if p.UltCharge < 4000 {
			return false
		}
		p.UltCharge = 0
		p.UltimateUses++
	} else {
		if len(p.Skills) == 0 || p.Skills[0].Charges <= 0 {
			return false
		}
		p.Skills[0].Charges--
		p.Skills[0].RechargeLeft = p.Skills[0].RechargeMs
	}
	rng := 2000
	throwType := 0
	tx, ty := 0.0, 0.0
	if ultimate {
		throwType = 1
		w.superKickHit = map[int]bool{}
		tx, ty = computeTarget(w, p, a, superKickRange, false, true)
	} else {
		tx, ty = ComputeTarget(w, p, a, rng, false)
	}
	// Удар от края поля целит за карту: отрицательная точка роняла снапшот
	// (координаты пишутся беззнаковыми) и обрывала весь матч.
	tx, ty = max(0, tx), max(0, ty)
	p.CarryableAimX, p.CarryableAimY = int(tx), int(ty)
	p.CarryableObjectID = 0
	ball.TargetX, ball.TargetY = tx, ty
	ball.X, ball.Y = p.X, p.Y
	ball.HasMoveTarget = true
	ball.ThrowCooldown = 6
	ball.LastThrowTeam = p.Team
	ball.LastThrowType = throwType
	return true
}

const (
	tackleReach    = 120
	tackleKnock    = 700
	tackleCooldown = 8
)

func findTackler(w *World, carrier *Player) *Player {
	for _, p := range w.Players {
		if p == carrier || !p.Alive || p.Abandoned || p.IsBall() || p.Team == carrier.Team || p.DashActiveTicks <= 0 {
			continue
		}
		if Dist(p.X, p.Y, carrier.X, carrier.Y) <= float64(p.CollisionRadius+carrier.CollisionRadius+tackleReach) {
			return p
		}
	}
	return nil
}

func tackle(w *World, carrier, tackler *Player) {
	ball := w.Ball
	carrier.CarryableObjectID = 0
	dx, dy := carrier.X-tackler.X, carrier.Y-tackler.Y
	if d := math.Hypot(dx, dy); d > 1 {
		dx, dy = dx/d, dy/d
	} else {
		dx, dy = 0, 1
	}
	ball.X, ball.Y = carrier.X, carrier.Y
	ball.TargetX, ball.TargetY = carrier.X+dx*tackleKnock, carrier.Y+dy*tackleKnock
	ball.HasMoveTarget = true
	ball.ThrowCooldown = tackleCooldown
	ball.LastThrowTeam = tackler.Team
	ball.LastThrowType = 0
}

// breakTiles ломает разрушаемые клетки под мячом; true — хоть одна сломалась.
func breakTiles(w *World, px, py float64, radius int) bool {
	broke := false
	r := float64(radius)
	for y := int((py - r) / PixelsPerTile); y <= int((py+r)/PixelsPerTile); y++ {
		for x := int((px - r) / PixelsPerTile); x <= int((px+r)/PixelsPerTile); x++ {
			if w.Tiles.DestroyTile(x, y) {
				broke = true
			}
		}
	}
	return broke
}

// superKickPushEnemies отталкивает соперников, которых задел супер-удар, по
// направлению полёта мяча. Каждого — один раз за удар.
func superKickPushEnemies(w *World, ball *Player, dx, dy float64) {
	d := math.Hypot(dx, dy)
	if d < 1 {
		return
	}
	dx, dy = dx/d, dy/d
	for _, e := range w.Players {
		if e == ball || !e.Alive || e.Abandoned || e.IsBall() || e.Team == ball.LastThrowTeam || w.superKickHit[e.ObjectIndex] {
			continue
		}
		if Dist(e.X, e.Y, ball.X, ball.Y) > float64(superKickReach+e.CollisionRadius) {
			continue
		}
		if w.superKickHit == nil {
			w.superKickHit = map[int]bool{}
		}
		w.superKickHit[e.ObjectIndex] = true
		movePlayer(w, e, dx*superKickPush, dy*superKickPush)
	}
}

func DropBall(w *World, p *Player) {
	if w.Ball != nil {
		w.Ball.X, w.Ball.Y = p.X, p.Y
		w.Ball.HasMoveTarget = false
		w.Ball.ThrowCooldown = 4
	}
	p.CarryableObjectID = 0
}

func (w *World) GoalsForWin() int {
	if w.GoalsToWin > 0 {
		return w.GoalsToWin
	}
	return defaultGoalsToWin
}

func resetRound(w *World) {
	for _, p := range w.Players {
		if p.Abandoned || p.IsBall() {
			continue
		}
		p.Alive = true
		p.HP = p.MaxHP
		p.State = StateRespawn
		p.CarryableObjectID = 0
		p.Executions = nil
		p.Effects = nil
		p.ControlRemoved = false
		p.Charged = false
		p.Teleporting = false
		p.SpawnShield = true
		p.SpawnShieldTicks = 40
		for _, sk := range p.Skills {
			sk.Charges = sk.MaxCharge
			sk.CoolLeft = 0
			sk.ActiveLeft = 0
			sk.CastPending = false
		}
		p.HasMoveTarget = false
		nx, ny := w.Tiles.FindOpen(p.SpawnX, p.SpawnY, p.CollisionRadius)
		p.X, p.Y, p.ExactX, p.ExactY = nx, ny, nx, ny
	}
	if w.Ball != nil {
		w.Ball.X, w.Ball.Y = w.Tiles.PixW/2, w.Tiles.PixH/2
		w.Ball.ExactX, w.Ball.ExactY = w.Ball.X, w.Ball.Y
		w.Ball.HasMoveTarget = false

		w.Ball.ThrowCooldown = 0
		w.Ball.LastThrowTeam = -1
	}
	w.BallWinnerTeam = -1
}
