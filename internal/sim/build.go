package sim

import (
	"fmt"
	"math"
	"strings"

	"github.com/freaks/openfreaks/internal/content"
)

type BuildParams struct {
	ArenaRound   bool
	BattleID     string
	LocationID   int
	LocationName string
	Seed         int64
	IntroTicks   int
	Players      []BuildPlayer
}

type BuildPlayer struct {
	TestBalance  content.TestBalance
	AugmentIDs   []int32
	PlayerID     int64
	IsBot        bool
	Index        int
	Team         int
	Name         string
	CharacterID  int
	HPLevel      int
	DamageLevel  int
	SpeedLevel   int
	DashID       int
	UltUpgradeID int
	StarPowerID  int
}

func UpgradeSteps(level, maxLevel int) int {
	if maxLevel < 1 {
		maxLevel = 1
	}
	if level < 1 {
		level = 1
	}
	s := level - 1
	if s > maxLevel-1 {
		s = maxLevel - 1
	}
	return s
}

func statOf(cat *content.CatalogData, name string) (inc, max int) {
	if s, ok := cat.Stats[name]; ok {
		return s.Increasement, s.MaxLevel
	}
	return 0, 1
}

func BuildWorld(cat *content.CatalogData, p BuildParams) (*World, error) {
	loc, ok := cat.LocByID[p.LocationID]
	if !ok {
		return nil, fmt.Errorf("unknown_location:%d", p.LocationID)
	}
	if loc.Hidden && !p.ArenaRound {
		return nil, fmt.Errorf("hidden_location:%d", p.LocationID)
	}
	mode, ok := cat.ModeByName[loc.Mode]
	if !ok {
		return nil, fmt.Errorf("missing_game_mode:%s", loc.Mode)
	}
	gm, ok := cat.Maps[loc.MapName]
	if !ok {
		return nil, fmt.Errorf("unknown_map:%s", loc.MapName)
	}
	w := &World{
		Endless: mode.Name == "test_arena", Catalog: cat, NextCharacterIndex: 1000, NextEffectIndex: 1,
		BattleID: p.BattleID, ModeName: mode.Name, ModeID: int32(mode.Id),
		LocationID:   int32(loc.Id),
		LocationName: loc.Name,
		TeamCount:    max1(mode.TeamCount), TeamPlayerCount: max1(mode.TeamPlayerCount),
		MaxPlayers: max1(mode.MaxPlayers),
		TeamPoints: make([]int, max1(mode.TeamCount)),
		IsShowdown: mode.Showdown, DPSEnabled: mode.DPS,
		IsAscent: mode.Ascent, IsArenaLobby: mode.Arena && strings.HasSuffix(mode.Name, "lobby"),
		HasCarryable: mode.Name == "ball" || mode.Name == "ball_3",
		IsBounty:     strings.HasPrefix(mode.Name, "bounty"),
		IsKOTH:       strings.HasPrefix(mode.Name, "king_of_the_hill"),
		IntroTicks:   p.IntroTicks, DurationTicks: max1(mode.DurationTicks),
		RespawnTicks: mode.RespawnTicks,
		GoalsToWin:   mode.GoalsToWin,
		WinnerTeam:   -1, BallWinnerTeam: -1, NextPoisonTick: 430,
	}
	if p.LocationName != "" {
		w.LocationName = p.LocationName
	}
	buildTiles(w, cat, gm)
	spawns := computeSpawns(p.Players, mode, w)
	// Нокаут: команды идут снизу вверх по длинной карте — точки '1' (низ,
	// команда 0) и '2' (верх, команда 1) берём прямо из карты.
	if mode.Name == ModeKnockout || mode.Name == ModeKaritGrab {
		mapSpawns(spawns, p.Players, gm)
	}
	megaBossHumans := 0
	if mode.Name == "mega_boss" {
		for _, bp := range p.Players {
			if !bp.IsBot {
				megaBossHumans++
			}
		}
	}
	if w.HasCarryable {
		for _, bp := range p.Players {
			offset := float64(bp.Index%3-1) * 600
			if w.BallHorizontal() {
				x := 2100.0
				if bp.Team == 1 {
					x = w.Tiles.PixW - 2100
				}
				spawns[bp.Index] = [2]float64{x, w.Tiles.PixH/2 + offset}
				continue
			}
			y := w.Tiles.PixH - 2100
			if bp.Team == 1 {
				y = 2100
			}
			spawns[bp.Index] = [2]float64{w.Tiles.PixW/2 + offset, y}
		}
	}
	for _, bp := range p.Players {
		ch := cat.CharByID[bp.CharacterID]
		if ch == nil {
			return nil, fmt.Errorf("unknown_character:%d", bp.CharacterID)
		}
		rosterModel := int32(ch.Id)
		if mode.Name == ModeNightmare {
			if explorer := explorerFor(cat, &bp); explorer != nil {
				ch = explorer
			}
		}
		pl := buildPlayer(cat, w, bp, ch)
		pl.RosterTeam = bp.Team
		pl.RosterModelID = rosterModel
		if ch.Name == "karity_boss_character" {
			switch mode.Name {
			case "mega_boss":
				pl.MaxHP = ch.HitPoints * max(3, megaBossHumans) / 3
				w.MegaBossHumans = megaBossHumans
			case "test_arena":
				pl.MaxHP = 36000
			}
			pl.HP = pl.MaxHP
		}
		sx, sy := spawns[bp.Index][0], spawns[bp.Index][1]
		// Karity is the arena boss, not another participant on the spawn ring.
		// Put the large body at the visual centre of the map. FindOpen still
		// moves it to the nearest legal tile if the exact centre is blocked.
		if ch.Name == "karity_boss_character" && (mode.Name == "test_arena" || mode.Name == "mega_boss") {

			sx, sy = w.Tiles.PixW/2, w.Tiles.PixH/2+2*PixelsPerTile
		}
		ox, oy := w.Tiles.FindOpen(sx, sy, pl.CollisionRadius)
		pl.X, pl.Y, pl.ExactX, pl.ExactY = ox, oy, ox, oy
		pl.SpawnX, pl.SpawnY = ox, oy
		w.Players = append(w.Players, pl)
	}
	if w.IsBounty {
		for _, pl := range w.Players {
			pl.GamePoints = 2
		}
	}
	if w.HasCarryable && len(w.Players) > 0 {
		w.SpawnBall()
	}
	setupModes(w, p, gm)
	return w, nil
}

func (w *World) SpawnBall() *Player {
	index := w.AllocCharacterIndex()
	if index == 0 {
		return nil
	}
	ball := &Player{
		Index: -1, ObjectIndex: index, Team: -1, Name: "ball",
		HeroType: "laser_ball", ModelID: 0,
		HP: 1 << 30, MaxHP: 1 << 30, Alive: true,
		Visibility: 15, SpawnShield: true, CollisionRadius: 150,
	}
	if w.Catalog != nil {
		for _, c := range w.Catalog.Characters {
			if c.HeroType == "laser_ball" {
				ball.ModelID = int32(c.Id)
				ball.Definition = w.Catalog.SpecByID("characters", c.Id)
				ball.HP = c.HitPoints
				ball.MaxHP = c.HitPoints
				break
			}
		}
	}
	ball.X, ball.Y = w.Tiles.PixW/2, w.Tiles.PixH/2
	ball.ExactX, ball.ExactY = ball.X, ball.Y
	w.Minions = append(w.Minions, ball)
	w.TouchMinions()
	w.Ball = ball
	return ball
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

func buildTiles(w *World, cat *content.CatalogData, gm *content.GameMap) {
	t := TileGrid{W: gm.W, H: gm.H, PixW: float64(gm.W * PixelsPerTile), PixH: float64(gm.H * PixelsPerTile)}
	n := gm.W * gm.H
	t.Blocked = make([]bool, n)
	t.BlocksProj = make([]bool, n)
	t.Destructible = make([]bool, n)
	t.Destroyed = make([]bool, n)
	t.HidesHero = make([]bool, n)
	for y, row := range gm.Rows {
		for x := 0; x < gm.W && x < len(row); x++ {
			code := string(row[x])
			def, ok := cat.Tiles[code]
			if !ok {
				continue
			}
			// Unity MapModel.GetCodes flips the top-to-bottom YAML rows.
			i := (gm.H-1-y)*gm.W + x
			t.Blocked[i] = def.BlocksMove
			t.BlocksProj[i] = def.BlocksProj
			t.Destructible[i] = def.Destructible
			t.HidesHero[i] = def.HidesHero
		}
	}
	w.Tiles = t
	w.IsBigMap = gm.W > 21
}

// testSpeedAdd переводит тестовый множитель скорости (базисные пункты) в
// надбавку SpeedAdd: (100 + add) × bp / 10000 − 100, в пределах снапшота.
func testSpeedAdd(add, bp int) int {
	if bp == content.BalanceUnit || bp <= 0 {
		return add
	}
	return max(-90, min(1000, content.ScaleBP(100+add, bp)-100))
}

func buildPlayer(cat *content.CatalogData, w *World, bp BuildPlayer, ch *content.Character) *Player {
	hpInc, hpMax := statOf(cat, "hit_points")
	dmgInc, dmgMax := statOf(cat, "damage")
	spdInc, spdMax := statOf(cat, "speed")
	hpSteps := UpgradeSteps(bp.HPLevel, hpMax)
	maxHP := ch.HitPoints + ch.HitPoints*hpSteps*hpInc/100
	pl := &Player{
		PlayerID: bp.PlayerID, IsBot: bp.IsBot,
		Index: bp.Index, ObjectIndex: bp.Index, Team: bp.Team,
		Name: bp.Name, ModelID: int32(ch.Id), HeroType: ch.HeroType,
		HP: maxHP, MaxHP: maxHP, Alive: true, Visibility: 15,
		BaseSpeed:     ch.Speed,
		DamageAddPct:  UpgradeSteps(bp.DamageLevel, dmgMax) * dmgInc,
		SpeedAdd:      UpgradeSteps(bp.SpeedLevel, spdMax) * spdInc,
		UltChargeMult: ch.UltChargeMult, UltUltMult: ch.UltUltMult,
		ChargeAuto:      ch.ChargeAuto,
		CollisionRadius: ch.CollisionRadius,
		HPLevel:         bp.HPLevel, DamageLevel: bp.DamageLevel, SpeedLevel: bp.SpeedLevel,
		UltUpgradeID:     int32(bp.UltUpgradeID),
		DashModelID:      int32(bp.DashID),
		SpawnShield:      true,
		SpawnShieldTicks: 40,
	}
	pl.Skills = buildSkills(cat, ch, bp)
	pl.World = w
	pl.Definition = cat.SpecByID("characters", ch.Id)
	if modeHero(ch, pl.Definition) {
		pl.HeroType = "hero"
		pl.WireHero = true
	}
	// Кролик выходит в бой с готовым телевизором (characters.yaml).
	if pl.Definition.Bool("ultimate_ready_at_start") {
		pl.UltCharge = 4000
	}
	pl.DashDefinition = cat.SpecByID("dashes", bp.DashID)
	pl.Upgrade = cat.SpecByID("ultimate_upgrades", bp.UltUpgradeID).At("type")
	pl.StarPower = cat.SpecByID("star_powers", bp.StarPowerID).At("type")
	balance := bp.TestBalance.Scaled()
	pl.MaxHP = max(1, content.ScaleBP(pl.MaxHP, balance.HP))
	pl.HP = pl.MaxHP
	// Тестовая скорость — через SpeedAdd, а не BaseSpeed: SpeedAdd уходит в
	// снапшот, и клиент предсказывает свой бег как model.speed × (1 + SpeedAdd%).
	// Правку BaseSpeed клиент не видит: при ×2 он бежал вдвое медленнее
	// сервера, и позицию всё время дёргало вперёд («рывки», «невидимая стена»).
	pl.SpeedAdd = testSpeedAdd(pl.SpeedAdd, balance.Speed)

	pl.DamageScaleBP = balance.Damage
	pl.BaseDamageAddPct = pl.DamageAddPct
	ApplyAugments(w, pl, bp.AugmentIDs)
	pl.SpawnHeight = float64(pl.Definition.Int("projectile_start_z", 450))
	if pl.DashDefinition.Exists() {
		count := pl.DashDefinition.Int("charge_count", -1)
		pl.DashChargeLeft = min(7, max(0, count))
		pl.DashUnlimited = count < 0
	}

	if pl.DashModelID >= 0 {
		if d, ok := cat.Dashes[int(pl.DashModelID)]; ok && d.TypeName == "dash_low_hit_points" {
			_ = d
		}
	}
	return pl
}

func buildSkills(cat *content.CatalogData, ch *content.Character, bp BuildPlayer) []*Skill {
	type skillSlot struct {
		name   string
		parent *skillSlot
	}
	queue := []*skillSlot{}
	if _, ok := cat.SkillByName[ch.WeaponSkill]; ok {
		queue = append(queue, &skillSlot{name: ch.WeaponSkill})
	}
	if _, ok := cat.SkillByName[ch.UltSkill]; ok {
		queue = append(queue, &skillSlot{name: ch.UltSkill})
	}
	isHero := ch.HeroType == "hero" || ch.HeroType == "minion_mirage"
	if isHero && bp.DashID > 0 {
		if d, ok := cat.Dashes[bp.DashID]; ok && d.TypeName == "skill" && d.SkillName != "" {
			if _, ok := cat.SkillByName[d.SkillName]; ok {
				queue = append(queue, &skillSlot{name: d.SkillName})
			}
		}
	}
	var out []*Skill
	for len(queue) > 0 {
		slot := queue[0]
		queue = queue[1:]
		cycle := false
		for ancestor := slot.parent; ancestor != nil; ancestor = ancestor.parent {
			if ancestor.name == slot.name {
				cycle = true
				break
			}
		}
		if cycle {
			continue
		}
		def, ok := cat.SkillByName[slot.name]
		if !ok {
			continue
		}
		// Unity CollectSkills serializes each slot, even when weapon and ultimate share a model.
		out = append(out, toRuntimeSkill(cat, def))
		if def.Charge != nil && def.Charge.AfterSkill != "" {
			if _, ok := cat.SkillByName[def.Charge.AfterSkill]; ok {
				queue = append(queue, &skillSlot{name: def.Charge.AfterSkill, parent: slot})
			}
		}
	}
	if ch.HeroType == "hero" && bp.UltUpgradeID > 0 {
		if u, ok := cat.UltUpgrades[bp.UltUpgradeID]; ok && u.TypeName == "chained_skill" && u.SkillName != "" {
			if def, ok := cat.SkillByName[u.SkillName]; ok {
				out = append(out, toRuntimeSkill(cat, def))
			}
		}
	}
	return out
}

func toRuntimeSkill(cat *content.CatalogData, def *content.Skill) *Skill {
	s := &Skill{
		Definition: cat.SpecByID("skills", def.Id),
		ModelID:    def.Id, Name: def.Name, Behaviour: def.Behaviour,
		MaxCharge: def.MaxCharge, RechargeMs: def.RechargeMs,
		CoolMs: def.CoolDownMs, ActiveMs: def.ActiveTimeMs,
		RangePx:   def.CastingRange * 100,
		MsBetween: 100, NumBullets: 1,
	}
	if def.MaxCharge >= 1 {
		s.Charges = def.MaxCharge
	}
	if def.Attack != nil {
		s.Damage = def.Attack.Damage
		s.MsBetween = def.Attack.MsBetween
		s.NumBullets = def.Attack.NumBullets
		s.Spread = def.Attack.Spread
		s.AttackType = def.Attack.Type
		s.Projectile = def.Attack.Projectile
		if p, ok := cat.Projectiles[def.Attack.Projectile]; ok {
			s.ProjectileID = p.Id
			s.ProjSpeed = p.Speed
			s.ProjRadius = p.Radius
			s.ProjPierceChars = p.PiercesChars
			s.ProjPierceEnv = p.PiercesEnv
			s.ProjPassEnv = p.PassesEnv
			s.Homing = p.Homing
			s.ProjIndirect = p.Indirect
		} else {
			s.ProjSpeed = 3500
		}
	}
	if def.Charge != nil {
		s.ChargeSpeed = def.Charge.ChargeSpeed
		s.ChargeTypeName = def.Charge.Type
		s.AfterSkill = def.Charge.AfterSkill
		if def.Charge.Damage > 0 {
			s.BehaviourDamage = def.Charge.Damage
		}
	}
	if s.MsBetween < 50 {
		s.MsBetween = 50
	}
	return s
}

func computeSpawns(players []BuildPlayer, mode *content.GameMode, w *World) map[int][2]float64 {
	out := map[int][2]float64{}
	W, H := w.Tiles.PixW, w.Tiles.PixH
	if mode.Showdown {
		n := len(players)
		if n < 1 {
			n = 1
		}
		radius := 0.35 * math.Min(W, H)
		for i, p := range players {
			a := 2 * math.Pi * float64(i) / float64(n)
			out[p.Index] = [2]float64{
				clampF(W/2+math.Cos(a)*radius, 0, 65535),
				clampF(H/2+math.Sin(a)*radius, 0, 65535),
			}
		}
		return out
	}
	if mode.TeamCount > 2 {
		teams := map[int][]BuildPlayer{}
		for _, p := range players {
			teams[p.Team] = append(teams[p.Team], p)
		}
		for team, members := range teams {
			angle := 2 * math.Pi * float64(team) / float64(mode.TeamCount)
			cx, cy := W/2+math.Cos(angle)*W*.36, H/2+math.Sin(angle)*H*.36
			for slot, p := range members {
				offset := (float64(slot) - float64(len(members)-1)/2) * 350
				out[p.Index] = [2]float64{cx - math.Sin(angle)*offset, cy + math.Cos(angle)*offset}
			}
		}
		return out
	}

	teams := map[int][]BuildPlayer{}
	for _, p := range players {
		t := p.Team
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}
		teams[t] = append(teams[t], p)
	}
	for t, members := range teams {
		for slot, p := range members {
			var x float64
			if t == 0 {
				x = W / 8
			} else {
				x = W * 7 / 8
			}
			y := H * (float64(slot) + 1) / (float64(len(members)) + 1)
			out[p.Index] = [2]float64{clampF(x, 0, 65535), clampF(y, 0, 65535)}
		}
	}
	return out
}

// mapSpawns ставит игроков команды на её клетки спавна из карты ('1' — команда 0,
// '2' — команда 1), по порядку слева направо; лишним — ближайшая клетка по кругу.
// Строки YAML идут сверху вниз, логика — снизу вверх (как в buildTiles).
func mapSpawns(out map[int][2]float64, players []BuildPlayer, gm *content.GameMap) {
	cells := [2][][2]float64{}
	for y, row := range gm.Rows {
		for x := 0; x < gm.W && x < len(row); x++ {
			t := -1
			switch row[x] {
			case '1':
				t = 0
			case '2':
				t = 1
			}
			if t < 0 {
				continue
			}
			ly := gm.H - 1 - y
			cells[t] = append(cells[t], [2]float64{
				(float64(x) + .5) * PixelsPerTile, (float64(ly) + .5) * PixelsPerTile,
			})
		}
	}
	slot := [2]int{}
	for _, p := range players {
		t := p.Team
		if t < 0 || t > 1 || len(cells[t]) == 0 {
			continue
		}
		out[p.Index] = cells[t][slot[t]%len(cells[t])]
		slot[t]++
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
