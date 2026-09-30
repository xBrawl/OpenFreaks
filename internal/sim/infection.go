package sim

import "github.com/freaks/openfreaks/internal/protocol"

// ЗАРАЖЕНИЕ. Десять выживших в одной команде (0). После 15 секунд карантина
// один становится «Пациентом Зеро» (команда 1). Убитый выживший через 3
// секунды встаёт заражённым. Остался один — «Последний рубеж»: 30 секунд,
// усиления и метка для всей орды. Дожил хоть кто-то — победа выживших.

const (
	infZombieName = "infection_zombie_character"
	infAlphaName  = "infection_alpha_character"

	infQuarantineTicks = 300 // 15 секунд без стрельбы
	infLastStandTicks  = 600 // 30 секунд последнего рубежа
	infCrateEvery      = 600 // ящик раз в 30 секунд
	infCrateMax        = 3
	infCrateReach      = 250.0
	infFearTicks       = 80 // 4 секунды на месте — запах страха
	infReloadPenalty   = 15
	infLastStandHP     = 5000
	infPushWindow      = 80 // «истощение контроля»: два толчка за 4 секунды
	infPushLimit       = 2
	infMutationShield  = 20
)

const (
	infPhaseQuarantine = 0
	infPhaseHunt       = 1
	infPhaseLastStand  = 2
	infPhaseOver       = 3
)

type infPlayer struct {
	zombie     bool
	pending    bool // погиб выжившим — встанет заражённым
	infections int
	lastMove   int32
	prevX      float64
	prevY      float64
	fear       *Area
	pushes     []int32
	deathOrder int
}

type infectionState struct {
	players       map[int]*infPlayer
	phase         int
	quarantineEnd int32
	alpha         int
	lastStand     int
	lastStandEnd  int32
	nextCrate     int32
	crates        []*Area
	deaths        int
	lastStandRef  int
}

func (w *World) inf() *infectionState { return w.modes.infection }

func (s *infectionState) of(p *Player) *infPlayer {
	if p == nil {
		return nil
	}
	return s.players[p.Index]
}

func setupInfection(w *World) {
	s := &infectionState{players: map[int]*infPlayer{}, alpha: -1, lastStand: -1, lastStandRef: -1}
	w.modes.infection = s
	s.quarantineEnd = int32(w.IntroTicks + infQuarantineTicks)
	s.nextCrate = s.quarantineEnd + infCrateEvery/2
	// Все начинают выжившими в одной команде, разбросанными по карте.
	placed := []*Player{}
	for _, p := range w.Players {
		p.Team = 0
		x, y := randomOpenPoint(w, p.CollisionRadius, placed, 1800)
		p.X, p.Y, p.ExactX, p.ExactY = x, y, x, y
		p.SpawnX, p.SpawnY = x, y
		placed = append(placed, p)
		s.players[p.Index] = &infPlayer{prevX: x, prevY: y}
	}
}

func infectionCanCast(w *World, p *Player) bool {
	s := w.inf()
	return s.phase != infPhaseQuarantine && s.phase != infPhaseOver
}

// infectionDashAllowed: у выживших гаджет один на матч, у заражённых его нет.
func infectionDashAllowed(w *World, p *Player) bool {
	s := w.inf()
	st := s.of(p)
	if st == nil || st.zombie || s.phase == infPhaseQuarantine {
		return false
	}
	return p.DashUses < 1
}

func (s *infectionState) counts(w *World) (survivors, zombies int, last *Player) {
	for _, p := range w.Players {
		st := s.of(p)
		if st == nil || p.Abandoned {
			continue
		}
		if st.zombie || st.pending {
			zombies++
			continue
		}
		if p.Alive {
			survivors++
			last = p
		}
	}
	return
}

func tickInfection(w *World) {
	s := w.inf()
	if w.Finished || int(w.TickNum) < w.IntroTicks || s.phase == infPhaseOver {
		return
	}
	if s.phase == infPhaseQuarantine {
		if w.TickNum >= s.quarantineEnd {
			infectionPickAlpha(w)
		}
		infectionTrackMoves(w)
		return
	}

	// Ящики с припасами.
	kept := s.crates[:0]
	for _, c := range s.crates {
		if c != nil && c.TimeMsLeft > 0 {
			keepProp(c)
			kept = append(kept, c)
		}
	}
	s.crates = kept
	if w.TickNum >= s.nextCrate {
		s.nextCrate = w.TickNum + infCrateEvery
		for i := 0; i < 2 && len(s.crates) < infCrateMax; i++ {
			x, y := randomOpenPoint(w, 150, livePlayers(w), 1200)
			if c := spawnProp(w, "infection_crate", x, y); c != nil {
				s.crates = append(s.crates, c)
			}
		}
	}
	for _, p := range w.Players {
		st := s.of(p)
		if st == nil || st.zombie || !p.Alive || p.Abandoned {
			continue
		}
		for _, c := range s.crates {
			if c.TimeMsLeft > 0 && Dist(p.X, p.Y, c.X, c.Y) <= infCrateReach {
				infectionCrateReward(w, p)
				removeProp(c)
			}
		}
	}

	infectionTrackMoves(w)

	survivors, _, last := s.counts(w)
	if survivors == 0 {
		infectionFinish(w, 1, "infected")
		return
	}
	if s.phase == infPhaseHunt && survivors == 1 && last != nil {
		infectionLastStand(w, last)
	}
}

// infectionTrackMoves: кто засиделся — пахнет страхом, орда видит его сквозь стены.
func infectionTrackMoves(w *World) {
	s := w.inf()
	for _, p := range w.Players {
		st := s.of(p)
		if st == nil {
			continue
		}
		if !p.Alive || st.zombie {
			removeProp(st.fear)
			st.fear = nil
			st.lastMove = w.TickNum
			continue
		}
		if Dist(p.X, p.Y, st.prevX, st.prevY) > 60 {
			st.lastMove = w.TickNum
			st.prevX, st.prevY = p.X, p.Y
		}
		scared := s.phase == infPhaseHunt && w.TickNum-st.lastMove >= infFearTicks
		if scared {
			p.RevealedUntil = max(p.RevealedUntil, w.TickNum+2)
			if st.fear == nil || st.fear.TimeMsLeft <= 0 {
				st.fear = SpawnArea(w, p, "infection_fear", p.X, p.Y, 0, 0, true, false)
			}
			keepProp(st.fear)
		} else if st.fear != nil {
			removeProp(st.fear)
			st.fear = nil
		}
	}
}

func infectionCrateReward(w *World, p *Player) {
	switch w.modes.rng.Intn(3) {
	case 0:
		Heal(p, p.MaxHP)
	case 1:
		GiveShield(p, p.MaxHP*30/100, 200)
	default:
		p.UltCharge = 4000
	}
	SpawnArea(w, p, "infection_mutation", p.X, p.Y, 0, 0, false, false)
}

func infectionPickAlpha(w *World) {
	s := w.inf()
	candidates := livePlayers(w)
	s.phase = infPhaseHunt
	if len(candidates) == 0 {
		return
	}
	p := candidates[w.modes.rng.Intn(len(candidates))]
	s.alpha = p.Index
	// Мутация на месте: у персонажа новый облик и новый номер объекта,
	// клиент просто получает другое существо в той же точке.
	infectionTurn(w, p, infAlphaName)
	w.addHighlight(HighlightPatientZero, p, 1, 5)
	p.SpawnShield = true
	p.SpawnShieldTicks = infMutationShield
}

func infectionTurn(w *World, p *Player, name string) {
	s := w.inf()
	st := s.of(p)
	if st == nil {
		return
	}
	swapCharacter(w, p, name)
	p.Team = 1
	// Новый номер объекта: клиент держит облик героя по номеру и иначе
	// оставил бы заражённому старую модель и старую команду.
	if id := w.AllocCharacterIndex(); id != 0 {
		p.ObjectIndex = id
	}
	st.zombie = true
	st.pending = false
	removeProp(st.fear)
	st.fear = nil
	SpawnArea(w, p, "infection_mutation", p.X, p.Y, 0, 0, false, false)
}

func infectionLastStand(w *World, p *Player) {
	s := w.inf()
	s.phase = infPhaseLastStand
	s.lastStand = p.Index
	s.lastStandEnd = w.TickNum + infLastStandTicks
	// Таймер матча сбрасывается до 30 секунд.
	w.DurationTicks = int(s.lastStandEnd) - w.IntroTicks
	p.MaxHP += infLastStandHP
	p.HP = p.MaxHP
	p.UltCharge = 4000
	s.lastStandRef = w.addHighlight(HighlightLastStand, p, 0, 15)
}

// infectionModifiers — поправки после TickEffects (он обнуляет эффекты).
func infectionModifiers(w *World) {
	s := w.inf()
	for _, p := range w.Players {
		st := s.of(p)
		if st == nil || !p.Alive || st.zombie {
			continue
		}
		if s.phase == infPhaseLastStand && p.Index == s.lastStand {
			p.ReloadAddPct += 100
			p.EffectsSpeedAdd = min(500, p.EffectsSpeedAdd+15)
			p.UltCharge = 4000
			p.RevealedUntil = max(p.RevealedUntil, w.TickNum+2)
			continue
		}
		p.ReloadAddPct -= infReloadPenalty
	}
}

// infectionPushAllowed — «истощение контроля»: заражённого, которого
// оттолкнули дважды за 4 секунды, третий толчок не двигает.
func infectionPushAllowed(w *World, p *Player) bool {
	s := w.modes.infection
	if s == nil {
		return true
	}
	st := s.of(p)
	if st == nil || !st.zombie {
		return true
	}
	kept := st.pushes[:0]
	for _, t := range st.pushes {
		if w.TickNum-t < infPushWindow {
			kept = append(kept, t)
		}
	}
	st.pushes = kept
	if len(st.pushes) >= infPushLimit {
		return false
	}
	st.pushes = append(st.pushes, w.TickNum)
	return true
}

func infectionOnKill(w *World, killer, victim *Player) {
	s := w.inf()
	st := s.of(victim)
	if st == nil {
		return
	}
	removeProp(st.fear)
	st.fear = nil
	if !st.zombie {
		st.pending = true
		s.deaths++
		st.deathOrder = s.deaths
		if killer != nil {
			if k := s.of(killer); k != nil && k.zombie {
				k.infections++
			}
		}
	}
	victim.RespawnLeft = max(1, w.RespawnTicks)
}

func infectionOnRespawn(w *World, p *Player) {
	s := w.inf()
	st := s.of(p)
	if st == nil {
		return
	}
	if st.pending {
		infectionTurn(w, p, infZombieName)
	}
	// Встаёт подальше от выживших, чтобы не появиться у них за спиной.
	survivors := []*Player{}
	for _, q := range w.Players {
		if qs := s.of(q); qs != nil && !qs.zombie && !qs.pending && q.Alive {
			survivors = append(survivors, q)
		}
	}
	x, y := randomOpenPoint(w, p.CollisionRadius, survivors, 2400)
	p.X, p.Y, p.ExactX, p.ExactY = x, y, x, y
	p.HasMoveTarget = false
}

func infectionFinishByTime(w *World) {
	survivors, _, _ := w.inf().counts(w)
	if survivors > 0 {
		infectionFinish(w, 0, "survived")
	} else {
		infectionFinish(w, 1, "infected")
	}
}

// infectionFinish — итоги. Победа выживших: живые и трое погибших последними
// (частичная победа). Победа орды: Пациент Зеро и все, кто кого-то заразил.
func infectionFinish(w *World, side int, reason string) {
	if w.Finished {
		return
	}
	s := w.inf()
	s.phase = infPhaseOver
	w.Finished = true
	w.WinReason = reason
	w.WinnerTeam = side
	lastThree := s.deaths - 2
	if h := w.highlights; side == 0 && h != nil && s.lastStandRef >= 0 && s.lastStandRef < len(h.list) &&
		h.list[s.lastStandRef].Kind == HighlightLastStand && s.lastStand >= 0 && w.Players[s.lastStand].Alive {
		// Продержался до конца — лучший момент матча.
		h.list[s.lastStandRef].Count, h.list[s.lastStandRef].Score = 1, 35
	}
	for _, p := range w.Players {
		st := s.of(p)
		if st == nil {
			continue
		}
		switch {
		case p.Index == s.alpha:
			w.setRole(p, RoleAlpha)
		case st.zombie || st.pending:
			w.setRole(p, RoleZombie)
		default:
			w.setRole(p, RoleSurvivor)
		}
		win := false
		if side == 0 {
			win = !st.zombie && !st.pending && p.Alive ||
				(st.deathOrder > 0 && st.deathOrder >= lastThree && p.Index != s.alpha)
		} else {
			win = p.Index == s.alpha || st.infections > 0
		}
		if win && !p.Abandoned {
			w.setRank(p, 1)
		} else {
			w.setRank(p, 2)
		}
	}
}

func encodeInfectionBlock(bw *protocol.BitWriter, w *World, o *Player) {
	s := w.inf()
	survivors, zombies, _ := s.counts(w)
	left := 0
	switch s.phase {
	case infPhaseQuarantine:
		left = int(s.quarantineEnd - w.TickNum)
	case infPhaseLastStand:
		left = int(s.lastStandEnd - w.TickNum)
	default:
		left = w.TotalTicks() - int(w.TickNum)
	}
	bw.WritePositiveIntMax3(s.phase)
	bw.WritePositiveVIntMax65535(min(65535, max(0, left)))
	bw.WritePositiveIntMax15(min(15, survivors))
	bw.WritePositiveIntMax15(min(15, zombies))
	alpha, last := 15, 15
	if s.alpha >= 0 {
		alpha = min(14, s.alpha)
	}
	if s.phase == infPhaseLastStand && s.lastStand >= 0 {
		last = min(14, s.lastStand)
	}
	bw.WritePositiveIntMax15(alpha)
	bw.WritePositiveIntMax15(last)
	own := s.of(o)
	bw.WriteBool(own != nil && (own.zombie || own.pending))
	bw.WriteBool(o != nil && o.DashUses > 0)
	bw.WriteBool(own != nil && own.fear != nil)
	bw.WritePositiveIntMax15(min(15, boolInt(own != nil)*infOwnInfections(own)))
}

func infOwnInfections(st *infPlayer) int {
	if st == nil {
		return 0
	}
	return st.infections
}

// InfectionHears — голос по сторонам: выжившие слышат выживших, орда — орду.
func (w *World) InfectionHears(listener, speaker *Player) bool {
	s := w.modes.infection
	if s == nil || listener == nil || speaker == nil {
		return false
	}
	a, b := s.of(listener), s.of(speaker)
	if a == nil || b == nil {
		return false
	}
	return (a.zombie || a.pending) == (b.zombie || b.pending)
}
