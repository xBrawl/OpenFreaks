package battleserver

import (
	"log/slog"
	"sort"
	"strings"
)

// Здесь учитываются только подтверждённые нарушения для автоблокировки.
// Ошибки протокола и частота ввода сами по себе не доказывают использование
// чита: их обрабатывает inputguard.go, не добавляя игрока в Result.Cheaters.
// Детекторы, вызывающие cheat, должны учитывать эту границу доверия.

const (
	cheatThreshold = 100
	maxActionType  = 5
)

// CheatReport — игрок, пойманный античитом в этом бою.
type CheatReport struct {
	PlayerID int64
	Index    int
	Reason   string
	Score    int
}

type cheatMeter struct {
	score   map[int]int
	reasons map[int]map[string]bool
	flagged map[int]bool
}

// cheat начисляет очки игроку index. Вызывается под actionMu.
func (m *Match) cheat(index int, reason string, points int) {
	c := &m.cheats
	if c.score == nil {
		c.score, c.reasons, c.flagged = map[int]int{}, map[int]map[string]bool{}, map[int]bool{}
	}
	if p := m.parts[index]; p == nil || p.IsBot || c.flagged[index] {
		return
	}
	c.score[index] += points
	if c.reasons[index] == nil {
		c.reasons[index] = map[string]bool{}
	}
	c.reasons[index][reason] = true
	if c.score[index] >= cheatThreshold {
		c.flagged[index] = true
		slog.Warn("anticheat flagged", "battle", m.ID, "index", index, "player", m.parts[index].PlayerID, "score", c.score[index], "reasons", m.cheatReason(index))
	}
}

func (m *Match) cheatFlagged(index int) bool { return m.cheats.flagged[index] }

func (m *Match) cheatReason(index int) string {
	var reasons []string
	for r := range m.cheats.reasons[index] {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	return strings.Join(reasons, ", ")
}

// cheaters — пойманные за бой, для итога матча.
func (m *Match) cheaters() []CheatReport {
	m.actionMu.Lock()
	defer m.actionMu.Unlock()
	var out []CheatReport
	for index := range m.cheats.flagged {
		p := m.parts[index]
		if p == nil || p.IsBot || p.PlayerID <= 0 {
			continue
		}
		out = append(out, CheatReport{PlayerID: p.PlayerID, Index: index, Reason: m.cheatReason(index), Score: m.cheats.score[index]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}
