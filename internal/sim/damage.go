package sim

import "github.com/freaks/openfreaks/internal/content"

func DealDamage(w *World, attacker, victim *Player, amount int) {
	applyDamage(w, attacker, victim, amount, false)
}
func applyDamage(w *World, attacker, victim *Player, amount int, ultimate bool) {
	applyDamageProc(w, attacker, victim, amount, ultimate, true)
}
func applyDamageProc(w *World, attacker, victim *Player, amount int, ultimate, procs bool) {
	applyDamageCore(w, attacker, victim, amount, ultimate, procs, true)
}
func applyDamageCore(w *World, attacker, victim *Player, amount int, ultimate, procs, scale bool) {
	if w.Finished || !victim.Alive || victim.Abandoned || victim.IsBall() || victim.SpawnShield || amount <= 0 {
		return
	}
	if attacker != nil && attacker != victim && attacker.Team == victim.Team {
		return
	}
	if attacker != nil && scale {
		amount = int(int64(amount) * int64(100+attacker.DamageAddPct) / 100)
		if attacker.DamageScaleBP > 0 && attacker.DamageScaleBP != content.BalanceUnit {
			amount = content.ScaleBP(amount, attacker.DamageScaleBP)
		}
		spot := 0
		for _, e := range victim.Effects {
			if e.Kind == "spot" && e.Source == attacker.ObjectIndex && e.Expires > w.TickNum {
				spot += e.Value
			}
		}
		amount = int(int64(amount) * int64(100+spot) / 100)
		if d := attacker.augment("executor"); d.Exists() && victim.HP*100 < victim.MaxHP*d.Int("value2", 50) {
			amount = amount * (100 + d.Int("value")) / 100
		}
	}
	amount = augmentDefend(w, attacker, victim, amount, procs)
	remaining := min(32767, max(0, amount))
	if victim.ShieldBuff {
		remaining /= 2
	}
	if victim.SegmentTicks > 0 {
		remaining = remaining * min(100, max(0, 100-victim.SegmentPercent)) / 100
	}
	absorbed := 0
	if victim.OneShotShield {
		victim.OneShotShield = false
		absorbed = remaining
		remaining = 0
	}
	if victim.ConsumableShield > 0 {
		take := min(remaining, victim.ConsumableShield)
		victim.ConsumableShield -= take
		remaining -= take
		absorbed += take
	}
	applied := min(victim.HP, remaining)
	victim.HP -= applied
	victim.DamageTaken += applied
	if applied+absorbed > 0 {
		victim.RevealedUntil = w.TickNum + 40
		victim.DamageDeltas = append(victim.DamageDeltas, applied+absorbed)
	}
	if attacker != nil && attacker != victim && applied > 0 {
		attacker.DamageDealt += applied
		owner := attacker
		if attacker.Owner != nil {
			owner = attacker.Owner
			owner.DamageDealt += applied
		}
		mult := owner.UltChargeMult
		if ultimate {
			mult = owner.UltUltMult
		}
		if !owner.Definition.Exists() && mult <= 0 {
			mult = 100
		}
		owner.UltCharge = min(4000, owner.UltCharge+applied*mult/100)
		if procs {
			onDamageEffects(w, owner, victim, applied)
			onAugmentDamage(w, owner, victim, applied)
		}
	}
	if victim.HP <= 0 {
		if ultimate && attacker != nil && victim.Owner == nil && victim.HeroType == "hero" {
			owner := attacker
			if owner.Owner != nil {
				owner = owner.Owner
			}
			owner.KillsWithUltimate++
		}
		Kill(w, attacker, victim)
	}
}
func Kill(w *World, killer, victim *Player) {
	if !victim.Alive {
		return
	}
	victim.Alive = false
	victim.DeathTick = w.TickNum
	victim.HP = 0
	victim.Deaths++
	onAugmentDeath(w, killer, victim)
	CancelCast(victim)
	victim.Effects = nil
	victim.ControlRemoved = false
	victim.Slowdown = false
	victim.EffectsSpeedAdd = 0
	victim.AccessoryState = 0
	victim.ConsumableShield = 0
	victim.SegmentTicks = 0
	victim.HasMoveTarget = false
	victim.State = StateRespawn
	victim.RespawnLeft = w.RespawnTicks
	if w.ModeName == "mega_boss" && victim.Definition.String("name") == "karity_boss_character" {
		w.Finished = true
		w.WinnerTeam = 0
		w.WinReason = "boss_killed"
	}
	if victim.CarryableObjectID != 0 {
		DropBall(w, victim)
	}
	if killer != nil && killer.Owner != nil {
		killer = killer.Owner
	}
	if killer != nil && killer != victim && victim.Owner == nil && victim.HeroType == "hero" {
		killer.Kills++
		points := 1
		if w.IsBounty {
			points = max(1, victim.GamePoints)
			killer.GamePoints = min(7, max(2, killer.GamePoints)+1)
			victim.GamePoints = 2
		}
		if !w.HasCarryable {
			w.AddPoint(killer.Team, points)
		}
		killer.PendingKills = append(killer.PendingKills, KillEvent{VictimIndex: victim.Index, Points: points, Tick: w.TickNum})
	}
	if victim.Owner != nil && victim.Upgrade.String("name") == "override_minion_death_area_effect" {
		SpawnArea(w, victim.Owner, victim.Upgrade.String("area_effect_name"), victim.X, victim.Y, 0, 0, false, false)
	}
}
