package battleserver

import "github.com/freaks/openfreaks/internal/sim"

// refreshVoiceProximity пересчитывает, кто кого слышит, после тика мира.
// Слушателям, у которых таблица поменялась, уходит новая таблица голосов:
// значки говорящих появляются и гаснут вместе со слышимостью.
func (m *Match) refreshVoiceProximity() {
	if m.voice == nil || !m.voice.proximity {
		return
	}
	var next [16][16]bool
	m.worldMu.Lock()
	byIndex := map[int]*sim.Player{}
	for _, p := range m.World.Players {
		if p.Index >= 0 && p.Index < 16 {
			byIndex[p.Index] = p
		}
	}
	for a, pa := range byIndex {
		for b, pb := range byIndex {
			if a == b {
				continue
			}
			switch {
			case m.World.IsNightmare():
				next[a][b] = m.World.NightmareHears(pa, pb)
			case m.World.IsInfection():
				next[a][b] = m.World.InfectionHears(pa, pb)
			}
		}
	}
	m.worldMu.Unlock()

	m.voice.mu.Lock()
	changed := []int{}
	for a := 0; a < 16; a++ {
		if m.voice.near[a] != next[a] {
			changed = append(changed, a)
		}
	}
	m.voice.near = next
	m.voice.mu.Unlock()
	for _, listener := range changed {
		m.sendVoiceTable(listener)
	}
}
