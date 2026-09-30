package sim

import (
	"github.com/freaks/openfreaks/internal/protocol"
)

// Snapshot encoding — bit-exact port of SnapshotEncoder.cs field order:
// own identity, tile rect, mode block, player dash/kill states, map tiles,
// visible ids (characters, projectiles, areas, items), then payloads.

const (
	TypeCharacter   = 0
	TypeProjectile  = 1
	TypeAreaEffect  = 2
	TypeItem        = 3
	hitPointsThresh = 64999
)

func MakeMagicIndex(maxPlayers, team, index int) int {
	tn := team
	if tn < 0 {
		tn = maxPlayers
	}
	in := index
	if in < 0 {
		in = maxPlayers
	}
	return tn*16 + in
}

func EncodeSnapshot(w *World, recipientIdx int, participant int) []byte {
	return encodeSnapshotWith(w, recipientIdx, participant, protocol.NewBitWriter(8192))
}

func EncodeSnapshotInto(w *World, recipientIdx int, participant int, bw *protocol.BitWriter) []byte {
	bw.Reset()
	return encodeSnapshotWith(w, recipientIdx, participant, bw)
}

func encodeSnapshotWith(w *World, recipientIdx int, participant int, bw *protocol.BitWriter) []byte {
	ownIdx := recipientIdx
	isAlive := false
	var observer *Player
	for _, p := range w.Players {
		if p.Index == recipientIdx {
			isAlive = p.Alive
			ownIdx = p.ObjectIndex
			if participant == protocol.ParticipantPlayer {
				observer = p
			}
			break
		}
	}
	bw.WritePositiveVIntMax65535(ownIdx)
	if participant == protocol.ParticipantPlayer {
		bw.WritePositiveIntMax15(recipientIdx)
	} else {
		bw.WritePositiveIntMax15(0)
	}
	bw.WriteBool(isAlive)
	writeTileRect(bw, w)

	if w.HasCarryable {
		bw.WriteBool(w.RoundResetTicks > 0)
		bw.WriteIntMax1(w.BallWinnerTeam)
		if isFootballLocation(w.LocationName) {
			bw.WritePositiveIntMax1023(w.GetPoint(0))
			bw.WritePositiveIntMax1023(w.GetPoint(1))
			bw.WriteBool(w.BallOvertime)
		}
	} else if w.IsShowdown {
		teams := map[int]bool{}
		for _, p := range w.Players {
			if p.Alive && !p.Abandoned && !p.IsBall() {
				teams[p.Team] = true
			}
		}
		n := len(teams)
		if n > 15 {
			n = 15
		}
		bw.WritePositiveIntMax15(n)
	} else {
		for t := 0; t < w.TeamCount; t++ {
			v := w.GetPoint(t)
			if v < 0 {
				v = 0
			}
			if v > 1023 {
				v = 1023
			}
			bw.WritePositiveIntMax1023(v)
		}
	}
	if w.ModeName == "mega_boss" {
		// Мегабосс Карити: фаза (0 шар, 1 переход, 2 титан) и щит эмодзи (karity.go).
		bw.WritePositiveIntMax3(w.BossPhase)
		bw.WritePositiveIntMax1023(w.EmojiShieldLeft)
		bw.WritePositiveIntMax1023(w.EmojiShieldTotal)
	}
	// Режимы 26.09: личный блок режима (у каждого получателя свой).
	switch {
	case w.modes.nightmare != nil:
		encodeNightmareBlock(bw, w, observer)
	case w.modes.infection != nil:
		encodeInfectionBlock(bw, w, observer)
	case w.modes.knockout != nil:
		encodeKnockoutBlock(bw, w, observer)
	case w.modes.karitGrab != nil:
		encodeKaritGrabBlock(bw, w)
	}
	if w.DPSEnabled {
		dps := 0
		if participant == protocol.ParticipantPlayer {
			for _, p := range w.Players {
				if p.Index == recipientIdx {
					dps = w.DamagePerSecond(p)
					break
				}
			}
		}
		bw.WritePositiveIntMax131072(min(131071, dps))
	}

	bw.WritePositiveIntMax31(len(w.Players))
	for _, p := range w.Players {
		encodeDash(bw, p, participant == protocol.ParticipantPlayer && p.Index == recipientIdx)
		bw.WriteBool(len(p.PendingKills) > 0)
		if len(p.PendingKills) > 0 {
			n := min(15, len(p.PendingKills))
			bw.WritePositiveIntMax15(n)
			for _, k := range p.PendingKills[:n] {
				bw.WriteIntMax15(k.VictimIndex)
				stars := 0
				if w.IsBounty {
					stars = k.Points
				}
				bw.WriteIntMax7(stars)
			}
		}
	}

	encodeMapTiles(bw, w)

	chars := w.snapChars[:0]
	for _, p := range w.Characters() {
		if p.Alive && !p.Abandoned {
			chars = append(chars, p)
		}
	}
	w.snapChars = chars
	projs := w.snapProjs[:0]
	for _, pr := range w.Projectiles {
		if pr.Alive || pr.TerminalTicks > 0 {
			projs = append(projs, pr)
		}
	}
	w.snapProjs = projs
	areas := w.snapAreas[:0]
	for _, a := range w.Areas {
		if w.modes.nightmare == nil || w.nightmareAreaVisible(observer, a) {
			areas = append(areas, a)
		}
	}
	w.snapAreas = areas
	bw.WritePositiveVIntMax65535(len(chars) + len(projs) + len(areas) + len(w.Items))
	for _, c := range chars {
		bw.WritePositiveIntMax31(TypeCharacter)
		bw.WritePositiveVIntMax65535(c.ObjectIndex)
	}
	for _, pr := range projs {
		bw.WritePositiveIntMax31(TypeProjectile)
		bw.WritePositiveVIntMax65535(pr.LocalIndex)
	}
	for _, a := range areas {
		bw.WritePositiveIntMax31(TypeAreaEffect)
		bw.WritePositiveVIntMax65535(a.LocalIndex)
	}
	for _, i := range w.Items {
		bw.WritePositiveIntMax31(TypeItem)
		bw.WritePositiveVIntMax65535(i.LocalIndex)
	}

	ownCharID := -1
	if participant == protocol.ParticipantPlayer {
		ownCharID = (TypeCharacter+1)*1000000 + ownIdx
	}
	for _, c := range chars {
		bw.WritePositiveIntMax511(int(c.ModelID))
		bw.WritePositiveVIntMax131072(MakeMagicIndex(w.MaxPlayers, c.Team, c.Index))
		visibility := c.Visibility
		var hidden *hiddenView
		if observer != nil {
			if w.modes.nightmare != nil {
				// Кошмар: 15 — видно целиком, 1 — только луч фонаря, 2 — «пульс»
				// бегущего для убийцы. Для луча и пульса нужна настоящая точка.
				visibility = w.nightmareVisibility(observer, c)
				if visibility == 15 {
					w.rememberSeen(recipientIdx, c)
				} else if visibility == 0 {
					hidden = w.hiddenViewFor(recipientIdx, c)
				}
			} else if w.CanSee(observer, c) {
				w.rememberSeen(recipientIdx, c)
			} else {
				visibility = 0
				hidden = w.hiddenViewFor(recipientIdx, c)
			}
		}
		// В Кошмаре чужие навыки и заряд — это роль: их не шлём никому.
		secret := w.modes.nightmare != nil && c != observer && !w.Finished
		encodeCharacterMode(bw, w, c, ownCharID, visibility, hidden, secret)
	}
	for _, pr := range projs {
		bw.WritePositiveIntMax511(int(pr.ModelID))
		ri := pr.RosterIndex
		if ri < 0 {
			if pr.Owner != nil && pr.Owner.Index < w.MaxPlayers {
				ri = pr.Owner.Index
			} else {
				ri = -1
			}
		}
		bw.WritePositiveVIntMax131072(MakeMagicIndex(w.MaxPlayers, pr.Team, ri))
		encodeProjectile(bw, w, pr)
	}
	for _, a := range areas {
		bw.WritePositiveIntMax511(a.Definition.ID)
		bw.WritePositiveVIntMax131072(MakeMagicIndex(w.MaxPlayers, a.Team, a.RosterIndex))
		bw.WritePositiveVIntMax65535(clampPos(a.X))
		bw.WritePositiveVIntMax65535(clampPos(a.Y))
		bw.WritePositiveVIntMax65535(0)
		bw.WritePositiveIntMax15(15)
		bw.WritePositiveIntMax255(clamp255((a.TimeMsLeft + 49) / 50))
		if a.Definition.Bool("is_frame_controlled_by_server") {
			bw.WritePositiveIntMax127(0)
		}
		bw.WriteBool(a.Following)
		if a.Following {
			bw.WritePositiveVIntMax65535(a.OwnerIndex)
		}
	}
	for _, i := range w.Items {
		bw.WritePositiveIntMax511(i.Definition.ID)
		bw.WritePositiveVIntMax131072(MakeMagicIndex(w.MaxPlayers, i.Team, i.RosterIndex))
		bw.WritePositiveVIntMax65535(clampPos(i.X))
		bw.WritePositiveVIntMax65535(clampPos(i.Y))
		bw.WritePositiveVIntMax65535(0)
		bw.WritePositiveIntMax15(15)
		if i.Definition.String("name") == "portal" {
			bw.WriteBool(true)
			bw.WritePositiveIntMax3(i.State)
			bw.WritePositiveIntMax127(min(127, max(0, i.StateTicks)))
		}
	}
	return bw.Bytes()
}

func writeTileRect(bw *protocol.BitWriter, w *World) {
	toX := w.Tiles.W - 1
	toY := w.Tiles.H - 1
	if toX < 0 {
		toX = 0
	}
	if toY < 0 {
		toY = 0
	}
	if w.IsBigMap {
		bw.WritePositiveIntMax255(0)
		bw.WritePositiveIntMax255(0)
		bw.WritePositiveIntMax255(toX)
		bw.WritePositiveIntMax255(toY)
	} else {
		bw.WritePositiveIntMax31(0)
		bw.WritePositiveIntMax63(0)
		bw.WritePositiveIntMax31(toX)
		bw.WritePositiveIntMax63(toY)
	}
}

func encodeDash(bw *protocol.BitWriter, p *Player, isOwn bool) {
	if p.DashModelID < 0 {
		return
	}
	activating := p.AccessoryState == 1
	if isOwn {
		if !p.DashUnlimited {
			bw.WritePositiveIntMax7(p.DashChargeLeft)
		}
		bw.WritePositiveVIntMax255OftenZero(p.AccessoryState)
		bw.WritePositiveVIntMax255OftenZero(p.DashCooldown)
		if activating && p.DashDefinition.At("type").String("name") == "dash_low_hit_points" {
			n := min(15, len(p.ScentSources))
			bw.WritePositiveIntMax15(n)
			for _, source := range p.ScentSources[:n] {
				bw.WritePositiveVIntMax65535(source[0])
				bw.WritePositiveVIntMax65535(source[1])
			}
		}
	} else {
		bw.WriteBool(activating)
	}
	if activating {
		bw.WritePositiveIntMax1023(p.ActingTicks)
	}
}

func encodeMapTiles(bw *protocol.BitWriter, w *World) {
	// Динамические плитки: опущенные решётки Кошмара (sim/nightmare.go).
	gates := w.activeGateTiles()
	bw.WritePositiveVIntMax65535OftenZero(len(gates))
	for _, i := range gates {
		bw.WritePositiveIntMax4095(min(4095, max(0, i)))
		bw.WritePositiveIntMax7(gateDynamicCode)
	}
	for _, i := range w.Tiles.DestructibleOrder() {
		bw.WriteBool(w.Tiles.Destroyed[i])
	}
}

func encodeCharacter(bw *protocol.BitWriter, w *World, p *Player, ownCharID, visibility int, hidden *hiddenView) {
	encodeCharacterMode(bw, w, p, ownCharID, visibility, hidden, false)
}

func isFootballLocation(name string) bool {
	return name == "experimental_football" || name == "experimental_football_3v3"
}

func encodeCharacterMode(bw *protocol.BitWriter, w *World, p *Player, ownCharID, visibility int, hidden *hiddenView, secret bool) {
	custom := p.HeroType == "minion_mirage" || p.WireHero
	bw.WriteBool(custom)
	if custom {
		// Персонаж режима — для клиента миньон, но в бою он герой (HeroType.Hero = 0).
		if p.WireHero {
			bw.WritePositiveIntMax31(0)
		} else {
			bw.WritePositiveIntMax31(8)
		}
	}
	x, y, z := p.X, p.Y, p.Z
	if hidden != nil {
		x, y, z = hidden.X, hidden.Y, hidden.Z
	}
	if p.HeroType == "minion_find_teammates" {
		bw.WriteIntMax65535(int(x))
		bw.WriteIntMax65535(int(y))
	} else {
		bw.WritePositiveVIntMax65535(clampPos(x))
		bw.WritePositiveVIntMax65535(clampPos(y))
	}
	bw.WritePositiveVIntMax65535(clampPos(z))
	bw.WritePositiveIntMax15(visibility)
	encodeReplication(bw, p, w.HasCarryable, hidden)
	for _, sk := range p.Skills {
		if secret {
			if sk.MaxCharge >= 1 {
				bw.WritePositiveIntMax8191(min(8191, sk.MaxCharge*1000))
			}
			bw.WritePositiveVIntMax255OftenZero(0)
			bw.WritePositiveVIntMax255OftenZero(0)
			bw.WriteBool(false)
			continue
		}
		if sk.MaxCharge >= 1 {
			bw.WritePositiveIntMax8191(sk.WireCharge())
		}
		// Поле вмещает 255 × 50 мс. Навык с перезарядкой длиннее (заглушки
		// миньонов Карити) иначе ронял весь бой паникой на записи снимка.
		bw.WritePositiveVIntMax255OftenZero(min(255, max(0, sk.ActiveLeft/50)))
		bw.WritePositiveVIntMax255OftenZero(min(255, max(0, sk.CoolLeft/50)))
		bw.WriteBool(sk.CastPending)
	}

	n := 0
	if hidden == nil && (w.modes.nightmare == nil || visibility == 15) {
		n = min(31, len(p.DamageDeltas))
	}
	bw.WritePositiveIntMax31(n)
	for _, delta := range p.DamageDeltas[:n] {
		bw.WriteIntMax32767(delta)
	}
	_ = ownCharID
}

func encodeProjectile(bw *protocol.BitWriter, w *World, pr *Projectile) {
	if pr.Circling {
		bw.WriteIntMax65535(int(pr.X))
		bw.WriteIntMax65535(int(pr.Y))
	} else {
		bw.WritePositiveVIntMax65535(clampPos(pr.X))
		bw.WritePositiveVIntMax65535(clampPos(pr.Y))
	}
	bw.WritePositiveVIntMax65535(clampPos(pr.Z))
	st := ProjFlying
	if !pr.Alive {
		st = pr.State
		if st == ProjFlying {
			st = ProjRangeEnd
		}
	}
	bw.WritePositiveIntMax7(st)
	pierced := st == ProjEnvImpactDestruct
	if !pierced && pr.Bouncing {
		bw.WriteBool(pr.TilePierced)
		pierced = pr.TilePierced
	}
	if pierced {
		if w.IsBigMap {
			bw.WritePositiveIntMax32767(pr.PiercedTileIndex)
		} else {
			bw.WritePositiveIntMax1023(pr.PiercedTileIndex)
		}
	}
	prog := 0
	if pr.RangePx > 0 {
		prog = int(pr.Traveled / pr.RangePx * 1023)
		if prog < 0 {
			prog = 0
		}
		if prog > 1023 {
			prog = 1023
		}
	}
	// Number projectiles reuse the unused visual progress field without changing packet layout.
	if pr.Definition.String("name") == "rabbit_tv_number_projectile" {
		prog = min(1023, max(0, pr.Damage))
	}
	bw.WritePositiveIntMax1023(prog)
	ang := pr.Angle % 360
	if ang < 0 {
		ang += 360
	}
	bw.WritePositiveIntMax511(ang)
	bw.WriteBool(pr.Returning)
	early := pr.EarlyTicks > 0
	bw.WriteBool(early)
	if early {
		bw.WritePositiveVIntMax65535(clampPos(pr.X))
		bw.WritePositiveVIntMax65535(clampPos(pr.Y))
	}
}

func cell(bw *protocol.BitWriter, fn func()) {
	bw.WriteBool(true)
	fn()
}

func encodeReplication(bw *protocol.BitWriter, p *Player, hasCarryable bool, hidden *hiddenView) {
	hero := p.HeroType == "hero" || p.HeroType == ""
	mirage := p.HeroType == "minion_mirage"
	carryable := p.IsBall()

	bw.WriteBool(true)
	cell(bw, func() { bw.WritePositiveIntMax7(p.State) })
	cell(bw, func() { bw.WriteBool(p.AngleForced) })
	cell(bw, func() { bw.WritePositiveIntMax511(p.AttackAngle) })
	cell(bw, func() { bw.WritePositiveIntMax511(p.MoveAngle) })
	writeHP := func(hp int) {
		if p.Definition.Int("hit_points") > hitPointsThresh {
			bw.WritePositiveIntMax524287(min(524287, max(0, hp)))
		} else {
			bw.WritePositiveVIntMax65535(clampHP(hp))
		}
	}
	hp := p.HP
	if hidden != nil {
		hp = hidden.HP
	}
	cell(bw, func() { writeHP(hp) })
	cell(bw, func() { writeHP(p.MaxHP) })
	if hero {
		cell(bw, func() { bw.WritePositiveVIntMax255OftenZero(min(255, max(0, p.SkillCastLeft/50))) })
		cell(bw, func() {
			v := p.UltCharge
			if v < 0 {
				v = 0
			}
			if v > 4095 {
				v = 4095
			}
			bw.WritePositiveIntMax4095(v)
		})
		cell(bw, func() { bw.WriteBool(p.UltActive) })
		cell(bw, func() { bw.WriteBool(p.UltUsed) })
		if hasCarryable {
			cell(bw, func() { bw.WritePositiveVIntMax65535OftenZero(min(65535, max(0, p.CarryableAimX))) })
			cell(bw, func() { bw.WritePositiveVIntMax65535OftenZero(min(65535, max(0, p.CarryableAimY))) })
			cell(bw, func() { bw.WritePositiveVIntMax65535(p.CarryableObjectID) })
		}
	}
	if carryable {
		cell(bw, func() { bw.WritePositiveIntMax3(p.LastThrowType) })

		cell(bw, func() { bw.WritePositiveIntMax3(min(3, max(0, p.LastThrowTeam))) })
	}

	if hero {
		bw.WriteBool(true)
		cell(bw, func() { bw.WriteBool(p.SpawnShield) })
		cell(bw, func() { bw.WriteBool(p.OneShotShield) })
		cell(bw, func() { bw.WriteBool(p.ShieldBuff) })
		cell(bw, func() { bw.WritePositiveIntMax255(clamp255(p.ConsumableTicks)) })
		cell(bw, func() { bw.WritePositiveIntMax255(clamp255(p.SegmentTicks)) })
		cell(bw, func() { bw.WritePositiveIntMax3(p.SegmentLevel) })
		cell(bw, func() {
			if p.Definition.Int("hit_points") > hitPointsThresh {
				bw.WritePositiveIntMax2097152OftenZero(min(2097151, max(0, p.ConsumableShield)))
			} else {
				bw.WritePositiveVIntMax65535OftenZero(clampPos(float64(p.ConsumableShield)))
			}
		})
		cell(bw, func() { bw.WriteBool(p.Spot) })
	} else {
		bw.WriteBool(false)
	}

	if hero {
		bw.WriteBool(true)
		cell(bw, func() { bw.WriteBool(p.Charged) })
		cell(bw, func() { bw.WritePositiveIntMax255(clamp255(p.ChargeMsLeft / 50)) })
		cell(bw, func() {
			v := p.ChargeSpeed
			if v < 0 {
				v = 0
			}
			if v > 16383 {
				v = 16383
			}
			bw.WritePositiveIntMax16383(v)
		})
		cell(bw, func() { bw.WritePositiveIntMax255(clamp255(p.ChargeTotalMs / 50)) })
		cell(bw, func() { bw.WritePositiveIntMax15(p.ChargeType) })
	} else {
		bw.WriteBool(false)
	}

	bw.WriteBool(true)
	cell(bw, func() { bw.WriteBool(p.ControlRemoved) })
	cell(bw, func() { bw.WriteBool(p.Teleporting) })
	if hero || mirage {
		cell(bw, func() { bw.WritePositiveVIntMax255OftenZero(p.PowerPoints) })
		cell(bw, func() { bw.WritePositiveVIntMax255OftenZero(p.GamePoints) })
		cell(bw, func() { bw.WriteBool(p.BlueStar) })
	}
	cell(bw, func() { bw.WriteBool(p.FirePoison) })
	if hero {
		cell(bw, func() { bw.WriteBool(p.Slowdown) })
		cell(bw, func() { bw.WriteIntMax1023(p.SpeedAdd) })
		cell(bw, func() { bw.WriteIntMax1023(p.EffectsSpeedAdd) })
		cell(bw, func() { bw.WriteBool(p.Electrocuted) })
	}
	bw.WriteBool(false)
}

func clampHP(v int) int {
	if v < 0 {
		return 0
	}
	if v > MaxWirePosition {
		return MaxWirePosition
	}
	return v
}

func clamp255(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
