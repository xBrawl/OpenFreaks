// Typed YAML catalog parsing (parity with Emulator.Content catalog loading).
// Files are lists of maps; IDs are 1-based file order like the C# importer.
// Unknown fields are ignored; missing files fall back to synthetic content.
package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func asInt(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return n
		}
		return def
	case bool:
		if t {
			return 1
		}
		return 0
	}
	return def
}

func asStr(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return def
	}
	return fmt.Sprint(v)
}

func asBool(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func loadList(root, rel string) []map[string]any {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	var out []map[string]any
	if err := yaml.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

type Character struct {
	Id               int
	Name             string
	HeroType         string
	WeaponSkill      string
	UltSkill         string
	Speed            int
	HitPoints        int
	UltChargeMult    int
	UltUltMult       int
	ChargeAuto       int
	CollisionRadius  int
	Rarity           string
	Role             string
	HPRegenPctPerSec int
	HPLostPctPerSec  int
}

func (c *Character) IsHero() bool { return c.HeroType == "hero" || c.HeroType == "" }

type SkillAttack struct {
	Type       string
	Projectile string
	Damage     int
	MsBetween  int
	Spread     int
	NumBullets int
}

type SkillCharge struct {
	Type        string
	ChargeSpeed int
	Damage      int
	AfterSkill  string
}

type Skill struct {
	Id            int
	Name          string
	Behaviour     string
	Attack        *SkillAttack
	Charge        *SkillCharge
	AreaEffect    string
	RechargeMs    int
	MaxCharge     int
	CoolDownMs    int
	ActiveTimeMs  int
	CastingTimeMs int
	CastingRange  int
	CanMove       bool
	ExecuteFirst  bool
	IsTargeted    bool
	AlwaysMax     bool
}

type Projectile struct {
	Id           int
	Name         string
	Speed        int
	Radius       int
	EarlyTicks   int
	Gravity      int
	Indirect     bool
	Homing       bool
	PiercesChars bool
	PiercesEnv   bool
	PassesEnv    bool
	Pushback     int
}

const defaultGoalsToWin = 3

type GameMode struct {
	Ascent          bool
	Id              int
	Name            string
	TeamCount       int
	TeamPlayerCount int
	MaxPlayers      int
	DurationTicks   int
	RespawnTicks    int
	GoalsToWin      int
	Showdown        bool
	Arena           bool
	DPS             bool
}

type Location struct {
	RoundLocations []string
	Id             int
	Name           string
	Theme          string
	Mode           string
	MapName        string
	Hidden         bool
}

type GameMap struct {
	Name string
	Rows []string
	W    int
	H    int
}

type TileDef struct {
	Name         string
	Code         string
	Destructible bool
	BlocksMove   bool
	BlocksProj   bool
	HidesHero    bool
	Water        bool
}

type CharStat struct {
	Name         string
	Increasement int
	MaxLevel     int
}

type Dash struct {
	Id            int
	Name          string
	TargetChar    string
	TypeName      string
	SkillName     string
	CoolDownTicks int
}

type UltUpgrade struct {
	Id         int
	Name       string
	TargetChar string
	TypeName   string
	SkillName  string
}

type StarPower struct {
	Id         int
	Name       string
	TargetChar string
	UnlockCost int
}

type Currency struct {
	Id   int
	Name string
}

type Progression struct {
	MaxPoints  int
	KaritCosts []int
	TokenCosts []int
}

type CatalogData struct {
	Fingerprint string
	Documents   map[string]Spec
	Definitions map[string][]Spec
	Characters  []*Character
	CharByName  map[string]*Character
	CharByID    map[int]*Character
	Skills      []*Skill
	SkillByName map[string]*Skill
	SkillByID   map[int]*Skill
	Projectiles map[string]*Projectile
	Modes       []*GameMode
	ModeByName  map[string]*GameMode
	ModeByID    map[int]*GameMode
	Locations   []*Location
	LocByID     map[int]*Location
	LocByName   map[string]*Location
	Maps        map[string]*GameMap
	Tiles       map[string]*TileDef
	Stats       map[string]*CharStat
	Dashes      map[int]*Dash
	DashByName  map[string]*Dash
	UltUpgrades map[int]*UltUpgrade
	StarPowers  map[int]*StarPower
	Currencies  []*Currency
	Progression Progression
	Synthetic   bool
}

// Heroes — герои в порядке id, но Кролик (default_character) последним: он
// первый в YAML (id 1), а новый аккаунт и боты берут героев с начала списка.
func (c *CatalogData) Heroes() []*Character {
	var out []*Character
	var rabbit *Character
	for _, ch := range c.Characters {
		if ch.HeroType == "hero" {
			if ch.Name == "default_character" {
				rabbit = ch
				continue
			}
			out = append(out, ch)
		}
	}
	if rabbit != nil {
		out = append(out, rabbit)
	}
	if len(out) == 0 {
		for _, ch := range c.Characters {
			out = append(out, ch)
		}
	}
	return out
}

func Parse(root string) (*CatalogData, error) {
	root = resolveRoot(root)
	documents, lists, err := readDocuments(root)
	if err != nil {
		return nil, err
	}
	loadList := func(_ string, rel string) []map[string]any { return lists[rel] }
	c := &CatalogData{
		Fingerprint: Load(root).FingerprintChecksum(),
		Documents:   documents,
		CharByName:  map[string]*Character{}, CharByID: map[int]*Character{},
		SkillByName: map[string]*Skill{}, SkillByID: map[int]*Skill{},
		Projectiles: map[string]*Projectile{},
		ModeByName:  map[string]*GameMode{}, ModeByID: map[int]*GameMode{},
		LocByID: map[int]*Location{}, LocByName: map[string]*Location{},
		Maps: map[string]*GameMap{}, Tiles: map[string]*TileDef{},
		Stats:  map[string]*CharStat{},
		Dashes: map[int]*Dash{}, DashByName: map[string]*Dash{},
		UltUpgrades: map[int]*UltUpgrade{}, StarPowers: map[int]*StarPower{},
	}

	for i, m := range loadList(root, "models/characters.yaml") {
		ch := &Character{
			Id: i + 1, Name: asStr(m["name"], ""),
			HeroType:    asStr(m["hero_type_name"], "hero"),
			WeaponSkill: asStr(m["weapon_skill_name"], ""),
			UltSkill:    asStr(m["ultimate_skill_name"], ""),
			Speed:       asInt(m["speed"], 700), HitPoints: asInt(m["hit_points"], 3600),
			UltChargeMult:   asInt(m["ultimate_charge_multiplier"], 100),
			UltUltMult:      asInt(m["ultimate_charge_ultimate_multiplier"], 100),
			ChargeAuto:      asInt(m["charge_ultimate_automatically"], 0),
			CollisionRadius: asInt(m["collision_radius"], 120),
			Rarity:          asStr(m["rarity_name"], ""), Role: asStr(m["role_name"], ""),
			HPRegenPctPerSec: asInt(m["hit_points_regeneration_percent_per_second"], 0),
			HPLostPctPerSec:  asInt(m["hit_points_lost_percent_per_second"], 0),
		}
		c.Characters = append(c.Characters, ch)
		c.CharByName[ch.Name] = ch
		c.CharByID[ch.Id] = ch
	}

	for i, m := range loadList(root, "models/skills.yaml") {
		sk := &Skill{
			Id: i + 1, Name: asStr(m["name"], ""),
			RechargeMs:    asInt(m["recharge_time"], 0),
			MaxCharge:     asInt(m["max_charge"], 0),
			CoolDownMs:    asInt(m["cool_down"], 0),
			ActiveTimeMs:  asInt(m["active_time"], 0),
			CastingTimeMs: asInt(m["casting_time"], 0),
			CastingRange:  asInt(m["casting_range"], 15),
			CanMove:       asBool(m["can_move_at_same_time"], true),
			ExecuteFirst:  asBool(m["execute_first_attack_immediately"], false),
			IsTargeted:    asBool(m["is_targeted"], false),
			AlwaysMax:     asBool(m["always_cast_at_max_range"], false),
		}
		if b := asMap(m["behaviour"]); b != nil {
			sk.Behaviour = asStr(b["type"], "attack")
			if a := asMap(b["attack"]); a != nil {
				sk.Attack = &SkillAttack{
					Type:       asStr(a["type"], "projectile"),
					Projectile: asStr(a["projectile_name"], ""),
					Damage:     asInt(a["damage"], 0),
					MsBetween:  asInt(a["ms_between_attacks"], 100),
					Spread:     asInt(a["spread"], 0),
					NumBullets: asInt(a["num_bullets_in_one_attack"], 1),
				}
				if sk.Attack.NumBullets < 1 {
					sk.Attack.NumBullets = 1
				}
			} else if sk.Behaviour == "attack" {
				sk.Attack = &SkillAttack{Type: "direct", Damage: asInt(b["damage"], 100), NumBullets: 1}
			}
			if chg := asMap(b["charge"]); chg != nil {
				sk.Charge = &SkillCharge{
					Type:        asStr(chg["type"], ""),
					ChargeSpeed: asInt(chg["charge_speed"], 2000),
					Damage:      asInt(chg["damage"], 0),
					AfterSkill:  asStr(chg["after_charge_skill_name"], ""),
				}
			}
			sk.AreaEffect = asStr(b["area_effect_name"], "")
		}
		c.Skills = append(c.Skills, sk)
		c.SkillByName[sk.Name] = sk
		c.SkillByID[sk.Id] = sk
	}

	pid := 0
	for _, m := range loadList(root, "models/projectiles.yaml") {
		pid++
		p := &Projectile{
			Id: pid, Name: asStr(m["name"], ""),
			Speed: asInt(m["speed"], 3000), Radius: asInt(m["radius"], 0),
			EarlyTicks: asInt(m["early_ticks"], 0), Gravity: asInt(m["gravity"], 0),
			Indirect:     asBool(m["is_indirect"], false),
			Homing:       asBool(m["is_homing_missile"], false),
			PiercesChars: asBool(m["pierces_characters"], false),
			PiercesEnv:   asBool(m["pierces_environment"], false),
			PassesEnv:    asBool(m["passes_environment"], false),
			Pushback:     asInt(m["pushback_strength"], 0),
		}
		c.Projectiles[p.Name] = p
	}

	for i, m := range loadList(root, "models/game_modes.yaml") {
		tc := asInt(m["team_count"], 1)
		tpc := asInt(m["team_player_count"], 1)
		g := &GameMode{
			Id: i + 1, Name: asStr(m["name"], ""),
			TeamCount: tc, TeamPlayerCount: tpc, MaxPlayers: asInt(m["max_players"], tc*tpc),
			DurationTicks: asInt(m["duration_ticks"], 3600),
			RespawnTicks:  asInt(m["respawn_ticks"], 60),
			GoalsToWin:    asInt(m["goals_to_win"], defaultGoalsToWin),
			Showdown:      asBool(m["is_showdown"], false),
			Ascent:        asBool(m["is_ascent"], false),
			Arena:         asBool(m["is_arena"], false),
			DPS:           asBool(m["is_dps_calculation_enabled"], false),
		}
		if g.MaxPlayers < 1 {
			g.MaxPlayers = 1
		}
		c.Modes = append(c.Modes, g)
		c.ModeByName[g.Name] = g
		c.ModeByID[g.Id] = g
	}

	for i, m := range loadList(root, "models/locations.yaml") {
		l := &Location{
			Id: i + 1, Name: asStr(m["name"], ""),
			Theme:   asStr(m["location_theme_name"], ""),
			Mode:    asStr(m["game_mode_name"], "showdown"),
			MapName: asStr(m["map_name"], "default"),
			Hidden:  asBool(m["is_hidden"], false),
		}
		for _, v := range (Spec{Value: m["round_location_names"]}).List() {
			l.RoundLocations = append(l.RoundLocations, v.Text())
		}
		c.Locations = append(c.Locations, l)
		c.LocByID[l.Id] = l
		c.LocByName[l.Name] = l
	}
	// Maps (data: multi-line plain scalar, one row per source line).
	// NOTE: yaml folds plain-scalar newlines, so parse line-wise.
	if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("models/maps.yaml"))); err == nil {
		var name string
		var rows []string
		flush := func() {
			if name == "" {
				return
			}
			w := 0
			for _, row := range rows {
				if len(row) > w {
					w = len(row)
				}
			}
			c.Maps[name] = &GameMap{Name: name, Rows: rows, W: w, H: len(rows)}
		}
		inData := false
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimRight(line, "\r")
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- name:") {
				flush()
				name = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
				rows = nil
				inData = false
				continue
			}
			if trimmed == "data:" {
				inData = true
				continue
			}
			if inData && name != "" && trimmed != "" && (strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")) {
				rows = append(rows, trimmed)
			}
		}
		flush()
	}

	for _, m := range loadList(root, "models/map_tiles.yaml") {
		code := asStr(m["tile_code"], "")
		if code == "" {
			continue
		}
		c.Tiles[code] = &TileDef{
			Name: asStr(m["name"], ""), Code: code,
			Destructible: asBool(m["is_destructible_with_any"], false) || asBool(m["is_destructible_with_piercing"], false),
			BlocksMove:   asBool(m["blocks_movement"], false),
			BlocksProj:   asBool(m["blocks_projectiles"], false),
			HidesHero:    asBool(m["hides_hero"], false),
			Water:        asStr(m["name"], "") == "water",
		}
	}

	for _, m := range loadList(root, "models/character_stats.yaml") {
		name := asStr(m["name"], "")
		if name == "" {
			continue
		}
		c.Stats[name] = &CharStat{
			Name:         name,
			Increasement: asInt(m["increasement_per_level_percent"], 0),
			MaxLevel:     asInt(m["max_level"], 1),
		}
	}

	for i, m := range loadList(root, "models/dashes.yaml") {
		t := asMap(m["type"])
		d := &Dash{
			Id: i + 1, Name: asStr(m["name"], ""),
			TargetChar:    asStr(m["target_character_name"], ""),
			CoolDownTicks: asInt(m["cool_down_ticks"], 200),
		}
		if t != nil {
			d.TypeName = asStr(t["name"], "")
			d.SkillName = asStr(t["skill_name"], "")
		}
		c.Dashes[d.Id] = d
		c.DashByName[d.Name] = d
	}

	for i, m := range loadList(root, "models/ultimate_upgrades.yaml") {
		t := asMap(m["type"])
		u := &UltUpgrade{Id: i + 1, Name: asStr(m["name"], ""), TargetChar: asStr(m["target_character_name"], "")}
		if t != nil {
			u.TypeName = asStr(t["name"], "")
			u.SkillName = asStr(t["skill_name"], "")
		}
		c.UltUpgrades[u.Id] = u
	}

	for i, m := range loadList(root, "models/star_powers.yaml") {
		sp := &StarPower{
			Id: i + 1, Name: asStr(m["name"], ""),
			TargetChar: asStr(m["target_character_name"], ""),
			UnlockCost: asInt(m["unlock_upgrade_tokens_spent"], 4),
		}
		c.StarPowers[sp.Id] = sp
	}

	for i, m := range loadList(root, "models/currencies.yaml") {
		c.Currencies = append(c.Currencies, &Currency{Id: i + 1, Name: asStr(m["name"], "")})
	}

	if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("models/character_progression.yaml"))); err == nil {
		var raw struct {
			MaxPoints  int   `yaml:"max_points"`
			KaritCosts []int `yaml:"karit_costs"`
			TokenCosts []int `yaml:"upgrade_token_costs"`
		}
		if err := yaml.Unmarshal(b, &raw); err == nil {
			c.Progression = Progression{MaxPoints: raw.MaxPoints, KaritCosts: raw.KaritCosts, TokenCosts: raw.TokenCosts}
		}
	}
	if len(c.Characters) == 0 || len(c.Modes) == 0 {
		s := SyntheticCatalog()
		s.Synthetic = true
		return s, fmt.Errorf("content: catalog empty, synthetic fallback")
	}
	c.Definitions = map[string][]Spec{}
	for _, table := range []string{"characters", "skills", "projectiles", "area_effects", "items", "dashes", "ultimate_upgrades", "star_powers", "character_stats", "game_modes", "augments", "shop_offers", "gacha_boxes", "skins", "alliance_roles", "missions", "battle_pass"} {
		for i, entry := range loadList(root, "models/"+table+".yaml") {
			c.Definitions[table] = append(c.Definitions[table], Spec{ID: i + 1, Value: entry})
		}
	}
	return c, nil
}

func SyntheticCatalog() *CatalogData {
	c := &CatalogData{
		CharByName: map[string]*Character{}, CharByID: map[int]*Character{},
		SkillByName: map[string]*Skill{}, SkillByID: map[int]*Skill{},
		Projectiles: map[string]*Projectile{},
		ModeByName:  map[string]*GameMode{}, ModeByID: map[int]*GameMode{},
		LocByID: map[int]*Location{}, LocByName: map[string]*Location{},
		Maps: map[string]*GameMap{}, Tiles: map[string]*TileDef{},
		Stats:  map[string]*CharStat{},
		Dashes: map[int]*Dash{}, DashByName: map[string]*Dash{},
		UltUpgrades: map[int]*UltUpgrade{}, Synthetic: true,
	}
	defs := []Character{
		{Id: 1, Name: "synthetic_brawler", HeroType: "hero", WeaponSkill: "synthetic_weapon", UltSkill: "synthetic_ult", Speed: 720, HitPoints: 3600, UltChargeMult: 100, CollisionRadius: 120},
		{Id: 2, Name: "synthetic_caster", HeroType: "hero", WeaponSkill: "synthetic_weapon", UltSkill: "synthetic_ult", Speed: 760, HitPoints: 2800, UltChargeMult: 100, CollisionRadius: 120},
		{Id: 3, Name: "synthetic_tank", HeroType: "hero", WeaponSkill: "synthetic_weapon", UltSkill: "synthetic_ult", Speed: 640, HitPoints: 5200, UltChargeMult: 100, CollisionRadius: 120},
	}
	for i := range defs {
		ch := defs[i]
		c.Characters = append(c.Characters, &ch)
		c.CharByName[ch.Name] = &ch
		c.CharByID[ch.Id] = &ch
	}
	wsk := &Skill{Id: 1, Name: "synthetic_weapon", Behaviour: "attack", Attack: &SkillAttack{Type: "projectile", Damage: 600, MsBetween: 100, NumBullets: 1}, RechargeMs: 800, CoolDownMs: 800, ActiveTimeMs: 250, CastingRange: 30, CanMove: true, ExecuteFirst: true, IsTargeted: true}
	usk := &Skill{Id: 2, Name: "synthetic_ult", Behaviour: "attack", Attack: &SkillAttack{Type: "projectile", Damage: 1500, MsBetween: 100, NumBullets: 1}, CoolDownMs: 1000, ActiveTimeMs: 250, CastingRange: 40, CanMove: true, ExecuteFirst: true, IsTargeted: true}
	c.Skills = []*Skill{wsk, usk}
	c.SkillByName[wsk.Name] = wsk
	c.SkillByName[usk.Name] = usk
	c.SkillByID[1] = wsk
	c.SkillByID[2] = usk
	m := &GameMode{Id: 1, Name: "showdown", TeamCount: 10, TeamPlayerCount: 1, MaxPlayers: 10, DurationTicks: 3600, RespawnTicks: -1, Showdown: true}
	c.Modes = []*GameMode{m}
	c.ModeByName[m.Name] = m
	c.ModeByID[m.Id] = m
	l := &Location{Id: 1, Name: "synthetic", Mode: "showdown", MapName: "synthetic"}
	c.Locations = []*Location{l}
	c.LocByID[1] = l
	c.LocByName[l.Name] = l
	rows := make([]string, 12)
	for i := range rows {
		rows[i] = "................"
	}
	c.Maps["synthetic"] = &GameMap{Name: "synthetic", Rows: rows, W: 16, H: 12}
	c.Stats["hit_points"] = &CharStat{Name: "hit_points", Increasement: 10, MaxLevel: 5}
	c.Stats["damage"] = &CharStat{Name: "damage", Increasement: 15, MaxLevel: 5}
	c.Stats["speed"] = &CharStat{Name: "speed", Increasement: 5, MaxLevel: 5}
	return c
}
