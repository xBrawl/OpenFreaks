package sim

import (
	"github.com/freaks/openfreaks/internal/content"
	"math"
)

type AugmentState struct {
	Definitions                                            map[string]content.Spec
	WeaponCasts                                            int
	InvulnerableUntil, PhantomReady, RageReady, RegenUntil int32
	RegenArmed, SacrificeArmed, Vengeance                  bool
	Hits                                                   map[int]int32
	Expires                                                int32
}

func (p *Player) augment(name string) content.Spec {
	if p == nil || p.Augments == nil {
		return content.Spec{}
	}
	return p.Augments.Definitions[name]
}

var SupportedAugments = map[string]bool{
	"executor": true, "decoy": true, "phantom_teleporter": true, "sniper": true, "vampire": true, "titan_boots": true,
	"accelerating_evolution": true, "back_to_basics": true, "brute_force": true, "cannot_touch": true,
	"healthy_and_peaceful": true, "death_cycle": true, "decisiveness": true, "machine_gun": true,
	"corrosion": true, "goliath": true, "tact_2": true, "tact_4": true, "mad": true, "dualism": true,
	"troublemaker": true, "simultaneous_attack": true, "healing_circle": true, "crossfire": true,
	"echo_of_ultimate": true, "sacrificial_shield": true, "bloody_vengeance": true, "symbiosis": true,
	"first_aid_kit": true, "sacrifice": true,
}

func ApplyAugments(w *World, p *Player, ids []int32) {
	if w.Catalog == nil || len(ids) == 0 {
		return
	}
	p.Augments = &AugmentState{Definitions: map[string]content.Spec{}, RegenArmed: true, SacrificeArmed: true, Hits: map[int]int32{}}
	for _, id := range ids {
		d := w.Catalog.SpecByID("augments", int(id))
		name := d.String("name")
		if !SupportedAugments[name] || p.augment(name).Exists() {
			continue
		}
		p.Augments.Definitions[name] = d
		p.AugmentIDs = append(p.AugmentIDs, id)
		value := d.Int("value")
		switch name {
		case "sniper":
			if len(p.Skills) > 0 {
				p.Skills[0].RangePx = p.Skills[0].RangePx * (100 + value) / 100
				p.Skills[0].MaxCharge = max(1, p.Skills[0].MaxCharge-1)
				p.Skills[0].Charges = p.Skills[0].MaxCharge
			}
		case "machine_gun":
			if len(p.Skills) > 0 {
				p.Skills[0].MaxCharge = min(8, p.Skills[0].MaxCharge+value)
				p.Skills[0].Charges = p.Skills[0].MaxCharge
			}
		case "titan_boots":
			p.MaxHP = p.MaxHP * (100 + value) / 100
			p.SpeedAdd -= d.Int("value2")
		case "healthy_and_peaceful":
			p.MaxHP += value
			p.BaseDamageAddPct -= d.Int("value2")
		case "back_to_basics":
			p.BaseDamageAddPct += value
			p.SpeedAdd += value
		case "brute_force":
			p.BaseDamageAddPct += value
		case "goliath":
			p.CollisionRadius += value
			p.MaxHP = p.MaxHP * (100 + d.Int("value2")) / 100
			p.BaseDamageAddPct += d.Int("value3")
		case "dualism":
			p.UltChargeMult = p.UltChargeMult * (100 + value) / 100
			p.UltUltMult = p.UltUltMult * (100 + value) / 100
		case "mad":
			if (p.Index+int(w.TickNum)+len(w.BattleID))%2 == 0 {
				p.BaseDamageAddPct += value
				p.MaxHP = p.MaxHP * (100 + d.Int("value2")) / 100
			} else {
				p.SpeedAdd += d.Int("value3")
				p.UltCharge = 4000 * d.Int("value3") / 100
			}
		}
	}
	p.MaxHP = min(65535, p.MaxHP)
	p.HP = p.MaxHP
	p.DamageAddPct = p.BaseDamageAddPct
}
func augmentWeaponCast(p *Player) int {
	if p.Augments == nil {
		return 0
	}
	p.Augments.WeaponCasts++
	bonus := 0
	for _, name := range []string{"tact_2", "tact_4"} {
		if d := p.augment(name); d.Exists() && p.Augments.WeaponCasts%max(1, d.Int("value2")) == 0 {
			bonus += d.Int("value")
		}
	}
	return bonus
}
func onAugmentUltimate(w *World, p *Player) {
	if p.Augments != nil {
		if d := p.augment("cannot_touch"); d.Exists() {
			p.Augments.InvulnerableUntil = w.TickNum + int32(d.Int("value"))
		}
		if d := p.augment("accelerating_evolution"); d.Exists() {
			p.UltChargeMult = min(10000, p.UltChargeMult*(100+d.Int("value"))/100)
		}
		if d := p.augment("dualism"); d.Exists() {
			p.BaseDamageAddPct = min(1000, p.BaseDamageAddPct+d.Int("value2"))
		}
		if d := p.augment("troublemaker"); d.Exists() {
			Heal(p, p.MaxHP*d.Int("value")/100)
		}
	}
	for _, ally := range w.Players {
		if ally != p && ally.Alive && ally.Team == p.Team {
			if d := ally.augment("echo_of_ultimate"); d.Exists() {
				ally.UltCharge = min(4000, ally.UltCharge+4000*d.Int("value")/100)
			}
		}
	}
}
func nearestAlly(w *World, p *Player, radius float64) *Player {
	var best *Player
	for _, ally := range w.Players {
		if ally == p || !ally.Alive || ally.Abandoned || ally.Team != p.Team {
			continue
		}
		if d := Dist(p.X, p.Y, ally.X, ally.Y); d < radius {
			best = ally
			radius = d
		}
	}
	return best
}
func augmentDefend(w *World, attacker, victim *Player, amount int, procs bool) int {
	if victim.Augments != nil && victim.Augments.InvulnerableUntil > w.TickNum {
		return 0
	}
	defense := 0
	for _, e := range victim.Effects {
		if (e.Kind == "defense" || e.Kind == "shared_defense") && e.Expires > w.TickNum {
			defense = max(defense, e.Value)
		}
	}
	amount = amount * (100 - min(90, defense)) / 100
	if !procs {
		return amount
	}
	if d := victim.augment("phantom_teleporter"); d.Exists() && w.TickNum >= victim.Augments.PhantomReady {
		victim.Augments.PhantomReady = w.TickNum + int32(d.Int("value"))
		amount = amount * (100 - d.Int("value2")) / 100
		a := float64(w.TickNum+int32(victim.Index*71)) * .73
		teleportPlayer(w, victim, victim.X+math.Cos(a)*float64(d.Int("value3")), victim.Y+math.Sin(a)*float64(d.Int("value3")))
	}
	// One protector only; redirected damage cannot redirect again.
	for _, ally := range w.Players {
		if ally == victim || !ally.Alive || ally.Team != victim.Team || Dist(ally.X, ally.Y, victim.X, victim.Y) > 1200 {
			continue
		}
		if d := ally.augment("sacrificial_shield"); d.Exists() {
			share := amount * d.Int("value") / 100
			amount -= share
			AddStatus(w, victim, "defense", d.Int("value2"), d.Int("value3"), ally.ObjectIndex)
			AddStatus(w, ally, "defense", d.Int("value2"), d.Int("value3"), ally.ObjectIndex)
			applyDamageCore(w, attacker, ally, share, false, false, false)
			break
		}
	}
	return amount
}
func onAugmentDamage(w *World, attacker, victim *Player, amount int) {
	if d := attacker.augment("vampire"); d.Exists() {
		Heal(attacker, amount*d.Int("value")/100)
	}
	if d := attacker.augment("corrosion"); d.Exists() && victim.HP > 0 {
		stacks := d.Int("value")
		for _, e := range victim.Effects {
			if e.Kind == "corrosion" && e.Source == attacker.ObjectIndex {
				stacks += e.Value
			}
		}
		AddStatus(w, victim, "corrosion", min(32767, stacks), 80, attacker.ObjectIndex)
	}
	if victim.Augments == nil {
		victim.Augments = &AugmentState{Definitions: map[string]content.Spec{}, Hits: map[int]int32{}}
	}
	if victim.Augments.Hits == nil {
		victim.Augments.Hits = map[int]int32{}
	}
	for source, last := range victim.Augments.Hits {
		ally := w.FindChar(source)
		if ally == nil || ally == attacker || ally.Team != attacker.Team {
			continue
		}
		for _, owner := range []*Player{attacker, ally} {
			if d := owner.augment("simultaneous_attack"); d.Exists() && w.TickNum-last <= int32(d.Int("value")) && w.TickNum >= owner.Augments.RageReady {
				AddStatus(w, owner, "damage", d.Int("value2"), d.Int("value3"), owner.ObjectIndex)
				owner.Augments.RageReady = w.TickNum + int32(d.Int("value4"))
			}
			if d := owner.augment("crossfire"); d.Exists() && w.TickNum-last <= int32(d.Int("value")) {
				AddStatus(w, victim, "speed", -d.Int("value2"), d.Int("value3"), owner.ObjectIndex)
			}
		}
	}
	victim.Augments.Hits[attacker.ObjectIndex] = w.TickNum
}
func onAugmentDeath(w *World, killer, victim *Player) {
	if victim.Owner != nil || victim.HeroType != "hero" {
		return
	}
	if killer != nil && killer.Owner != nil {
		killer = killer.Owner
	}
	if killer != nil && killer.augment("executor").Exists() {
		killer.UltCharge = 4000
	}
	if victim.Augments != nil {
		for index, tick := range victim.Augments.Hits {
			if w.TickNum-tick <= 200 {
				if helper := w.FindChar(index); helper != nil && helper.Alive && helper.augment("executor").Exists() {
					helper.UltCharge = 4000
				}
			}
		}
	}
	for _, ally := range w.Players {
		if ally != victim && ally.Alive && ally.Team == victim.Team {
			if d := ally.augment("bloody_vengeance"); d.Exists() && !ally.Augments.Vengeance {
				ally.Augments.Vengeance = true
				ally.BaseDamageAddPct += d.Int("value")
			}
		}
	}
}
func TickAugments(w *World) {
	for _, p := range w.Characters() {
		if !p.Alive {
			continue
		}
		if d := p.augment("mad"); d.Exists() && (p.Index+len(w.BattleID))%2 != 0 {
			p.UltCharge = max(p.UltCharge, 4000*d.Int("value3")/100)
		}
		if w.TickNum%20 == 0 {
			for _, e := range append([]StatusEffect(nil), p.Effects...) {
				if e.Kind == "corrosion" && e.Expires > w.TickNum {
					applyDamageProc(w, w.FindChar(e.Source), p, e.Value, false, false)
				}
			}
		}
		if p.Augments == nil {
			continue
		}
		state := p.Augments
		if state.Expires > 0 && w.TickNum >= state.Expires {
			p.Alive = false
			p.HP = 0
			continue
		}
		if !p.Alive {
			continue
		}
		if d := p.augment("decisiveness"); d.Exists() {
			if p.HP*100 >= p.MaxHP*d.Int("value") {
				state.RegenArmed = true
			}
			if state.RegenArmed && p.HP*100 < p.MaxHP*d.Int("value") {
				state.RegenArmed = false
				state.RegenUntil = w.TickNum + int32(d.Int("value3"))
			}
			if state.RegenUntil > w.TickNum && w.TickNum%20 == 0 {
				Heal(p, p.MaxHP*d.Int("value2")/100*20/max(20, d.Int("value3")))
			}
		}
		if d := p.augment("decoy"); d.Exists() && w.TickNum > 0 && w.TickNum%int32(max(1, d.Int("value"))) == 0 {
			if clone := SpawnMinion(w, p, p.Definition.String("name"), p.X+220, p.Y); clone != nil {
				clone.HP = 1
				clone.MaxHP = 1
				clone.HeroType = "minion_mirage"
				clone.Skills = buildSkills(w.Catalog, w.Catalog.CharByID[int(p.ModelID)], BuildPlayer{DashID: int(p.DashModelID), UltUpgradeID: -1, StarPowerID: -1})
				clone.Augments = &AugmentState{Expires: w.TickNum + int32(d.Int("value2"))}
			}
		}
		ally := nearestAlly(w, p, 900)
		if ally == nil {
			continue
		}
		if d := p.augment("healing_circle"); d.Exists() && w.TickNum%20 == 0 {
			Heal(p, p.MaxHP*d.Int("value")/100)
			healFrom(ally, p, ally.MaxHP*d.Int("value")/100)
		}
		if d := p.augment("sacrifice"); d.Exists() {
			if ally.HP*100 >= ally.MaxHP*d.Int("value") {
				state.SacrificeArmed = true
			}
			if state.SacrificeArmed && ally.HP*100 < ally.MaxHP*d.Int("value") && p.HP > 1 {
				state.SacrificeArmed = false
				cost := min(p.HP-1, p.HP*d.Int("value2")/100)
				p.HP -= cost
				giveShieldFrom(ally, p, cost, 200)
			}
		}
		if d := p.augment("symbiosis"); d.Exists() {
			damage, speed, defense := max(0, p.BaseDamageAddPct), max(0, p.SpeedAdd), 0
			for _, e := range p.Effects {
				if e.Expires <= w.TickNum {
					continue
				}
				switch e.Kind {
				case "damage":
					damage += max(0, e.Value)
				case "speed":
					speed += max(0, e.Value)
				case "defense":
					defense = max(defense, e.Value)
				}
			}
			for kind, value := range map[string]int{"shared_damage": damage, "shared_speed": speed, "shared_defense": defense} {
				AddStatus(w, ally, kind, value*d.Int("value")/100, 2, p.ObjectIndex)
			}
		}
	}
}
func augmentationHealing(p, source *Player, amount int) int {
	bonus := p.augment("first_aid_kit").Int("value") + p.augment("back_to_basics").Int("value")
	if source != nil && source != p {
		bonus += source.augment("first_aid_kit").Int("value") + source.augment("back_to_basics").Int("value")
	}
	return amount * (100 + bonus) / 100
}
func onAugmentHeal(p *Player, value int) {
	if d := p.augment("death_cycle"); d.Exists() && p.World != nil {
		w := p.World
		var enemy *Player
		distance := 1800.0
		for _, v := range w.Players {
			if v.Alive && v.Team != p.Team {
				if dist := Dist(p.X, p.Y, v.X, v.Y); dist < distance {
					enemy = v
					distance = dist
				}
			}
		}
		if enemy != nil {
			applyDamageProc(w, p, enemy, value*d.Int("value")/100, false, false)
		}
	}
}
