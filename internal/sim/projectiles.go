package sim

import (
	"github.com/freaks/openfreaks/internal/content"
	"math"
	"sort"
)

func spawnProjectile(w *World, p *Player, d content.Spec, angle, damage, rng int, tx, ty float64, ultimate bool, execution int) *Projectile {
	if !d.Exists() || len(w.Projectiles) >= 512 {
		return nil
	}
	for {
		w.NextProjIndex++
		if w.NextProjIndex > 60000 || w.NextProjIndex < 1000 {
			w.NextProjIndex = 1000
		}
		exists := false
		for _, v := range w.Projectiles {
			if v.LocalIndex == w.NextProjIndex {
				exists = true
				break
			}
		}
		if !exists {
			break
		}
	}
	pr := &Projectile{Definition: d, LocalIndex: w.NextProjIndex, ModelID: int32(d.ID), Owner: p, Team: p.Team, RosterIndex: p.Index,
		X: p.X, Y: p.Y, Z: p.SpawnHeight, SpawnX: p.X, SpawnY: p.Y, TargetX: tx, TargetY: ty, Angle: NormalizeAngle(angle), Damage: damage, RangePx: float64(max(1, rng)), SpeedTick: float64(max(1, d.Int("speed", 1600))) / 20, Radius: d.Int("radius"),
		PierceChars: d.Bool("pierces_characters"), PierceEnv: d.Bool("pierces_environment"), PassEnv: d.Bool("passes_environment"), Homing: d.Bool("is_homing_missile"), Bouncing: d.Bool("is_bouncing"), Circling: d.Int("travel_type") == 8, EarlyTicks: d.Int("early_ticks"), Ultimate: ultimate, ExecutionID: execution, Alive: true, HitSet: map[*Player]bool{}}
	w.Projectiles = append(w.Projectiles, pr)
	return pr
}

func fireProjectiles(w *World, p *Player, e *Execution, s *Skill) {
	b := s.Definition.At("behaviour")
	a := b
	if s.Behaviour == "attack" {
		a = b.At("attack")
	}
	name := a.String("projectile_name", s.Projectile)
	if p.AccessoryState == 1 && p.DashDefinition.At("type").String("name") == "attack_override" {
		name = p.DashDefinition.At("type").String("projectile_name")
	}
	var d content.Spec
	if w.Catalog != nil {
		d = w.Catalog.Spec("projectiles", name)
	} else {
		d = content.Spec{ID: s.ProjectileID, Value: map[string]any{"speed": s.ProjSpeed, "radius": s.ProjRadius, "pierces_characters": s.ProjPierceChars, "pierces_environment": s.ProjPierceEnv, "passes_environment": s.ProjPassEnv, "is_homing_missile": s.Homing, "is_indirect": s.ProjIndirect}}
	}
	bullets := min(32, max(1, a.Int("num_bullets_in_one_attack", s.NumBullets)))
	spread := a.Int("spread", s.Spread)
	var shared map[*Player]bool
	if d.Bool("damage_only_with_one_projectile") {
		shared = map[*Player]bool{}
	}
	for i := 0; i < bullets; i++ {
		angle := float64(e.Angle)
		if s.Behaviour == "shield" {
			angle += float64(e.Elapsed) / float64(max(1, e.Duration)) * float64(min(360, spread))
		} else if bullets > 1 {
			angle += -float64(spread)/2 + float64(spread*i)/float64(bullets-1)
		}
		if pr := spawnProjectile(w, p, d, int(angle), a.Int("damage", s.Damage)*(100+e.DamagePct)/100, max(100, s.RangePx), e.TargetX, e.TargetY, e.Ultimate, e.ID); pr != nil {
			pr.SharedHits = shared
			pr.SummonName = a.At("summon").String("summoned_character_name")
			pr.SummonBehavior = a.At("summon").String("spawn_behavior")
		}
	}
}

// Sweep both walls and character circles along the travelled segment. Resolve
// hits in order, stopping at the first wall so a victim behind it is not hit.
func TickProjectiles(w *World) {
	current := append([]*Projectile(nil), w.Projectiles...)
	for _, pr := range current {
		if !pr.Alive {
			pr.TerminalTicks--
			continue
		}
		pr.AgeTicks++
		d := pr.Definition
		pr.EarlyTicks = max(0, d.Int("early_ticks")-pr.AgeTicks+1)
		if pr.Returning && pr.Owner != nil && pr.Owner.Alive {
			pr.Angle = angleTo(pr.Owner.X-pr.X, pr.Owner.Y-pr.Y)
		} else if pr.Homing || d.Int("travel_type") == 10 {
			var target *Player
			best := math.MaxFloat64
			for _, v := range w.Characters() {
				if v.Alive && !v.Abandoned && !v.IsBall() && v.Team != pr.Team && !pr.HitSet[v] {
					if dist := Dist(pr.X, pr.Y, v.X, v.Y); dist < best {
						target = v
						best = dist
					}
				}
			}
			if target != nil && pr.AgeTicks > d.Int("steer_ignore_ticks") {
				want := angleTo(target.X-pr.X, target.Y-pr.Y)
				change := (want-pr.Angle+540)%360 - 180
				strength := max(0, d.Int("steer_strength", 15))
				pr.Angle = NormalizeAngle(pr.Angle + min(strength, max(-strength, change)))
			}
		}
		speed := pr.SpeedTick
		if d.Int("speed_change_ticks") > 0 {
			speed *= math.Max(.1, 1+float64(d.Int("speed_change_percent"))/100*math.Min(1, float64(pr.AgeTicks)/float64(d.Int("speed_change_ticks"))))
		}
		speed = math.Min(speed, math.Max(0, pr.RangePx-pr.Traveled))
		ox, oy := pr.X, pr.Y
		rad := float64(pr.Angle) * math.Pi / 180
		nx, ny := ox+math.Cos(rad)*speed, oy+math.Sin(rad)*speed
		pr.Traveled += speed
		if d.Bool("is_indirect") {
			distance := Dist(pr.SpawnX, pr.SpawnY, pr.TargetX, pr.TargetY)
			t := clampF(pr.Traveled/math.Max(1, distance), 0, 1)
			pr.X, pr.Y = pr.SpawnX+(pr.TargetX-pr.SpawnX)*t, pr.SpawnY+(pr.TargetY-pr.SpawnY)*t
			pr.Z = math.Sin(math.Pi*t) * float64(max(300, d.Int("gravity")*2))
			if t >= 1 || pr.Traveled >= pr.RangePx {
				endProjectile(w, pr, ProjRangeEnd)
			}
			continue
		}
		wall := false
		wallX, wallY := nx, ny
		if !pr.PassEnv {
			steps := max(1, int(math.Ceil(speed/30)))
			for i := 1; i <= steps; i++ {
				x, y := ox+(nx-ox)*float64(i)/float64(steps), oy+(ny-oy)*float64(i)/float64(steps)
				tx, ty := w.Tiles.tileAt(x, y)
				if projectileOpen(w, x, y) {
					continue
				}
				if pr.PierceEnv && w.Tiles.DestroyTile(tx, ty) {
					pr.TilePierced = true
					pr.PiercedTileIndex = ty*w.Tiles.W + tx
					continue
				}
				nx, ny = x, y
				wallX, wallY = x, y
				wall = true
				break
			}
		}
		type hit struct {
			p *Player
			t float64
		}
		hits := []hit{}
		for _, v := range w.Characters() {
			if !v.Alive || v.Abandoned || v.IsBall() || v == pr.Owner || v.Team == pr.Team || pr.HitSet[v] || pr.SharedHits[v] {
				continue
			}
			if t, ok := segmentHit(ox, oy, nx, ny, v.X, v.Y, float64(pr.Radius+v.CollisionRadius)); ok {
				hits = append(hits, hit{v, t})
			}
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].t == hits[j].t {
				return hits[i].p.ObjectIndex < hits[j].p.ObjectIndex
			}
			return hits[i].t < hits[j].t
		})
		pr.X, pr.Y = nx, ny
		for _, h := range hits {
			v := h.p
			if v.ShieldBuff && v.Upgrade.String("name") == "reflective_shield" && pr.Owner != nil && w.Catalog != nil {
				model := w.Catalog.Spec("projectiles", v.Upgrade.String("projectile_name"))
				spawnProjectile(w, v, model, angleTo(pr.Owner.X-v.X, pr.Owner.Y-v.Y), pr.Damage*v.Upgrade.Int("damage_percent", 80)/100, int(pr.RangePx), pr.Owner.X, pr.Owner.Y, false, pr.ExecutionID)
				endProjectileEffects(w, pr, ProjCancelled, false)
				break
			}
			if pr.HitSet == nil {
				pr.HitSet = map[*Player]bool{}
			}
			pr.HitSet[v] = true
			if pr.SharedHits != nil {
				pr.SharedHits[v] = true
			}
			damage := pr.Damage
			if pr.Returning && pr.Owner != nil && pr.Owner.StarPower.String("name") == "boomerang_projectile_damage" {
				damage += damage * pr.Owner.StarPower.Int("damage") / 100
			}
			applyDamage(w, pr.Owner, v, damage, pr.Ultimate)
			if v.Alive {
				source := -1
				if pr.Owner != nil {
					source = pr.Owner.ObjectIndex
				}
				if d.Int("freeze_strength") != 0 {
					AddStatus(w, v, "speed", -abs(d.Int("freeze_strength")), max(1, d.Int("freeze_duration_ms")/50), source)
				}
				push := float64(d.Int("pushback_strength"))
				if push != 0 {
					movePlayer(w, v, math.Cos(rad)*push, math.Sin(rad)*push)
				}
			}
			if !pr.PierceChars {
				pr.X, pr.Y = ox+(nx-ox)*h.t, oy+(ny-oy)*h.t
				endProjectile(w, pr, ProjCharacterImpact)
				break
			}
		}
		if !pr.Alive {
			continue
		}
		if wall {
			if pr.Bouncing {
				bx := !projectileOpen(w, wallX, oy)
				by := !projectileOpen(w, ox, wallY)
				if bx && !by {
					pr.Angle = NormalizeAngle(180 - pr.Angle)
				} else if by && !bx {
					pr.Angle = NormalizeAngle(-pr.Angle)
				} else {
					pr.Angle = NormalizeAngle(pr.Angle + 180)
				}
				pr.X, pr.Y = ox, oy
			} else {
				pr.X = clampF(pr.X, 0, w.Tiles.PixW)
				pr.Y = clampF(pr.Y, 0, w.Tiles.PixH)
				endProjectile(w, pr, ProjEnvironmentImpact)
			}
		}
		if pr.Alive && pr.Returning && pr.Owner != nil {
			if _, ok := segmentHit(ox, oy, pr.X, pr.Y, pr.Owner.X, pr.Owner.Y, float64(pr.Owner.CollisionRadius)); ok {
				endProjectileEffects(w, pr, ProjCancelled, false)
			}
		}
		if pr.Alive && (pr.Traveled >= pr.RangePx || pr.X <= 0 || pr.Y <= 0 || pr.X >= w.Tiles.PixW || pr.Y >= w.Tiles.PixH) {
			endProjectile(w, pr, ProjRangeEnd)
		}
	}
	kept := w.Projectiles[:0]
	for _, p := range w.Projectiles {
		if p.Alive || p.TerminalTicks > 0 {
			kept = append(kept, p)
		}
	}
	clear(w.Projectiles[len(kept):])
	w.Projectiles = kept
}
func projectileOpen(w *World, x, y float64) bool {
	if x < 0 || y < 0 || x >= w.Tiles.PixW || y >= w.Tiles.PixH {
		return false
	}
	tx, ty := w.Tiles.tileAt(x, y)
	i := ty*w.Tiles.W + tx
	return !w.Tiles.BlocksProj[i] || (i < len(w.Tiles.Destroyed) && w.Tiles.Destroyed[i])
}
func segmentHit(ax, ay, bx, by, x, y, r float64) (float64, bool) {
	dx, dy := bx-ax, by-ay
	fx, fy := ax-x, ay-y
	c := fx*fx + fy*fy - r*r
	if c <= 0 {
		return 0, true
	}
	a := dx*dx + dy*dy
	if a < .00001 {
		return 0, false
	}
	b := 2 * (fx*dx + fy*dy)
	disc := b*b - 4*a*c
	if disc < 0 {
		return 0, false
	}
	t := (-b - math.Sqrt(disc)) / (2 * a)
	return t, t >= 0 && t <= 1
}
func endProjectile(w *World, p *Projectile, state int) { endProjectileEffects(w, p, state, true) }
func endProjectileEffects(w *World, p *Projectile, state int, effects bool) {
	if !p.Alive {
		return
	}
	p.Alive = false
	p.State = state
	p.TerminalTicks = 4
	p.Z = 0
	owner := p.Owner
	if owner == nil || !effects || w.Catalog == nil {
		return
	}
	d := p.Definition
	if d.Bool("is_boomerang") && !p.Returning && owner.Alive {
		back := w.Catalog.Spec("projectiles", d.String("chain_projectile_name"))
		if !back.Exists() {
			back = d
		}
		if next := spawnProjectile(w, owner, back, angleTo(owner.X-p.X, owner.Y-p.Y), p.Damage, int(p.RangePx), owner.X, owner.Y, p.Ultimate, p.ExecutionID); next != nil {
			next.X, next.Y, next.SpawnX, next.SpawnY = p.X, p.Y, p.X, p.Y
			next.Returning = true
		}
	}
	area := d.String("spawn_area_effect_name")
	if p.Ultimate && owner.Upgrade.String("name") == "override_projectile_area_effect" {
		area = owner.Upgrade.String("area_effect_name")
	}
	if !p.Ultimate && owner.StarPower.String("name") == "override_attack_projectile_area_effect" {
		area = owner.StarPower.String("projectile_area_effect_name")
	}
	SpawnArea(w, owner, area, p.X, p.Y, 0, 0, false, p.Ultimate)
	if p.SummonName != "" {
		if p.SummonBehavior == "for_teammates" {
			for _, t := range w.Players {
				if t.Alive && t.Team == owner.Team {
					SpawnMinion(w, t, p.SummonName, t.X, t.Y)
				}
			}
		} else {
			SpawnMinion(w, owner, p.SummonName, p.X, p.Y)
		}
	}
	chain := w.Catalog.Spec("projectiles", d.String("chain_projectile_name"))
	if chain.Exists() && !d.Bool("is_boomerang") && p.ChainDepth < 8 {
		n := min(16, max(1, d.Int("chain_bullets", 1)))
		for i := 0; i < n; i++ {
			next := spawnProjectile(w, owner, chain, p.Angle+int((float64(i)-float64(n-1)/2)*15), p.Damage, max(100, d.Int("chain_travel_distance", 10)*100), p.TargetX, p.TargetY, p.Ultimate, p.ExecutionID)
			if next != nil {
				next.X, next.Y, next.SpawnX, next.SpawnY = p.X, p.Y, p.X, p.Y
				next.ChainDepth = p.ChainDepth + 1
			}
		}
	}
}
