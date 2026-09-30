package sim

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"math"
)

func TryCast(w *World, p *Player, s *Skill, a protocol.PlayerAction, ultimate bool) bool {
	if handled, ok := modeCast(w, p, s, a, ultimate); handled {
		return ok
	}
	if ultimate && p.augment("back_to_basics").Exists() {
		return false
	}
	if ultimate && p.UltCharge < 4000 && p.ConsecutiveLeft <= 0 {
		return false
	}
	if !castSkill(w, p, s, a, ultimate, false, false) {
		return false
	}
	if ultimate {
		p.UltimateUses++
		if p.ConsecutiveLeft > 0 {
			p.ConsecutiveLeft--
		} else {
			p.ConsecutiveLeft = max(0, s.Definition.At("behaviour").At("charge").At("unique_property").Int("max_consecutive_usages", 1)-1)
		}
		p.UltCharge = 0
		if p.ConsecutiveLeft > 0 {
			p.UltCharge = 4000
		}
		p.ConsecutiveTicks = 100
		p.UltUsed = true
		p.UltActive = false
		onAugmentUltimate(w, p)
	}
	return true
}

func CanAct(w *World, p *Player) bool {
	if p == nil || !p.Alive || p.Abandoned || p.ControlRemoved {
		return false
	}
	for _, e := range p.Effects {
		if e.Kind == "stun" && e.Expires > w.TickNum {
			return false
		}
	}
	return true
}

func castSkill(w *World, p *Player, s *Skill, a protocol.PlayerAction, ultimate, dash, chained bool) bool {
	if s == nil || !CanAct(w, p) || w.Finished || int(w.TickNum) < w.IntroTicks {
		return false
	}
	if !chained {
		if s.CoolLeft > 0 || s.ActiveLeft > 0 || s.CastPending {
			return false
		}
		for _, e := range p.Executions {
			if !e.Completed {
				return false
			}
		}
		if !ultimate && s.MaxCharge > 0 && s.Charges < 1 {
			return false
		}
	}
	d := s.Definition
	jump := s.ChargeTypeName == "jump" || s.ChargeTypeName == "jump_destroying"
	absolute := !dash && (s.ProjIndirect || jump)
	// Рывки-перемещения приходят смещением, но летят на его длину, а не на максимум.
	aimed := dash && (s.Behaviour == "teleport" || s.Behaviour == "portal" || jump)
	lead := 0.0
	if s.AttackType == "projectile" && s.Projectile != "" && !s.Homing {
		lead = float64(s.ProjSpeed)
	}
	tx, ty := aimTarget(w, p, a, s.RangePx, targetAim{absolute: absolute, forceMax: d.Bool("always_cast_at_max_range"), aimed: aimed, leadSpeed: lead})
	if absolute || aimed {
		tx = clampF(tx, 0, w.Tiles.PixW)
		ty = clampF(ty, 0, w.Tiles.PixH)
	}
	if s.ChargeTypeName == "attach" {
		var best *Player
		distance := math.MaxFloat64
		for _, t := range w.Players {
			if t != p && t.Alive && t.Team == p.Team && !t.Abandoned {
				if v := Dist(p.X, p.Y, t.X, t.Y); v < distance {
					best = t
					distance = v
				}
			}
		}
		if best == nil {
			return false
		}
		tx, ty = best.X, best.Y
	}
	if !chained && !ultimate && s.MaxCharge > 0 {
		s.Charges--
		s.RechargeLeft = s.RechargeMs
	}
	duration := max(50, s.ActiveMs)
	immediate := d.Bool("execute_first_attack_immediately", !d.Exists())
	if s.Behaviour == "attack" && !immediate {
		duration = max(duration, s.MsBetween)
	}
	if s.Behaviour == "charge" {
		duration = max(duration, int(math.Ceil(Dist(p.X, p.Y, tx, ty)*1000/float64(max(1, s.ChargeSpeed)))))
	}
	delay := max(0, d.Int("casting_time"))
	pulse := max(50, s.MsBetween)
	if immediate {
		pulse = 0
	}
	w.NextExecID++
	e := &Execution{ID: w.NextExecID, Skill: s, Ultimate: ultimate, Duration: duration, DurationLeft: duration, DelayLeft: delay, TargetX: tx, TargetY: ty, StartX: p.X, StartY: p.Y, Angle: angleTo(tx-p.X, ty-p.Y), NextPulseLeft: pulse}
	if !ultimate && !dash && !chained {
		e.DamagePct = augmentWeaponCast(p)
	}
	// Числа телевизора — урон ульты Кролика: ульту заряжают слабее.
	if isRabbitTV(p) {
		e.Ultimate = true
	}
	s.CoolLeft = max(0, s.CoolMs)
	s.CastPending = delay > 0
	s.ActiveLeft = 0
	p.SkillCastLeft = delay
	p.Executions = append(p.Executions, e)
	p.RevealedUntil = w.TickNum + 40
	if !dash {
		p.LastCombatTick = w.TickNum
	}
	p.AttackAngle = e.Angle
	p.State = StateAttack
	if !d.Bool("can_move_at_same_time", true) {
		p.HasMoveTarget = false
	}
	if delay == 0 {
		beginExecution(w, p, e)
	}
	return true
}

func beginExecution(w *World, p *Player, e *Execution) {
	e.Started = true
	e.Skill.CastPending = false
	e.Skill.ActiveLeft = e.Duration
	b := e.Skill.Definition.At("behaviour")
	kind := e.Skill.Behaviour
	switch kind {
	case "charge":
		p.Charged = true
		p.ChargeMsLeft = e.Duration
		p.ChargeTotalMs = e.Duration
		p.ChargeSpeed = min(16383, max(0, e.Skill.ChargeSpeed))
		p.ChargeType = map[string]int{"movement": 1, "jump": 2, "jump_destroying": 3, "dash": 5, "fast_movement": 12, "attach": 14, "fly": 15}[e.Skill.ChargeTypeName]
		if p.ChargeType == 0 {
			p.ChargeType = 1
		}
	case "teleport":
		SpawnArea(w, p, b.String("start_area_effect_name"), p.X, p.Y, 0, 0, false, e.Ultimate)
		teleportPlayer(w, p, e.TargetX, e.TargetY)
		SpawnArea(w, p, b.String("end_area_effect_name"), p.X, p.Y, 0, 0, false, e.Ultimate)
		e.Completed = true
	case "portal":
		SpawnPortal(w, p, b.String("spawned_item_name"), e.TargetX, e.TargetY)
		e.Completed = true
	case "area_buff_teammates":
		a := SpawnArea(w, p, b.String("area_effect_name"), p.X, p.Y, b.Int("damage"), 0, true, e.Ultimate)
		radius := 600
		if a != nil {
			radius = a.Definition.Int("radius", 600)
		}
		for _, t := range w.Characters() {
			if t.Alive && t.Team == p.Team && Dist(p.X, p.Y, t.X, t.Y) <= float64(radius) {
				giveShieldFrom(t, p, b.Int("consumable_shield"), b.Int("consumable_shield_ticks", 100))
				AddStatus(w, t, "damage", b.Int("rage_value"), b.Int("consumable_shield_ticks", 100), p.ObjectIndex)
			}
		}
		e.Completed = true
	case "shield":
		p.ShieldBuff = true
	case "fly":
		p.Charged = true
		p.ChargeType = 15
		p.ChargeSpeed = p.BaseSpeed
		p.ChargeMsLeft = e.Duration
		p.ChargeTotalMs = e.Duration
		p.Z = 600
	case "whirlwind":
		SpawnArea(w, p, b.String("area_effect_name"), p.X, p.Y, 0, 0, true, e.Ultimate)
	}
	if e.NextPulseLeft == 0 && (kind == "attack" || kind == "shield" || kind == "trail" || kind == "whirlwind") {
		FirePulse(w, p, e)
		e.NextPulseLeft = max(50, e.Skill.MsBetween)
	}
}

func TickExecutions(w *World, p *Player) {
	if !p.Alive {
		CancelCast(p)
		return
	}

	current := append([]*Execution(nil), p.Executions...)
	blocked := !CanAct(w, p)
	for _, e := range current {

		if blocked && !e.Completed {
			e.Completed = true
			e.Cancelled = true
			e.Skill.CastPending = false
			e.Skill.ActiveLeft = 0
			p.SkillCastLeft = 0
			finishExecution(w, p, e)
			continue
		}
		if !e.Started {
			e.DelayLeft -= TickMs
			e.Skill.CastPending = e.DelayLeft > 0
			p.SkillCastLeft = max(0, e.DelayLeft)
			if e.DelayLeft <= 0 {
				beginExecution(w, p, e)
			}
			continue
		}
		if !e.Completed {
			e.Elapsed += TickMs
			e.DurationLeft = max(0, e.Duration-e.Elapsed)
			switch e.Skill.Behaviour {
			case "charge":
				tickCharge(w, p, e)
			case "fly":
				if p.HasMoveTarget {
					dx, dy := p.TargetX-p.X, p.TargetY-p.Y
					d := math.Hypot(dx, dy)
					step := math.Min(d, DisplayedSpeed(p)/20)
					moveAirborne(w, p, dx/math.Max(1, d)*step, dy/math.Max(1, d)*step)
				}
				p.ChargeMsLeft = e.DurationLeft
			case "attack", "shield", "trail", "whirlwind":
				if e.Elapsed >= e.NextPulseLeft && e.Elapsed <= e.Duration {
					FirePulse(w, p, e)
					e.NextPulseLeft += max(50, e.Skill.MsBetween)
				}
			}
			if e.Elapsed >= e.Duration {
				e.Completed = true
			}
		}
		if e.Completed {
			finishExecution(w, p, e)
		} else {
			e.Skill.ActiveLeft = e.DurationLeft
		}
	}
	kept := p.Executions[:0]
	for _, e := range p.Executions {
		if !e.Completed {
			kept = append(kept, e)
		}
	}
	clear(p.Executions[len(kept):])
	p.Executions = kept
	if len(kept) == 0 {
		p.Charged = false
		p.ChargeMsLeft = 0
		p.Z = 0
		p.ShieldBuff = false
		if p.HasMoveTarget {
			p.State = StateWalk
		} else {
			p.State = StateIdle
		}
	}
}

func tickCharge(w *World, p *Player, e *Execution) {
	c := e.Skill.Definition.At("behaviour").At("charge")
	kind := e.Skill.ChargeTypeName
	dx, dy := e.TargetX-p.X, e.TargetY-p.Y
	d := math.Hypot(dx, dy)
	step := math.Min(d, float64(max(1, e.Skill.ChargeSpeed))/20)
	air := kind == "jump" || kind == "jump_destroying" || kind == "fly"
	if air {
		moveAirborne(w, p, dx/math.Max(1, d)*step, dy/math.Max(1, d)*step)
		p.Z = 600 * math.Sin(math.Pi*clampF(float64(e.Elapsed)/float64(e.Duration), 0, 1))
	} else {
		movePlayer(w, p, dx/math.Max(1, d)*step, dy/math.Max(1, d)*step)
	}
	p.ChargeMsLeft = e.DurationLeft
	if kind == "fly" && e.Elapsed%1000 == 0 {
		Heal(p, c.Int("healing_per_second"))
	}
	if kind == "attach" {
		for _, t := range w.Characters() {
			if t == p || !t.Alive || t.Team != p.Team || t.IsBall() || Dist(p.X, p.Y, t.X, t.Y) > float64(p.CollisionRadius+t.CollisionRadius) {
				continue
			}
			t.SegmentPercent = c.At("segment_shield").Int("percent")
			t.SegmentTicks = c.At("segment_shield").Int("ticks")
			t.SegmentLevel = 1
			for _, v := range w.Characters() {
				if v.Alive && v.Team != p.Team && Dist(t.X, t.Y, v.X, v.Y) <= 300 {
					applyDamage(w, p, v, c.Int("damage"), e.Ultimate)
				}
			}
			p.X, p.Y = t.X, t.Y
			p.Alive = false
			p.HP = 0
			e.Completed = true
			if p.Upgrade.String("name") == "override_minion_death_area_effect" {
				SpawnArea(w, t, p.Upgrade.String("area_effect_name"), t.X, t.Y, 0, 0, false, e.Ultimate)
			}
			break
		}
	} else if !air && c.Int("charge_pushback") != 0 {
		for _, t := range w.Characters() {
			if t.Alive && t.Team != p.Team && Dist(p.X, p.Y, t.X, t.Y) < float64(p.CollisionRadius+t.CollisionRadius) {
				rad := float64(e.Angle) * math.Pi / 180
				movePlayer(w, t, math.Cos(rad)*float64(c.Int("charge_pushback")), math.Sin(rad)*float64(c.Int("charge_pushback")))
			}
		}
	}
}

func FirePulse(w *World, p *Player, e *Execution) {
	s := e.Skill
	b := s.Definition.At("behaviour")
	kind := s.Behaviour
	a := b
	if kind == "attack" {
		a = b.At("attack")
	}
	damage := a.Int("damage", s.Damage) * (100 + e.DamagePct) / 100
	if kind == "attack" && s.AttackType == "dummy" {
		return
	}
	if kind == "trail" || kind == "whirlwind" || (kind == "attack" && s.AttackType == "area_effect") {
		x, y := p.X, p.Y
		if kind == "attack" {
			x, y = e.TargetX, e.TargetY
		}
		if kind == "whirlwind" {
			for _, v := range w.Characters() {
				if v.Alive && v.Team != p.Team && Dist(p.X, p.Y, v.X, v.Y) <= float64(s.RangePx) {
					applyDamage(w, p, v, damage, e.Ultimate)
				}
			}
		} else {
			SpawnArea(w, p, a.String("area_effect_name"), x, y, damage, 0, false, e.Ultimate)
		}
		if kind == "trail" && p.Upgrade.String("name") == "trail_minion_spawn" && e.Elapsed%max(50, p.Upgrade.Int("ticks_between_spawn", 200)*50) == 0 {
			names := p.Upgrade.At("area_effect_names").List()
			if len(names) > 0 && e.SpawnCount < p.Upgrade.Int("max_spawn_count", 3) {
				SpawnArea(w, p, names[e.SpawnCount%len(names)].Text(), p.X, p.Y, 0, 0, false, e.Ultimate)
				e.SpawnCount++
			}
		}
		return
	}
	if kind == "attack" && (s.AttackType == "direct" || s.Projectile == "") {
		half := math.Min(180, float64(a.Int("spread", 90))/2)
		for _, v := range w.Characters() {
			if v != p && v.Alive && v.Team != p.Team && !v.IsBall() && bodyDist(v, p.X, p.Y) <= float64(s.RangePx) {
				delta := abs((angleTo(v.X-p.X, v.Y-p.Y)-e.Angle+540)%360 - 180)
				if float64(delta) <= half {
					applyDamage(w, p, v, damage, e.Ultimate)
				}
			}
		}
		return
	}
	fireProjectiles(w, p, e, s)
}

func finishExecution(w *World, p *Player, e *Execution) {
	e.Skill.ActiveLeft = 0
	e.Skill.CastPending = false
	p.SkillCastLeft = 0
	if e.Cancelled || !p.Alive {
		return
	}
	b := e.Skill.Definition.At("behaviour")
	c := b.At("charge")
	if e.Skill.Behaviour == "charge" {
		p.Charged = false
		p.Z = 0
		if e.Skill.ChargeTypeName == "jump_destroying" {
			for x := int(p.X)/300 - 1; x <= int(p.X)/300+1; x++ {
				for y := int(p.Y)/300 - 1; y <= int(p.Y)/300+1; y++ {
					w.Tiles.DestroyTile(x, y)
				}
			}
		}

		x, y := w.Tiles.FindOpen(p.X, p.Y, p.CollisionRadius)
		p.X, p.Y, p.ExactX, p.ExactY = x, y, x, y
		SpawnArea(w, p, c.String("landing_area_effect_name"), p.X, p.Y, 0, 0, false, e.Ultimate)
		if e.Skill.ChargeTypeName == "jump" {
			if s := skillNamed(p, c.String("after_charge_skill_name")); s != nil {
				castSkill(w, p, s, protocol.PlayerAction{X: int(e.TargetX - p.X), Y: int(e.TargetY - p.Y)}, e.Ultimate, false, true)
			}
		}
		if e.Ultimate && p.Upgrade.String("name") == "spawn_projectiles_after_consecutive_skill" && p.ConsecutiveLeft == 0 && w.Catalog != nil {
			d := w.Catalog.Spec("projectiles", p.Upgrade.String("projectile_name"))
			for i := 0; i < min(32, p.Upgrade.Int("count")); i++ {
				spawnProjectile(w, p, d, e.Angle+(i-1)*15, p.Upgrade.Int("damage"), p.Upgrade.Int("casting_range")*100, e.TargetX, e.TargetY, true, e.ID)
			}
		}
	}
	if e.Ultimate && e.Skill.Behaviour != "charge" && p.Upgrade.String("name") == "chained_skill" {
		if s := skillNamed(p, p.Upgrade.String("skill_name")); s != nil && s != e.Skill {
			castSkill(w, p, s, protocol.PlayerAction{X: int(e.TargetX - p.X), Y: int(e.TargetY - p.Y)}, false, false, true)
		}
	}
}
func skillNamed(p *Player, name string) *Skill {
	for _, s := range p.Skills {
		if s.Name == name {
			return s
		}
	}
	return nil
}
func CancelCast(p *Player) {
	p.Executions = nil
	p.Charged = false
	p.ShieldBuff = false
	p.ChargeMsLeft = 0
	p.SkillCastLeft = 0
	p.Z = 0
	for _, s := range p.Skills {
		s.CastPending = false
		s.ActiveLeft = 0
	}
}
func angleTo(x, y float64) int { return NormalizeAngle(int(math.Atan2(y, x) * 180 / math.Pi)) }
func moveAirborne(w *World, p *Player, dx, dy float64) {
	p.X = clampF(p.X+dx, float64(p.CollisionRadius), w.Tiles.PixW-float64(p.CollisionRadius))
	p.Y = clampF(p.Y+dy, float64(p.CollisionRadius), w.Tiles.PixH-float64(p.CollisionRadius))
	p.ExactX, p.ExactY = p.X, p.Y
}

func TryDash(w *World, p *Player, a protocol.PlayerAction) bool {
	d := p.DashDefinition
	t := d.At("type")
	if !CanAct(w, p) || w.Finished || int(w.TickNum) < w.IntroTicks || !d.Exists() || p.DashCooldown > 0 || (!p.DashUnlimited && p.DashChargeLeft <= 0) || !modeDashAllowed(w, p) {
		return false
	}

	if !d.Bool("interrupts_actions") && len(p.Executions) > 0 {
		return false
	}
	// «Ускорение» Кролика переключает передачу — без телевизора жать нечего.
	if d.Bool("requires_rabbit_tv") && rabbitTV(w, p) == nil {
		return false
	}
	if t.String("name") == "dash_low_hit_points" {
		p.ScentSources = nil
		for _, v := range w.Players {
			if v.Alive && v.Team != p.Team && v.HP*100 < v.MaxHP*t.Int("target_lower_than", 50) && Dist(p.X, p.Y, v.X, v.Y) <= float64(d.Int("casting_range", 30)*300) {
				p.ScentSources = append(p.ScentSources, [2]int{int(v.X), int(v.Y)})
			}
		}
		if d.Bool("require_enemy_in_range") && len(p.ScentSources) == 0 {
			return false
		}
	}
	if d.Bool("interrupts_actions") {
		CancelCast(p)
	}
	switch t.String("name") {
	case "skill":
		s := skillNamed(p, t.String("skill_name"))
		if !castSkill(w, p, s, a, false, true, false) {
			return false
		}
		if d.Bool("requires_rabbit_tv") {
			rabbitSwitchChannel(w, p)
		}
	case "teleport":
		x, y := ComputeTarget(w, p, a, max(100, d.Int("casting_range")*300), false)
		SpawnArea(w, p, t.String("start_area_effect_name"), p.X, p.Y, 0, 0, false, false)
		teleportPlayer(w, p, x, y)
		SpawnArea(w, p, t.String("end_area_effect_name"), p.X, p.Y, 0, 0, false, false)
	case "dash_low_hit_points":
		AddStatus(w, p, "speed", t.Int("speed_change_percent"), t.Int("speed_change_ticks", 40), p.ObjectIndex)
	case "attack_override":
	default:
		return false
	}
	p.AccessoryState = 1
	p.DashUses++
	p.ActingTicks = 0
	p.DashActiveTicks = max(1, d.Int("active_ticks", 10))
	p.DashCooldown = min(255, max(0, d.Int("cool_down_ticks", 20)))
	if !p.DashUnlimited {
		p.DashChargeLeft--
	}
	if d.Bool("stop_movement") {
		p.HasMoveTarget = false
	}
	return true
}
