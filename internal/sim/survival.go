package sim

import "math"

// Must match PoisonContainer.IsPositionPoisoned in the Unity client: a rounded
// square, not a circle. The same tick origin and radius also drive the VFX.
func (w *World) PoisonedAt(x, y float64, tick int32) bool {
	if w.modes.knockout != nil {
		return knockoutPoisoned(w, x, y, tick)
	}
	if !(w.IsShowdown || w.IsAscent) || w.IsArenaLobby || tick <= 430 {
		return false
	}
	radius := math.Min(w.Tiles.PixW, w.Tiles.PixH)/2 - float64(tick-430)*3
	if radius <= 150 {
		return true
	}
	qx := math.Max(0, 2*math.Abs(x-w.Tiles.PixW/2)-radius)
	qy := math.Max(0, 2*math.Abs(y-w.Tiles.PixH/2)-radius)
	return qx*qx+qy*qy > radius*radius
}
func (w *World) TickPoison() {
	if w.TickNum < w.NextPoisonTick {
		return
	}
	w.NextPoisonTick = w.TickNum + 20
	for _, p := range w.Players {
		if p.Alive && !p.Abandoned && w.PoisonedAt(p.X, p.Y, w.TickNum) {

			DealDamage(w, nil, p, max(1, p.MaxHP/10))
		}
	}
}
func pickUpItem(w *World, p *Player, item *Item) {
	value := item.Definition.Int("value")
	switch item.Definition.String("name") {
	case "hit_points_level_up":
		increase := min(65535-p.MaxHP, max(0, p.MaxHP*value/100))
		p.MaxHP += increase
		p.HP += increase
		p.HPLevel = min(63, p.HPLevel+1)
	case "damage_level_up":
		p.BaseDamageAddPct = min(1000, p.BaseDamageAddPct+value)
		p.DamageLevel = min(63, p.DamageLevel+1)
	case "speed_level_up":
		p.SpeedAdd = min(300, p.SpeedAdd+value)
		p.SpeedLevel = min(63, p.SpeedLevel+1)
	case "game_point":
		p.GamePoints = min(255, p.GamePoints+value)
		w.AddPoint(p.Team, value)
	case kgItem:
		kgPickUp(w, p)
	case "angel_wind_feather":
		GiveShield(p, p.MaxHP*value/100, item.Definition.Int("value2", 100))
	}
}
