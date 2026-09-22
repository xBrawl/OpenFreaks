package sim

type hiddenView struct {
	X, Y, Z float64
	HP      int
}

type seenEntry struct {
	hiddenView
	tick int32
}

const (
	seenSweepThreshold = 512
	seenMaxAgeTicks    = 400
)

func (w *World) rememberSeen(recipientIdx int, p *Player) {
	if w.lastSeen == nil {
		w.lastSeen = map[int]map[int]seenEntry{}
	}
	byTarget := w.lastSeen[recipientIdx]
	if byTarget == nil {
		byTarget = map[int]seenEntry{}
		w.lastSeen[recipientIdx] = byTarget
	}
	if len(byTarget) > seenSweepThreshold {
		for id, e := range byTarget {
			if w.TickNum-e.tick > seenMaxAgeTicks {
				delete(byTarget, id)
			}
		}
	}
	byTarget[p.ObjectIndex] = seenEntry{hiddenView{p.X, p.Y, p.Z, p.HP}, w.TickNum}
}

func (w *World) hiddenViewFor(recipientIdx int, p *Player) *hiddenView {
	if byTarget, ok := w.lastSeen[recipientIdx]; ok {
		if e, ok := byTarget[p.ObjectIndex]; ok {
			view := e.hiddenView
			return &view
		}
	}
	return &hiddenView{X: p.SpawnX, Y: p.SpawnY, Z: 0, HP: p.MaxHP}
}
