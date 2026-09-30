package sim

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"math"
)

type BotBrain struct {
	Path                                     []navPoint
	Goal, Position, EnemyPosition            navPoint
	ReplanAt, EnemyIndex, EnemySeenAt, Stuck int
}

func BotStep(w *World, tick int) {
	if w.Finished || tick < w.IntroTicks {
		return
	}
	for _, bot := range w.Players {
		if (!bot.IsBot && !bot.BotControlled) || !bot.Alive || bot.Abandoned || bot.IsBall() || bot.ControlRemoved || karityBusy(bot) || (tick+bot.Index)%4 != 0 {
			continue
		}
		if bot.Brain == nil {
			bot.Brain = &BotBrain{EnemyIndex: -1}
		}
		brain := bot.Brain
		if bot.HasMoveTarget && Dist(bot.X, bot.Y, brain.Position.x, brain.Position.y) < 8 {
			brain.Stuck++
		} else {
			brain.Stuck = 0
		}
		brain.Position = navPoint{bot.X, bot.Y}
		if modeBot(w, bot, tick) {
			continue
		}
		enemy := nearestEnemy(w, bot)
		goal := navPoint{w.Tiles.PixW / 2, w.Tiles.PixH / 2}
		if enemy != nil && len(bot.Skills) > 0 {
			sk := bot.Skills[0]
			d := Dist(bot.X, bot.Y, enemy.X, enemy.Y)
			los := shotClear(w, bot.X, bot.Y, enemy.X, enemy.Y, sk)
			goal = navPoint{enemy.X, enemy.Y}
			preferred := float64(sk.RangePx) * .72
			retreat := bot.HP*100 < bot.MaxHP*30 || (sk.MaxCharge > 0 && sk.Charges == 0)
			if los && d > 1 && (d < preferred*.8 || (retreat && d < float64(sk.RangePx)+600)) {
				goal = navPoint{bot.X + (bot.X-enemy.X)/d*700, bot.Y + (bot.Y-enemy.Y)/d*700}
			} else if los && d <= preferred && sk.AttackType != "direct" {
				side := 1.0
				if (tick/60+bot.Index)%2 == 0 {
					side = -1
				}
				goal = navPoint{bot.X - (enemy.Y-bot.Y)/math.Max(d, 1)*350*side, bot.Y + (enemy.X-bot.X)/math.Max(d, 1)*350*side}
			}
			if d <= float64(sk.RangePx) && los && brain.EnemyIndex == enemy.ObjectIndex && tick-brain.EnemySeenAt >= 4 {
				if !tryBotUltimate(w, bot, enemy, d) {
					TryCast(w, bot, sk, botAim(bot, enemy, sk, brain, tick), false)
				}
			}
			if brain.EnemyIndex != enemy.ObjectIndex {
				brain.EnemyIndex = enemy.ObjectIndex
				brain.EnemySeenAt = tick
			}
			brain.EnemyPosition = navPoint{enemy.X, enemy.Y}
		} else {
			brain.EnemyIndex = -1
		}
		if w.IsKOTH && (enemy == nil || Dist(bot.X, bot.Y, enemy.X, enemy.Y) > 1200) {
			a := float64(bot.Index) * 2.4
			goal = navPoint{w.Tiles.PixW/2 + math.Cos(a)*420, w.Tiles.PixH/2 + math.Sin(a)*420}
		}
		if w.modes.karitGrab != nil {
			if g, ok := karitGrabGoal(w, bot, enemy); ok {
				goal = g
			}
		}
		if w.Ball != nil && w.RoundResetTicks == 0 {

			goal = footballGoal(w, bot, tick)
		}
		if dodge, ok := dodgeProjectile(w, bot); ok {
			goal = dodge
			brain.ReplanAt = 0
		}

		if w.PoisonedAt(bot.X, bot.Y, w.TickNum+60) || w.PoisonedAt(goal.x, goal.y, w.TickNum+60) {
			goal = navPoint{w.Tiles.PixW / 2, w.Tiles.PixH / 2}
		}
		botNavigate(w, bot, goal, tick)
	}
}

const (
	ballShotRange    = 1150.0
	ballAimError     = 700.0
	ballKeeperDepth  = 1500.0
	ballPressStandby = 300.0
)

// ballMouth — середина ворот, в которые бьёт команда team.
func ballMouth(w *World, team int) navPoint {
	if w.BallHorizontal() {
		x := w.Tiles.PixW - 900
		if team != 0 {
			x = 900
		}
		return navPoint{x, w.Tiles.PixH / 2}
	}
	y := 900.0
	if team != 0 {
		y = w.Tiles.PixH - 900
	}
	return navPoint{w.Tiles.PixW / 2, y}
}

func ballCarrier(w *World) *Player {
	for _, p := range w.Players {
		if p.Alive && !p.Abandoned && !p.IsBall() && p.CarryableObjectID == w.Ball.ObjectIndex {
			return p
		}
	}
	return nil
}

// ballDefender designates the one team member who stays home: the living
// non-carrier standing farthest from the ball, so the nearer one keeps
// contesting it. Ties break on the higher index to stay deterministic. A team
// down to a single player commits it to the ball instead of the goal.
func ballDefender(w *World, team int) *Player {
	var pick *Player
	best := -1.0
	living := 0
	for _, p := range w.Players {
		if p.Team != team || !p.Alive || p.Abandoned || p.IsBall() {
			continue
		}
		living++
		if p.CarryableObjectID == w.Ball.ObjectIndex {
			continue
		}
		d := Dist(p.X, p.Y, w.Ball.X, w.Ball.Y)
		if pick == nil || d > best || (d == best && p.Index > pick.Index) {
			pick, best = p, d
		}
	}
	if living < 2 {
		return nil
	}
	return pick
}

func keeperPost(w *World, bot *Player) navPoint {
	mouth := ballMouth(w, 1-bot.Team)
	dx, dy := w.Ball.X-mouth.x, w.Ball.Y-mouth.y
	d := math.Hypot(dx, dy)
	if d < 1 {
		return mouth
	}
	out := math.Min(ballKeeperDepth, d*.6)
	return navPoint{mouth.x + dx/d*out, mouth.y + dy/d*out}
}

func footballGoal(w *World, bot *Player, tick int) navPoint {
	attack := ballMouth(w, bot.Team)
	if bot.CarryableObjectID == w.Ball.ObjectIndex {
		shootBall(w, bot, attack, tick)
		return attack
	}
	if d := ballDefender(w, bot.Team); d == bot {
		return keeperPost(w, bot)
	}
	carrier := ballCarrier(w)
	if carrier == nil {
		return navPoint{w.Ball.X, w.Ball.Y}
	}
	if carrier.Team == bot.Team {

		return navPoint{(carrier.X + attack.x) / 2, (carrier.Y + attack.y) / 2}
	}

	target := ballMouth(w, carrier.Team)
	dx, dy := target.x-carrier.X, target.y-carrier.Y
	d := math.Hypot(dx, dy)
	if d < 1 {
		return navPoint{carrier.X, carrier.Y}
	}
	step := math.Min(ballPressStandby, d*.5)
	return navPoint{carrier.X + dx/d*step, carrier.Y + dy/d*step}
}

func shootBall(w *World, bot *Player, target navPoint, tick int) {
	if Dist(bot.X, bot.Y, target.x, target.y) > ballShotRange {
		return
	}
	if !shotClear(w, bot.X, bot.Y, target.x, target.y, &Skill{}) {
		return
	}
	// Разброс — поперёк створа.
	miss := math.Sin(float64(tick+bot.Index*23)*.41) * ballAimError
	x, y := target.x+miss, target.y
	if w.BallHorizontal() {
		x, y = target.x, target.y+miss
	}
	ThrowBall(w, bot, protocol.PlayerAction{X: int(x - bot.X), Y: int(y - bot.Y)}, false)
}

func nearestEnemy(w *World, bot *Player) *Player {
	var best *Player
	bestD := -1.0
	for _, c := range w.Players {
		if c == bot || !c.Alive || c.Abandoned || c.Team == bot.Team || c.IsBall() || c.Visibility == 0 || !w.CanSee(bot, c) {
			continue
		}
		d := Dist(bot.X, bot.Y, c.X, c.Y)
		if len(bot.Skills) > 0 && !shotClear(w, bot.X, bot.Y, c.X, c.Y, bot.Skills[0]) {
			d += 800
		}
		if bot.Brain != nil && bot.Brain.EnemyIndex == c.ObjectIndex {
			d -= 250
		}
		if best == nil || d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func botAim(bot, enemy *Player, sk *Skill, b *BotBrain, tick int) protocol.PlayerAction {
	x, y := enemy.X, enemy.Y
	if sk.ProjSpeed > 0 && sk.AttackType != "direct" {
		flight := math.Min(.6, Dist(bot.X, bot.Y, x, y)/float64(sk.ProjSpeed))
		vx, vy := (x-b.EnemyPosition.x)*5, (y-b.EnemyPosition.y)*5
		v := math.Hypot(vx, vy)
		if v > DisplayedSpeed(enemy) {
			vx *= DisplayedSpeed(enemy) / v
			vy *= DisplayedSpeed(enemy) / v
		}
		x += vx * flight
		y += vy * flight
	}
	e := math.Sin(float64(tick+bot.Index*17)*.37) * 45
	x += e
	y -= e
	if sk.ProjIndirect || sk.ChargeTypeName == "jump" {
		return protocol.PlayerAction{X: int(x), Y: int(y)}
	}
	return protocol.PlayerAction{X: int(x - bot.X), Y: int(y - bot.Y)}
}

func tryBotUltimate(w *World, bot, enemy *Player, d float64) bool {
	if bot.UltCharge < 4000 || len(bot.Skills) < 2 {
		return false
	}
	sk := bot.Skills[1]
	use := false
	switch sk.Behaviour {
	case "shield", "area_buff_teammates":
		use = bot.HP*100 < bot.MaxHP*65 && d < 1800
	case "attack", "whirlwind", "trail":
		use = d <= float64(sk.RangePx) && shotClear(w, bot.X, bot.Y, enemy.X, enemy.Y, sk)
	case "charge":
		use = sk.ChargeTypeName != "attach" && d > 400 && d <= float64(sk.RangePx)
	case "teleport":
		use = bot.HP*100 < bot.MaxHP*30
	}
	if !use {
		return false
	}
	a := protocol.PlayerAction{X: int(enemy.X - bot.X), Y: int(enemy.Y - bot.Y)}
	if sk.ProjIndirect || sk.ChargeTypeName == "jump" {
		a.X = int(enemy.X)
		a.Y = int(enemy.Y)
	}
	if sk.Behaviour == "teleport" {
		a.X = -a.X
		a.Y = -a.Y
	}
	return TryCast(w, bot, sk, a, true)
}

func dodgeProjectile(w *World, bot *Player) (navPoint, bool) {
	for _, p := range w.Projectiles {
		if !p.Alive || p.Team == bot.Team || Dist(bot.X, bot.Y, p.X, p.Y) > 900 {
			continue
		}
		a := float64(p.Angle) * math.Pi / 180
		vx, vy := math.Cos(a), math.Sin(a)
		dx, dy := bot.X-p.X, bot.Y-p.Y
		along := dx*vx + dy*vy
		if along < 0 || along > p.SpeedTick*8 || math.Abs(dx*vy-dy*vx) > float64(bot.CollisionRadius+p.Radius+80) {
			continue
		}
		side := 1.0
		if dx*(-vy)+dy*vx < 0 {
			side = -1
		}
		for _, sign := range []float64{side, -side} {
			goal := navPoint{bot.X - vy*450*sign, bot.Y + vx*450*sign}
			if walkClear(&w.Tiles, navPoint{bot.X, bot.Y}, goal, bot.CollisionRadius) {
				return goal, true
			}
		}
	}
	return navPoint{}, false
}
