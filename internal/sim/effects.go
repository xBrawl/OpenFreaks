package sim

import "math"

func Heal(p *Player, amount int) {
	healFrom(p, p, amount)
}
func healFrom(p, source *Player, amount int) {
	if !p.Alive || amount <= 0 {
		return
	}
	value := min(augmentationHealing(p, source, amount), p.MaxHP-p.HP)
	p.HP += value
	if value > 0 {
		p.DamageDeltas = append(p.DamageDeltas, -min(32767, value))
		onAugmentHeal(p, value)
		if source != nil {
			if source.Owner != nil {
				source = source.Owner
			}
			source.HealingDone += value
		}
	}
}
func GiveShield(p *Player, amount, ticks int) {
	giveShieldFrom(p, p, amount, ticks)
}
func giveShieldFrom(p, source *Player, amount, ticks int) {
	amount = augmentationHealing(p, source, amount)
	if amount > 0 {
		p.ConsumableShield = max(p.ConsumableShield, amount)
		p.ConsumableTicks = max(p.ConsumableTicks, ticks)
	}
}
func AddStatus(w *World, p *Player, kind string, value, ticks, source int) {
	// Пациент Зеро: стан и замедления вдвое короче (control_resistance_percent).
	if resist := p.Definition.Int("control_resistance_percent"); resist > 0 && (kind == "stun" || kind == "speed" && value < 0) {
		ticks = max(1, ticks*(100-min(90, resist))/100)
	}
	for i, e := range p.Effects {
		if e.Kind == kind && e.Source == source {
			p.Effects[i] = StatusEffect{kind, value, source, w.TickNum + int32(max(1, ticks))}
			return
		}
	}
	p.Effects = append(p.Effects, StatusEffect{kind, value, source, w.TickNum + int32(max(1, ticks))})
}
func SpawnArea(w *World, owner *Player, name string, x, y float64, damage, heal int, following, ultimate bool) *Area {
	if w.Catalog == nil || len(w.Areas) >= 256 {
		return nil
	}
	d := w.Catalog.Spec("area_effects", name)
	if !d.Exists() {
		return nil
	}
	index := w.AllocEffectIndex()
	if index == 0 {
		return nil
	}
	a := &Area{Owner: owner, Definition: d, LocalIndex: index, OwnerIndex: owner.ObjectIndex, RosterIndex: owner.Index, Team: owner.Team, X: x, Y: y,
		TimeMsLeft: max(50, d.Int("time_ms", 50)), Damage: damage, Heal: heal, Following: following, Ultimate: ultimate, NextPulseTick: w.TickNum}
	if a.Damage <= 0 {
		a.Damage = d.Int("damage")
	}
	if a.Heal <= 0 {
		a.Heal = d.Int("heal")
	}
	w.Areas = append(w.Areas, a)
	return a
}
func SpawnPortal(w *World, owner *Player, name string, x, y float64) {
	if w.Catalog == nil || len(w.Items) >= 256 {
		return
	}
	d := w.Catalog.Spec("items", name)
	if !d.Exists() {
		return
	}
	index := w.AllocEffectIndex()
	if index == 0 {
		return
	}
	x, y = w.Tiles.FindOpen(x, y, owner.CollisionRadius)
	w.Items = append(w.Items, &Item{Definition: d, LocalIndex: index, OwnerIndex: owner.ObjectIndex, RosterIndex: owner.Index, Team: owner.Team,
		X: owner.X, Y: owner.Y, ExitX: x, ExitY: y, TimeMsLeft: d.Int("life_time_ms", 12000), State: 1, StateTicks: d.Int("value", 60), UsedAt: map[int]int32{}})
	SpawnArea(w, owner, d.String("first_object_name"), owner.X, owner.Y, 0, 0, false, false)
	SpawnArea(w, owner, d.String("second_object_name"), x, y, 0, 0, false, false)
}
func (g *TileGrid) DestroyTile(x, y int) bool {
	if !g.at(x, y) {
		return false
	}
	i := y*g.W + x
	if !g.Destructible[i] || g.Destroyed[i] {
		return false
	}
	g.Destroyed[i] = true
	return true
}
func teleportPlayer(w *World, p *Player, x, y float64) {
	x, y = w.Tiles.FindOpen(x, y, p.CollisionRadius)
	p.X, p.Y, p.ExactX, p.ExactY = x, y, x, y
	p.Teleporting = true
	p.TeleportLeft = 4
	p.HasMoveTarget = false
}

func TickEffects(w *World) {
	for _, p := range w.Characters() {
		kept := p.Effects[:0]
		for _, e := range p.Effects {
			if p.Alive && e.Expires > w.TickNum {
				kept = append(kept, e)
			}
		}
		p.Effects = kept
		p.EffectsSpeedAdd = 0
		p.ReloadAddPct = 0
		p.ControlRemoved = false
		p.Spot = false
		p.Electrocuted = false
		p.DamageAddPct = p.BaseDamageAddPct
		for _, e := range p.Effects {
			switch e.Kind {
			case "speed", "shared_speed":
				p.EffectsSpeedAdd += e.Value
			case "damage", "shared_damage":
				p.DamageAddPct += e.Value
			case "reload":
				p.ReloadAddPct += e.Value
			case "stun":
				p.ControlRemoved = true
			case "spot":
				p.Spot = true
			case "electric":
				p.Electrocuted = true
			}
		}
		p.EffectsSpeedAdd = max(-99, min(500, p.EffectsSpeedAdd))
		p.Slowdown = p.EffectsSpeedAdd < 0
		if p.SegmentTicks > 0 {
			p.SegmentTicks--
			if p.SegmentTicks == 0 {
				p.SegmentPercent = 0
				p.SegmentLevel = 0
			}
		}
		if p.Alive && w.TickNum%20 == 0 {
			loss := p.Definition.Int("hit_points_lost_per_second") + p.MaxHP*p.Definition.Int("hit_points_lost_percent_per_second")/100
			if loss > 0 {
				p.HP = max(0, p.HP-loss)
				if p.HP == 0 {
					Kill(w, nil, p)
				}
			}
			Heal(p, p.MaxHP*p.Definition.Int("hit_points_regeneration_percent_per_second")/100)
		}
		if p.Alive && p.HeroType == "hero" {
			passiveEffects(w, p)
		}
	}
	for _, a := range w.Areas {
		owner := w.FindChar(a.OwnerIndex)
		if owner == nil {
			owner = a.Owner
		}
		if owner == nil {
			a.TimeMsLeft -= TickMs
			continue
		}
		if a.Following {
			if !owner.Alive {
				a.TimeMsLeft = 0
				continue
			}
			a.X, a.Y = owner.X, owner.Y
		}
		d := a.Definition
		radius := d.Int("radius", 300)
		if w.TickNum >= a.NextPulseTick {
			kind := d.String("type_name", d.String("type"))
			for _, p := range w.Characters() {
				if !p.Alive || p.IsBall() {
					continue
				}
				if d := Dist(p.X, p.Y, a.X, a.Y); isTitan(p) && bodyDist(p, a.X, a.Y) > float64(radius) || !isTitan(p) && d > float64(radius) {
					continue
				}
				if p.ObjectIndex == a.OwnerIndex && (d.Bool("area_effect_cannot_affect_owner") || d.Bool("area_effect_cannot_damage_owner")) {
					continue
				}
				friend := p.Team == a.Team
				if !friend && a.Damage > 0 {
					applyDamageProc(w, owner, p, a.Damage, a.Ultimate, !a.SuppressHitProcs)
				}
				if friend {
					heal := a.Heal
					if heal <= 0 {
						if kind == "damage_heal" {
							heal = a.Damage
						} else if kind == "heal" || kind == "cross_heal_team" {
							heal = p.MaxHP * d.Int("heal_percent") / 100
						}
					}
					healFrom(p, owner, heal)
					giveShieldFrom(p, owner, d.Int("consumable_shield"), d.Int("consumable_shield_ticks"))
				}
				teamKey := "enemies"
				if friend {
					teamKey = "teammates"
				}
				speed := d.At("speed_change").At(teamKey).List()
				if len(speed) >= 2 {
					AddStatus(w, p, "speed", speed[0].Number(), speed[1].Number(), a.OwnerIndex)
				}
				reload := d.At("reload_speed_change").List()
				if len(reload) >= 2 {
					AddStatus(w, p, "reload", reload[0].Number(), reload[1].Number(), a.OwnerIndex)
				}
				if !friend {
					spot := d.At("spot_damage").List()
					if len(spot) >= 2 {
						AddStatus(w, p, "spot", spot[0].Number(), spot[1].Number(), a.OwnerIndex)
					}
					if n := d.Int("madness_ticks"); n > 0 {
						AddStatus(w, p, "stun", 1, n, a.OwnerIndex)
					}
					dx, dy := p.X-a.X, p.Y-a.Y
					distance := math.Hypot(dx, dy)
					push := float64(d.Int("pushback_strength"))
					if push != 0 && distance > float64(d.Int("pushback_dead_zone")) && infectionPushAllowed(w, p) {
						movePlayer(w, p, dx/math.Max(1, distance)*push, dy/math.Max(1, distance)*push)
					}
				}
			}
			if d.Bool("destroys_environment") {
				for x := int(a.X-float64(radius)) / 300; x <= int(a.X+float64(radius))/300; x++ {
					for y := int(a.Y-float64(radius)) / 300; y <= int(a.Y+float64(radius))/300; y++ {
						w.Tiles.DestroyTile(x, y)
					}
				}
			}
			a.NextPulseTick = w.TickNum + int32(max(1, d.Int("same_area_effect_cannot_damage_ms", d.Int("time_ms", 1000))/50))
		}
		a.TimeMsLeft -= TickMs
	}
	areas := w.Areas[:0]
	for _, a := range w.Areas {
		if a.TimeMsLeft > 0 {
			areas = append(areas, a)
		}
	}
	clear(w.Areas[len(areas):])
	w.Areas = areas
	for _, item := range w.Items {
		d := item.Definition
		item.TimeMsLeft -= TickMs
		portal := d.String("name") == "portal"
		if portal && item.StateTicks > 0 {
			item.StateTicks--
			if item.StateTicks == 0 {
				item.State = item.State%3 + 1
				key := "value"
				if item.State == 2 {
					key = "value2"
				}
				if item.State == 3 {
					key = "value3"
				}
				item.StateTicks = d.Int(key, 60)
			}
		}
		for _, p := range w.Players {
			if !p.Alive || p.Abandoned || (item.Team >= 0 && p.Team != item.Team) || item.TimeMsLeft <= 0 {
				continue
			}
			radius := float64(d.Int("trigger_range", 3) * 100)
			nearStart := Dist(p.X, p.Y, item.X, item.Y) <= radius
			nearEnd := portal && Dist(p.X, p.Y, item.ExitX, item.ExitY) <= radius
			if !nearStart && !nearEnd {
				continue
			}
			if portal {
				if item.State != 2 {
					continue
				}
				if last, used := item.UsedAt[p.ObjectIndex]; used && w.TickNum-last < 20 {
					continue
				}
				if nearStart {
					teleportPlayer(w, p, item.ExitX, item.ExitY)
				} else {
					teleportPlayer(w, p, item.X, item.Y)
				}
				item.UsedAt[p.ObjectIndex] = w.TickNum
			} else if d.Bool("can_be_picked_up") {
				pickUpItem(w, p, item)
				item.TimeMsLeft = 0
			}
		}
	}
	items := w.Items[:0]
	for _, i := range w.Items {
		if i.TimeMsLeft > 0 {
			items = append(items, i)
		}
	}
	clear(w.Items[len(items):])
	w.Items = items
}
