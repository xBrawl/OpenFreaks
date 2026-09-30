package sim

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"math"
)

// KillEvent — одно событие убийства. Tick нужен не для провода, а для
// повторной отправки: одноразовые события жили ровно один снимок, а снимки
// идут unreliable и вытесняются более свежими, поэтому киллфид терял записи.
type KillEvent struct {
	VictimIndex, Points int
	Tick                int32
}

const KillRepeatTicks = 6

func SpawnMinion(w *World, owner *Player, name string, x, y float64) *Player {
	if w.Catalog == nil || len(w.Minions) >= 128 {
		return nil
	}
	if name == "karity_minion_character" {
		living := 0
		for _, minion := range w.Minions {
			if minion != nil && minion.Alive && minion.Owner == owner &&
				minion.Definition.String("name") == name {
				living++
			}
		}
		if living >= 3 {
			return nil
		}
	}
	ch := w.Catalog.CharByName[name]
	if ch == nil {
		return nil
	}
	// Телевизор Кролика один: новый выгоняет старый (с «Передачей» тот взрывается).
	if w.Catalog.Spec("characters", name).Bool("replaces_previous") {
		for _, minion := range w.Minions {
			if minion != nil && minion.Alive && minion.Owner == owner && minion.Definition.String("name") == name {
				Kill(w, nil, minion)
			}
		}
	}
	id := w.AllocCharacterIndex()
	if id == 0 {
		return nil
	}
	p := buildPlayer(w.Catalog, w, BuildPlayer{Index: owner.Index, Team: owner.Team, PlayerID: -1000000 - int64(id), CharacterID: ch.Id, DashID: -1, UltUpgradeID: -1, StarPowerID: -1}, ch)
	p.ObjectIndex = id
	p.Owner = owner
	p.SpawnShield = false
	p.SpawnShieldTicks = 0
	p.Upgrade = owner.Upgrade
	p.X, p.Y = w.Tiles.FindOpen(x, y, p.CollisionRadius)
	p.ExactX, p.ExactY = p.X, p.Y
	p.SpawnX, p.SpawnY = p.X, p.Y
	w.Minions = append(w.Minions, p)
	w.TouchMinions()
	if isRabbitTV(p) {
		rabbitTVSpawned(w, p)
	}
	return p
}
func TickMinions(w *World) {
	for _, p := range w.Minions {
		if !p.Alive || p.IsBall() || p.ControlRemoved || p.HeroType == "minion_mirage" || isTitan(p) || p.KarityGrab != nil || rabbitGlobalTV(p) {
			continue
		}
		friendly := p.HeroType == "minion_find_teammates"
		var target *Player
		best := math.MaxFloat64
		for _, v := range w.Characters() {
			if v == p || !v.Alive || v.Abandoned || v.IsBall() {
				continue
			}
			if friendly {
				if v.Team != p.Team || v.Owner != nil || v.HeroType != "hero" {
					continue
				}
			} else if v.Team == p.Team {
				continue
			}
			if d := Dist(p.X, p.Y, v.X, v.Y); d < best {
				best = d
				target = v
			}
		}
		if target == nil {
			continue
		}
		if p.BaseSpeed > 0 {
			if p.Brain == nil {
				p.Brain = &BotBrain{}
			}

			botNavigate(w, p, navPoint{target.X, target.Y}, int(w.TickNum))
		}
		if len(p.Skills) > 0 && best <= float64(p.Skills[0].RangePx) {
			TryCast(w, p, p.Skills[0], protocol.PlayerAction{X: int(target.X - p.X), Y: int(target.Y - p.Y)}, false)
		}
	}

	kept := w.Minions[:0]
	for _, p := range w.Minions {
		if p.Alive || p.IsBall() || w.TickNum-p.DeathTick < 5 {
			kept = append(kept, p)
		}
	}
	clear(w.Minions[len(kept):])
	w.Minions = kept
	w.TouchMinions()
}
func passiveEffects(w *World, p *Player) {
	p.PassiveTicks++
	s := p.StarPower
	switch s.String("name") {
	case "following_minion_spawn":
		if p.PassiveTicks%max(1, s.Int("auto_spawn_delay_ticks", 200)) == 0 {
			existing := 0
			for _, m := range w.Minions {
				if m.Owner == p && m.Alive && m.Definition.String("name") == s.String("minion_character_name") {
					existing++
				}
			}
			for i := existing; i < min(16, max(1, s.Int("count", 1))); i++ {
				SpawnMinion(w, p, s.String("minion_character_name"), p.X, p.Y)
			}
		}
	case "following_area_effect":
		found := false
		for _, a := range w.Areas {
			if a.OwnerIndex == p.ObjectIndex && a.TimeMsLeft > 0 && a.Definition.String("name") == s.String("area_effect_name") {
				found = true
				break
			}
		}
		if !found {
			SpawnArea(w, p, s.String("area_effect_name"), p.X, p.Y, 0, 0, true, false)
		}
	case "helping_feather":
		if p.PassiveTicks%max(1, s.Int("charge_up_milliseconds", 2000)/50) == 0 && w.Catalog != nil {
			count := 0
			for _, i := range w.Items {
				if i.OwnerIndex == p.ObjectIndex {
					count++
				}
			}
			d := w.Catalog.Spec("items", s.String("item_name"))
			for i := 0; d.Exists() && i < max(1, s.Int("item_count_per_spawn", 1)) && count < s.Int("max_item_count", 3) && len(w.Items) < 256; i++ {
				index := w.AllocEffectIndex()
				if index == 0 {
					break
				}
				x, y := w.Tiles.FindOpen(p.X+150, p.Y, p.CollisionRadius)
				w.Items = append(w.Items, &Item{Definition: d, LocalIndex: index, OwnerIndex: p.ObjectIndex, RosterIndex: p.Index, Team: p.Team, X: x, Y: y, TimeMsLeft: d.Int("life_time_ms", 5000)})
				count++
			}
		}
	}
}
func onDamageEffects(w *World, owner, target *Player, amount int) {

	if owner.Upgrade.String("name") == "spot_explosion" {
		found := false
		kept := target.Effects[:0]
		for _, e := range target.Effects {
			if e.Kind == "spot" && e.Source == owner.ObjectIndex {
				found = true
			} else {
				kept = append(kept, e)
			}
		}
		target.Effects = kept
		if found {
			target.Spot = false
			if area := SpawnArea(w, owner, owner.Upgrade.String("area_effect_name"), target.X, target.Y, amount*owner.Upgrade.Int("damage_percent", 25)/100, 0, false, false); area != nil {
				area.SuppressHitProcs = true
			}
		}
	}
	star := owner.StarPower
	switch star.String("name") {
	case "attack_heal":
		Heal(owner, star.Int("heal_value"))
		GiveShield(owner, star.Int("consumable_shield_value"), star.Int("consumable_shield_ticks"))
	case "attack_spot":
		AddStatus(w, target, "spot", star.Int("spot_extra_damage_percent"), star.Int("spot_ticks"), owner.ObjectIndex)
	case "attack_freeze":
		AddStatus(w, target, "speed", -abs(star.Int("freeze_strength")), star.Int("freeze_duration"), owner.ObjectIndex)
	case "static_electricity":
		AddStatus(w, owner, "speed", star.Int("extra_speed"), star.Int("speed_ticks"), owner.ObjectIndex)
		AddStatus(w, target, "speed", star.Int("freeze_strength"), star.Int("speed_ticks"), owner.ObjectIndex)
	case "shield_after_caused_damage":
		GiveShield(owner, amount*star.Int("shield_percent")/100, star.Int("shield_ticks"))
	case "attack_absorption":
		GiveShield(owner, amount*star.Int("shield_percent")/100, star.Int("shield_ticks"))
		owner.HitCounter++
		if owner.HitCounter >= max(1, star.Int("required_charge_up", 5)) {
			owner.HitCounter = 0
			if w.Catalog != nil {
				d := w.Catalog.Spec("projectiles", star.String("projectile_name"))
				spawnProjectile(w, owner, d, angleTo(target.X-owner.X, target.Y-owner.Y), star.Int("projectile_base_damage"), 2000, target.X, target.Y, false, 0)
			}
		}
	}

}
