package sim

const (
	dpsWindowTicks   = 100
	dpsWindowSeconds = float64(dpsWindowTicks) * float64(TickMs) / 1000
)

func (w *World) TickDPS() {
	if !w.DPSEnabled {
		return
	}
	slot := int(w.TickNum) % dpsWindowTicks
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		if p.damageHistory == nil {
			p.damageHistory = make([]int, dpsWindowTicks)
			for i := range p.damageHistory {
				p.damageHistory[i] = p.DamageDealt
			}
		}
		p.damageHistory[slot] = p.DamageDealt
	}
}

func (w *World) DamagePerSecond(p *Player) int {
	if p == nil || p.damageHistory == nil {
		return 0
	}

	oldest := p.damageHistory[(int(w.TickNum)+1)%dpsWindowTicks]
	delta := p.DamageDealt - oldest
	if delta <= 0 {
		return 0
	}
	return int(float64(delta) / dpsWindowSeconds)
}
