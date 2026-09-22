package sim

const (
	maxEntityIndex = 60000

	minCharacterIndex = 1000
	minEffectIndex    = 1
)

func (w *World) AllocCharacterIndex() int {
	if w.NextCharacterIndex < minCharacterIndex || w.NextCharacterIndex > maxEntityIndex {
		w.NextCharacterIndex = minCharacterIndex
	}
	span := maxEntityIndex - minCharacterIndex + 1
	for i := 0; i < span; i++ {
		id := w.NextCharacterIndex
		w.NextCharacterIndex++
		if w.NextCharacterIndex > maxEntityIndex {
			w.NextCharacterIndex = minCharacterIndex
		}
		if !w.characterIndexTaken(id) {
			return id
		}
	}
	return 0
}

func (w *World) characterIndexTaken(id int) bool {
	for _, p := range w.Players {
		if p != nil && p.ObjectIndex == id {
			return true
		}
	}
	for _, p := range w.Minions {
		if p != nil && p.ObjectIndex == id {
			return true
		}
	}
	return false
}

func (w *World) AllocEffectIndex() int {
	if w.NextEffectIndex < minEffectIndex || w.NextEffectIndex > maxEntityIndex {
		w.NextEffectIndex = minEffectIndex
	}
	span := maxEntityIndex - minEffectIndex + 1
	for i := 0; i < span; i++ {
		id := w.NextEffectIndex
		w.NextEffectIndex++
		if w.NextEffectIndex > maxEntityIndex {
			w.NextEffectIndex = minEffectIndex
		}
		if !w.effectIndexTaken(id) {
			return id
		}
	}
	return 0
}

func (w *World) effectIndexTaken(id int) bool {
	for _, a := range w.Areas {
		if a != nil && a.LocalIndex == id {
			return true
		}
	}
	for _, it := range w.Items {
		if it != nil && it.LocalIndex == id {
			return true
		}
	}
	return false
}
