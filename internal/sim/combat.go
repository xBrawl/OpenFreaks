package sim

import (
	"math"

	"github.com/freaks/openfreaks/internal/protocol"
)

func ComputeTarget(w *World, p *Player, a protocol.PlayerAction, rng int, absolute bool) (float64, float64) {
	return computeTarget(w, p, a, rng, absolute, false)
}
func computeTarget(w *World, p *Player, a protocol.PlayerAction, rng int, absolute, forceMax bool) (float64, float64) {
	return aimTarget(w, p, a, rng, targetAim{absolute: absolute, forceMax: forceMax})
}

// targetAim — как читать прицел навыка.
type targetAim struct {
	absolute bool // ввод — точка на карте, иначе смещение от героя
	forceMax bool // всегда на полную дальность
	// Ввод — смещение, и его длина задаёт дальность: телепорт, портал и
	// прыжок-рывок приземляются там, куда целились, а не всегда на максимум.
	aimed bool
	// Скорость снаряда, пикс/с: автоприцел берёт упреждение по бегущей цели.
	leadSpeed float64
}

// Упреждение автоприцела: полное легко обмануть сменой курса, поэтому
// снаряд летит в 60% пути, который цель пройдёт за время полёта, и не
// дальше 4 клеток.
const (
	leadShare = .6
	leadMaxPx = 1200
)

func aimTarget(w *World, p *Player, a protocol.PlayerAction, rng int, o targetAim) (float64, float64) {
	var dx, dy float64
	if o.absolute {
		dx = float64(a.X) - p.X
		dy = float64(a.Y) - p.Y
	} else {
		dx, dy = float64(a.X), float64(a.Y)
	}
	if (a.AutoAttack || a.TargetFound) && a.TargetObjectID/1000000 == 1 {
		if hint := w.FindChar(a.TargetObjectID % 1000000); hint != nil && hint.Alive && !hint.Abandoned && hint.Team != p.Team && w.CanSee(p, hint) {
			hx, hy := hint.X, hint.Y
			reach := Dist(p.X, p.Y, hx, hy)
			if isTitan(hint) {
				// Центр титана — под ящиком посреди арены; бить надо в ближний хитбокс.
				hx, hy = titanAimPoint(hint, p.X, p.Y)
				reach = bodyDist(hint, p.X, p.Y)
			} else if o.leadSpeed > 0 {
				hx, hy = leadPoint(p, hint, hx, hy, o.leadSpeed)
			}
			if reach <= float64(rng) {
				dx, dy = hx-p.X, hy-p.Y
			}
		}
	} else if dx == 0 && dy == 0 {
		best, bestD := (*Player)(nil), float64(rng)
		for _, e := range w.Characters() {
			if e == p || !e.Alive || e.Team == p.Team || e.IsBall() || !w.CanSee(p, e) {
				continue
			}
			if d := Dist(p.X, p.Y, e.X, e.Y); d <= float64(rng) && d < bestD {
				best, bestD = e, d
			}
		}
		if best != nil {
			dx, dy = best.X-p.X, best.Y-p.Y
		} else {
			dx = math.Cos(float64(p.AttackAngle) * math.Pi / 180)
			dy = math.Sin(float64(p.AttackAngle) * math.Pi / 180)
		}
	}
	length := math.Hypot(dx, dy)
	var dist float64
	if rng == 0 {
		dist = 0
	} else if o.forceMax || !o.absolute && !o.aimed {
		dist = float64(rng)
	} else {
		dist = math.Min(float64(rng), length)
	}
	if length < 1 {
		length = 1
	}
	return p.X + dx/length*dist, p.Y + dy/length*dist
}

// leadPoint — куда целиться в бегущую цель, чтобы снаряд со скоростью speed
// её встретил. Дальше точки, куда цель идёт, упреждение не уводит.
func leadPoint(p, t *Player, x, y, speed float64) (float64, float64) {
	if !t.HasMoveTarget || speed <= 0 {
		return x, y
	}
	dx, dy := t.TargetX-t.X, t.TargetY-t.Y
	d := math.Hypot(dx, dy)
	if d < 1 {
		return x, y
	}
	flight := Dist(p.X, p.Y, x, y) / speed
	step := math.Min(d, math.Min(leadMaxPx, DisplayedSpeed(t)*flight*leadShare))
	return x + dx/d*step, y + dy/d*step
}

func dashToward(w *World, p *Player, tx, ty, dist float64) {
	dx, dy := tx-p.X, ty-p.Y
	d := math.Hypot(dx, dy)
	if d < 1 {
		return
	}
	if d < dist {
		dist = d
	}
	nx, ny := p.X+dx/d*dist, p.Y+dy/d*dist
	movePlayer(w, p, nx-p.X, ny-p.Y)
	p.AttackAngle = NormalizeAngle(int(math.Atan2(dy, dx) * 180 / math.Pi))
}

func movePlayer(w *World, p *Player, dx, dy float64) {
	steps := int(math.Ceil(math.Max(math.Abs(dx), math.Abs(dy)) / 30))
	if steps < 1 {
		steps = 1
	}
	for i := 0; i < steps; i++ {
		nx := clampF(p.ExactX+dx/float64(steps), float64(p.CollisionRadius), w.Tiles.PixW-float64(p.CollisionRadius))
		ny := clampF(p.ExactY+dy/float64(steps), float64(p.CollisionRadius), w.Tiles.PixH-float64(p.CollisionRadius))
		if w.Tiles.CanOccupy(nx, ny, p.CollisionRadius) {
			p.ExactX, p.ExactY = nx, ny
		} else if w.Tiles.CanOccupy(nx, p.ExactY, p.CollisionRadius) {
			p.ExactX = nx
		} else if w.Tiles.CanOccupy(p.ExactX, ny, p.CollisionRadius) {
			p.ExactY = ny
		}
	}
	p.X = math.Round(p.ExactX)
	p.Y = math.Round(p.ExactY)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
