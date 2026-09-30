package sim

import "sort"

// ЛУЧШИЕ МОМЕНТЫ. Бой отмечает яркие события — серию убийств, гол, удар
// убийцы, поимку убийцы, последний рубеж. Главный сервер кладёт шесть лучших
// в историю боёв, а лог боёв показывает их кнопками: реплей открывается и
// перематывается на несколько секунд до момента.

const (
	HighlightMultiKill     = 1 // Count — убийств подряд
	HighlightGoal          = 2
	HighlightNightmareKill = 3 // Count — какой по счёту удар убийцы
	HighlightKillerDown    = 4
	HighlightLightsOn      = 5
	HighlightPatientZero   = 6
	HighlightOutbreak      = 7 // Count — заражений подряд
	HighlightLastStand     = 8 // Count 1 — продержался до конца
	HighlightAce           = 9
	HighlightRoundWin      = 10 // Count — номер раунда
	HighlightBossDown      = 11
	HighlightKill          = 12
)

// Роли игроков для итогов и лога боёв (PlayerRoles).
const (
	RoleNone      = 0
	RoleInnocent  = 1
	RoleDetective = 2
	RoleKiller    = 3
	RoleSurvivor  = 4
	RoleZombie    = 5
	RoleAlpha     = 6
)

const (
	highlightsKept   = 6
	highlightsRaw    = 64
	killStreakWindow = 80  // 4 секунды между убийствами — ещё серия
	outbreakWindow   = 200 // заражения — 10 секунд
)

type Highlight struct {
	Tick   int32
	Kind   int
	Player int // индекс в составе, -1 — без героя
	Count  int
	Score  int
}

type highlightState struct {
	list      []Highlight
	lastKill  map[int]int32
	streak    map[int]int
	streakRef map[int]int // индекс в list текущей серии игрока
	ballTouch int
}

func (w *World) hl() *highlightState {
	if w.highlights == nil {
		w.highlights = &highlightState{lastKill: map[int]int32{}, streak: map[int]int{}, streakRef: map[int]int{}, ballTouch: -1}
	}
	return w.highlights
}

func ownerOf(p *Player) *Player {
	if p != nil && p.Owner != nil {
		return p.Owner
	}
	return p
}

func heroIndex(p *Player) int {
	if p == nil {
		return -1
	}
	if p.Owner != nil {
		p = p.Owner
	}
	return p.Index
}

// addHighlight — момент в общий список. Возвращает его номер (-1 — не влез).
func (w *World) addHighlight(kind int, p *Player, count, score int) int {
	s := w.hl()
	h := Highlight{Tick: w.TickNum, Kind: kind, Player: heroIndex(p), Count: count, Score: score}
	if len(s.list) < highlightsRaw {
		s.list = append(s.list, h)
		return len(s.list) - 1
	}
	// Полный список: вытесняем самый слабый момент, если новый сильнее.
	weakest := 0
	for i, x := range s.list {
		if x.Score < s.list[weakest].Score {
			weakest = i
		}
	}
	if s.list[weakest].Score >= score {
		return -1
	}
	s.list[weakest] = h
	for player, ref := range s.streakRef {
		if ref == weakest {
			delete(s.streakRef, player)
		}
	}
	return weakest
}

// noteStreak — убийство (или заражение) в серию: второе подряд превращает
// простое убийство в «двойное», дальше — «тройное» и выше.
func (w *World) noteStreak(p *Player, window int32, single, multi int) {
	idx := heroIndex(p)
	if idx < 0 {
		return
	}
	s := w.hl()
	last, seen := s.lastKill[idx]
	s.lastKill[idx] = w.TickNum
	if seen && w.TickNum-last <= window {
		s.streak[idx]++
	} else {
		s.streak[idx] = 1
	}
	n := s.streak[idx]
	if n == 1 {
		if single > 0 {
			s.streakRef[idx] = w.addHighlight(single, p, 1, 3)
		} else {
			delete(s.streakRef, idx)
		}
		return
	}
	score := 10 * n
	if ref, ok := s.streakRef[idx]; ok && ref >= 0 && ref < len(s.list) && s.list[ref].Player == idx {
		// Серия растёт: момент тот же, но сильнее. Тик — первое убийство серии.
		s.list[ref].Kind, s.list[ref].Count, s.list[ref].Score = multi, n, score
		return
	}
	s.streakRef[idx] = w.addHighlight(multi, p, n, score)
}

// noteKill — обычное убийство героя героем (из Kill).
func (w *World) noteKill(killer, victim *Player) {
	if killer == nil || killer == victim {
		return
	}
	if s := w.modes.infection; s != nil {
		// Орда заражает: серия заражений — «вспышка», а не «двойное убийство».
		if st := s.of(ownerOf(killer)); st != nil && st.zombie {
			w.noteStreak(killer, outbreakWindow, 0, HighlightOutbreak)
			return
		}
	}
	if w.modes.knockout != nil {
		w.ko().roundKills[heroIndex(killer)]++
		w.ko().lastKiller = heroIndex(killer)
		w.ko().lastKillTick = w.TickNum
	}
	w.noteStreak(killer, killStreakWindow, HighlightKill, HighlightMultiKill)
}

// noteGoal — гол: автор — тот, кто последним вёл мяч, если он из забившей команды.
func (w *World) noteGoal(team int, winning bool) {
	s := w.hl()
	var scorer *Player
	if s.ballTouch >= 0 && s.ballTouch < len(w.Players) && w.Players[s.ballTouch].Team == team {
		scorer = w.Players[s.ballTouch]
	}
	score := 12
	if winning {
		score = 22
	}
	w.addHighlight(HighlightGoal, scorer, 1, score)
}

// Highlights — лучшие моменты боя по порядку времени.
func (w *World) Highlights() []Highlight {
	if w.highlights == nil {
		return nil
	}
	list := append([]Highlight(nil), w.highlights.list...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Score > list[j].Score })
	// Один и тот же эпизод (решающий удар раунда и серия убийств того же
	// игрока) — одна кнопка: остаётся более сильный момент.
	kept := list[:0]
	for _, h := range list {
		dup := false
		for _, k := range kept {
			if k.Player == h.Player && h.Player >= 0 && absTick(k.Tick-h.Tick) < 60 {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, h)
		}
	}
	list = kept
	if len(list) > highlightsKept {
		list = list[:highlightsKept]
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Tick < list[j].Tick })
	return list
}

func (w *World) setRole(p *Player, role int) {
	if w.PlayerRoles == nil {
		w.PlayerRoles = map[int]int{}
	}
	w.PlayerRoles[p.Index] = role
}

func absTick(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
