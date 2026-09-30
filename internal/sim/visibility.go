package sim

func (w *World) CanSee(observer, target *Player) bool {
	if w.modes.nightmare != nil {
		return w.nightmareVisibility(observer, target) == 15
	}
	if observer == nil || observer == target || observer.Team == target.Team || target.RevealedUntil > w.TickNum {
		return true
	}
	x, y := w.Tiles.tileAt(target.X, target.Y)
	index := y*w.Tiles.W + x
	if !w.Tiles.at(x, y) || index >= len(w.Tiles.HidesHero) || !w.Tiles.HidesHero[index] || w.Tiles.Destroyed[index] {
		return true
	}
	for _, ally := range w.Players {
		if ally.Alive && !ally.Abandoned && ally.Team == observer.Team && Dist(ally.X, ally.Y, target.X, target.Y) <= 600 && shotClear(w, ally.X, ally.Y, target.X, target.Y, &Skill{}) {
			return true
		}
	}
	return false
}
