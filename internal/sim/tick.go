package sim

import "math"

func (p *Player) ApplyWalk(x, y int) {
	if !p.Alive || p.Abandoned || p.BaseSpeed == 0 || p.ControlRemoved {
		return
	}
	p.TargetX = float64(clampInput(x))
	p.TargetY = float64(clampInput(y))
	p.HasMoveTarget = true
	p.WalkPending = false
	if x != 0 || y != 0 {
		if p.State == StateIdle {
			p.State = StateWalk
		}
		ang := int(math.Atan2(float64(y)-p.Y, float64(x)-p.X) * 180 / math.Pi)
		p.MoveAngle = NormalizeAngle(ang)
		p.AttackAngle = p.MoveAngle
	}
}

func (w *World) Advance() {
	if w.Finished {
		w.TickNum++
		return
	}
	if int(w.TickNum) < w.IntroTicks {
		w.TickNum++
		return
	}
	if w.Ball != nil && w.RoundResetTicks > 0 {
		BallTick(w)
		w.TickNum++
		return
	}
	TickEffects(w)
	TickAugments(w)
	TickMinions(w)

	BotStep(w, int(w.TickNum))

	for _, p := range w.Characters() {
		if !p.Alive || !p.HasMoveTarget || p.Abandoned || p.IsBall() {
			continue
		}
		if p.ControlRemoved || p.Charged || (len(p.Executions) > 0 && !p.Executions[0].Skill.Definition.Bool("can_move_at_same_time", true)) {
			continue
		}
		dx, dy := p.TargetX-p.X, p.TargetY-p.Y
		d := math.Hypot(dx, dy)
		if d < 1 {
			p.HasMoveTarget = false
			if p.State == StateWalk {
				p.State = StateIdle
			}
			continue
		}
		travel := math.Min(d, DisplayedSpeed(p)/20)
		movePlayer(w, p, dx/d*travel, dy/d*travel)
		p.MoveAngle = NormalizeAngle(int(math.Atan2(dy, dx) * 180 / math.Pi))
		if len(p.Executions) > 0 {
			p.State = StateWalkAttack
		} else {
			p.State = StateWalk
		}
	}

	for _, p := range w.Characters() {
		if !p.Alive || p.Abandoned || p.IsBall() {
			continue
		}
		if p.SpawnShield {
			p.SpawnShieldTicks--
			if p.SpawnShieldTicks <= 0 {
				p.SpawnShield = false
			}
		}
		if p.ShieldBuffTicks > 0 {
			p.ShieldBuffTicks--
			if p.ShieldBuffTicks == 0 {
				p.ShieldBuff = false
			}
		}
		if p.ConsumableTicks > 0 {
			p.ConsumableTicks--
			if p.ConsumableTicks == 0 {
				p.ConsumableShield = 0
			}
		}
		for _, sk := range p.Skills {
			if sk.CoolLeft > 0 {
				sk.CoolLeft -= TickMs
				if sk.CoolLeft < 0 {
					sk.CoolLeft = 0
				}
			}
			if sk.ActiveLeft > 0 {
				sk.ActiveLeft -= TickMs
				if sk.ActiveLeft < 0 {
					sk.ActiveLeft = 0
				}
			}
			if sk.MaxCharge >= 1 && sk.Charges < sk.MaxCharge {
				sk.RechargeLeft -= TickMs * max(1, 100+p.ReloadAddPct) / 100
				if sk.RechargeLeft <= 0 {
					sk.Charges++
					if sk.Charges < sk.MaxCharge {
						sk.RechargeLeft = sk.RechargeMs
					} else {
						sk.RechargeLeft = 0
					}
				}
			}
		}
		if p.TeleportLeft > 0 {
			p.TeleportLeft--
			if p.TeleportLeft == 0 {
				p.Teleporting = false
			}
		}
		if p.ChargeAuto > 0 && p.UltCharge < 4000 {
			p.UltCharge += p.ChargeAuto
			if p.UltCharge > 4000 {
				p.UltCharge = 4000
			}
		}
		if p.DashCooldown > 0 {
			p.DashCooldown--
		}
		if p.DashActiveTicks > 0 {
			p.DashActiveTicks--
			p.ActingTicks = min(255, p.ActingTicks+1)
			if p.DashActiveTicks == 0 {
				p.AccessoryState = 0
				p.ScentSources = nil
			}
		}
		if p.ConsecutiveTicks > 0 {
			p.ConsecutiveTicks--
			if p.ConsecutiveTicks == 0 && p.ConsecutiveLeft > 0 {
				p.ConsecutiveLeft = 0
				p.UltCharge = 0
			}
		}
		TickExecutions(w, p)
	}
	TickProjectiles(w)

	for _, p := range w.Players {
		if p.Alive || p.Abandoned || p.IsBall() {
			continue
		}
		if w.IsShowdown {

			ok := false
			for _, t := range w.Players {
				if t != p && t.Team == p.Team && t.Alive && !t.Abandoned && !t.IsBall() {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if p.RespawnLeft > 0 {
			p.RespawnLeft--
		}
		if p.RespawnLeft <= 0 && w.RespawnTicks >= 0 {
			p.Alive = true
			p.HP = p.MaxHP
			p.State = StateRespawn
			p.SpawnShield = true
			p.SpawnShieldTicks = 40
			nx, ny := w.Tiles.FindOpen(p.SpawnX, p.SpawnY, p.CollisionRadius)
			p.X, p.Y, p.ExactX, p.ExactY = nx, ny, nx, ny
			p.HasMoveTarget = false
			if w.IsBounty {
				p.GamePoints = 2
			}
		}
	}

	w.ModeTick()
	w.TickDPS()
	w.TickNum++
	if w.Finished {
		return
	}
	if !w.Endless && int(w.TickNum) >= w.TotalTicks() {
		if w.ModeName == "mega_boss" {
			w.Finished = true
			w.WinnerTeam = 1
			w.WinReason = "boss_survived"
			return
		}
		w.FinishByTime()
		return
	}
	if w.IsShowdown && !w.IsArenaLobby {
		alive := map[int]bool{}
		for _, p := range w.Players {
			if p.Alive && !p.Abandoned && !p.IsBall() {
				alive[p.Team] = true
			}
		}
		if len(alive) <= 1 {
			for t := range alive {
				w.WinnerTeam = t
			}
			w.Finished = true
			w.WinReason = "last_team"
		}
		return
	}
	if w.IsBounty {
		for t, pts := range w.TeamPoints {
			if pts >= 10 {
				w.WinnerTeam = t
				w.Finished = true
				w.WinReason = "points"
				return
			}
		}
	}
}

func (w *World) ModeTick() {
	w.TickPoison()
	if w.IsKOTH && w.TickNum%20 == 0 {
		cx, cy := w.Tiles.PixW/2, w.Tiles.PixH/2
		teams := map[int]bool{}
		for _, p := range w.Players {
			if !p.Alive || p.Abandoned || p.IsBall() {
				continue
			}
			dx, dy := p.X-cx, p.Y-cy
			if dx*dx+dy*dy <= 1000000 {
				teams[p.Team] = true
			}
		}
		if len(teams) == 1 {
			for t := range teams {
				w.AddPoint(t, 1)
			}
		}
	}
	if w.Ball != nil {
		BallTick(w)
	}
}

func (w *World) FinishByTime() {
	if w.Ball != nil && !w.BallOvertime && w.GetPoint(0) == w.GetPoint(1) {
		w.BallOvertime = true
		w.DurationTicks += 1200
		for i := range w.Tiles.Destroyed {
			if w.Tiles.Destructible[i] {
				w.Tiles.Destroyed[i] = true
			}
		}
		return
	}
	w.Finished = true
	w.WinReason = "time"
	best, bestPts, draw := -1, -1, false
	for t, pts := range w.TeamPoints {
		if pts > bestPts {
			bestPts, best, draw = pts, t, false
		} else if pts == bestPts {
			draw = true
		}
	}
	if draw {
		w.WinnerTeam = -1
	} else {
		w.WinnerTeam = best
	}
}
