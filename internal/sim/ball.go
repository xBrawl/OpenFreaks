package sim

import (
	"math"

	"github.com/freaks/openfreaks/internal/protocol"
)

const defaultGoalsToWin = 3

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
			if tackler := findTackler(w, p); tackler != nil {
				tackle(w, p, tackler)
				carried = false
			}
			break
		}
	}
	if !carried && ball.HasMoveTarget {
		dx, dy := ball.TargetX-ball.X, ball.TargetY-ball.Y
		distance := math.Hypot(dx, dy)
		if distance > 1 {
			step := math.Min(150, distance)
			mx, my := dx/distance*step, dy/distance*step
			nx, ny := ball.X+mx, ball.Y+my
			if w.Tiles.CanOccupy(nx, ny, 100) {
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
		if distance < 160 {
			ball.HasMoveTarget = false
		}
	}
	if math.Abs(ball.X-w.Tiles.PixW/2) < 600 && (ball.Y <= 1200 || ball.Y >= w.Tiles.PixH-1200) {
		team := 0
		if ball.Y >= w.Tiles.PixH-1200 {
			team = 1
		}
		w.AddPoint(team, 1)
		w.BallWinnerTeam = team
		w.RoundResetTicks = 40
		if w.GetPoint(team) >= w.GoalsForWin() || w.BallOvertime {
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
	if ultimate {
		rng = 3000
		throwType = 1
	}
	tx, ty := ComputeTarget(w, p, a, rng, false)
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
