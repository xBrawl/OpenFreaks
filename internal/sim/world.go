package sim

import (
	"github.com/freaks/openfreaks/internal/content"
	"math"
)

const (
	TickMs          = 50
	TicksPerSecond  = 20
	PixelsPerTile   = 300
	MaxWirePosition = 65535
	MaxWireInput    = 32767
	MaxPlayers      = 10
)

const (
	StateIdle       = 0
	StateWalk       = 1
	StateAttack     = 2
	StateWalkAttack = 3
	StateRespawn    = 4
)

const (
	ProjFlying            = 0
	ProjRangeEnd          = 1
	ProjEnvironmentImpact = 2
	ProjCharacterImpact   = 3
	ProjEnvImpactDestruct = 4
	ProjCancelled         = 5
)

type Skill struct {
	Definition   content.Spec `json:"-"`
	ModelID      int
	Name         string
	Behaviour    string
	MaxCharge    int
	Charges      int
	RechargeMs   int
	CoolMs       int
	ActiveMs     int
	CoolLeft     int
	ActiveLeft   int
	RechargeLeft int
	CastPending  bool

	Damage          int
	RangePx         int
	MsBetween       int
	NumBullets      int
	Spread          int
	AttackType      string
	Projectile      string
	ProjectileID    int
	ProjSpeed       int
	ProjRadius      int
	ProjPierceChars bool
	ProjPierceEnv   bool
	ProjPassEnv     bool
	Homing          bool
	ProjIndirect    bool

	ChargeSpeed     int
	ChargeTypeName  string
	AfterSkill      string
	AreaEffect      string
	BehaviourDamage int
}

func (s *Skill) WireCharge() int {
	if s.MaxCharge >= 1 {
		v := s.Charges * 1000
		if v < 0 {
			v = 0
		}
		if v > 8191 {
			v = 8191
		}
		return v
	}
	return 0
}

type Execution struct {
	DamagePct                     int
	Cancelled                     bool
	ID                            int
	Started                       bool
	Duration, Elapsed, SpawnCount int
	StartX, StartY                float64
	Skill                         *Skill
	Ultimate                      bool
	DelayLeft                     int
	DurationLeft                  int
	TargetX                       float64
	TargetY                       float64
	Angle                         int
	NextPulseLeft                 int
	Completed                     bool
}

type Player struct {
	RevealedUntil                                            int32
	AugmentIDs                                               []int32
	Augments                                                 *AugmentState `json:"-"`
	World                                                    *World        `json:"-"`
	DamageTaken, HealingDone                                 int
	UltimateUses, DashUses, KillsWithUltimate                int
	PendingKills                                             []KillEvent
	Definition, DashDefinition, Upgrade, StarPower           content.Spec `json:"-"`
	Owner                                                    *Player      `json:"-"`
	Effects                                                  []StatusEffect
	BaseDamageAddPct, ReloadAddPct, PassiveTicks, HitCounter int
	ConsecutiveLeft, ConsecutiveTicks, DashActiveTicks       int
	SegmentPercent                                           int
	SpawnHeight                                              float64
	ScentSources                                             [][2]int
	Deaths                                                   int
	PlayerID                                                 int64
	IsBot                                                    bool
	BotControlled                                            bool
	DisconnectedTicks                                        int
	Brain                                                    *BotBrain `json:"-"`
	Index                                                    int
	ObjectIndex                                              int
	Team                                                     int
	Name                                                     string
	ModelID                                                  int32
	HeroType                                                 string
	X, Y, Z                                                  float64
	ExactX, ExactY                                           float64
	SpawnX, SpawnY                                           float64
	TargetX, TargetY                                         float64
	HasMoveTarget                                            bool
	HP, MaxHP                                                int
	Alive                                                    bool
	Abandoned                                                bool
	Visibility                                               int
	State                                                    int
	AngleForced                                              bool
	AttackAngle                                              int
	MoveAngle                                                int
	SkillCastLeft                                            int
	UltCharge                                                int
	UltActive                                                bool
	UltUsed                                                  bool
	UltChargeMult                                            int
	UltUltMult                                               int
	ChargeAuto                                               int
	BaseSpeed                                                int
	DamageAddPct                                             int
	// DamageScaleBP is the test-balance damage multiplier in basis points; 0 means 100%.
	DamageScaleBP    int
	SpeedAdd         int
	EffectsSpeedAdd  int
	CollisionRadius  int
	SpawnShield      bool
	SpawnShieldTicks int
	ShieldBuffTicks  int
	DamageDeltas     []int
	DeathTick        int32
	OneShotShield    bool
	ShieldBuff       bool
	ConsumableShield int
	ConsumableTicks  int
	SegmentTicks     int
	SegmentLevel     int
	Spot             bool
	Charged          bool
	ChargeMsLeft     int
	ChargeSpeed      int
	ChargeTotalMs    int
	ChargeType       int
	ControlRemoved   bool
	Teleporting      bool
	TeleportLeft     int
	PowerPoints      int
	GamePoints       int
	BlueStar         bool
	FirePoison       bool
	Slowdown         bool
	Electrocuted     bool
	Skills           []*Skill
	Executions       []*Execution

	DashModelID    int32
	DashChargeLeft int
	DashUnlimited  bool
	AccessoryState int
	DashCooldown   int
	ActingTicks    int

	HPLevel, DamageLevel, SpeedLevel int
	UltUpgradeID                     int32

	WalkPending    bool
	DashDX, DashDY int
	DashPending    bool

	Kills       int
	DamageDealt int

	damageHistory []int `json:"-"`
	RespawnLeft   int

	CarryableObjectID int
	CarryableAimX     int
	CarryableAimY     int
	LastThrowType     int
	LastThrowTeam     int
	ThrowCooldown     int
}

type TileGrid struct {
	W, H         int
	PixW, PixH   float64
	Blocked      []bool
	BlocksProj   []bool
	Destructible []bool
	Destroyed    []bool
	HidesHero    []bool

	order []int32
}

func (g *TileGrid) DestructibleOrder() []int32 {
	if g.order != nil {
		return g.order
	}
	order := make([]int32, 0, 64)
	for x := 0; x < g.W; x++ {
		for y := 0; y < g.H; y++ {
			i := y*g.W + x
			if i < len(g.Destructible) && g.Destructible[i] {
				order = append(order, int32(i))
			}
		}
	}
	if order == nil {
		order = []int32{}
	}
	g.order = order
	return order
}

func (g *TileGrid) at(x, y int) bool {
	if x < 0 || y < 0 || x >= g.W || y >= g.H {
		return false
	}
	return true
}

func (g *TileGrid) tileAt(px, py float64) (int, int) {
	return int(px / PixelsPerTile), int(py / PixelsPerTile)
}

func (g *TileGrid) CanOccupy(px, py float64, radius int) bool {
	if px < float64(radius) || py < float64(radius) || px > g.PixW-float64(radius) || py > g.PixH-float64(radius) {
		return false
	}
	r := float64(radius)
	for y := int((py - r) / PixelsPerTile); y <= int((py+r)/PixelsPerTile); y++ {
		for x := int((px - r) / PixelsPerTile); x <= int((px+r)/PixelsPerTile); x++ {
			if !g.at(x, y) {
				continue
			}
			i := y*g.W + x
			if !g.Blocked[i] || (i < len(g.Destroyed) && g.Destroyed[i]) {
				continue
			}
			nx := clampF(px, float64(x*PixelsPerTile), float64((x+1)*PixelsPerTile))
			ny := clampF(py, float64(y*PixelsPerTile), float64((y+1)*PixelsPerTile))
			if (px-nx)*(px-nx)+(py-ny)*(py-ny) < r*r || (r == 0 && px == nx && py == ny) {
				return false
			}
		}
	}
	return true
}

func (g *TileGrid) FindOpen(px, py float64, radius int) (float64, float64) {
	if g.CanOccupy(px, py, radius) {
		return px, py
	}
	for r := float64(PixelsPerTile); r < float64(g.W+g.H)*PixelsPerTile; r += PixelsPerTile {
		for a := 0; a < 16; a++ {
			nx := px + r*math.Cos(float64(a)*math.Pi/8)
			ny := py + r*math.Sin(float64(a)*math.Pi/8)
			if g.CanOccupy(nx, ny, radius) {
				return nx, ny
			}
		}
	}
	return px, py
}

type Projectile struct {
	ChainDepth                                           int
	Definition                                           content.Spec `json:"-"`
	SpawnX, SpawnY, TargetX, TargetY                     float64
	AgeTicks, ExecutionID, PiercedTileIndex              int
	Ultimate, Returning, Bouncing, Circling, TilePierced bool
	SummonName, SummonBehavior                           string
	SharedHits                                           map[*Player]bool
	LocalIndex                                           int
	ModelID                                              int32
	Owner                                                *Player
	Team                                                 int
	RosterIndex                                          int
	X, Y, Z                                              float64
	Angle                                                int
	SpeedTick                                            float64
	Traveled                                             float64
	RangePx                                              float64
	Damage                                               int
	Radius                                               int
	PierceChars                                          bool
	PierceEnv                                            bool
	PassEnv                                              bool
	Homing                                               bool
	HomingTarget                                         *Player
	EarlyTicks                                           int
	Alive                                                bool
	TerminalTicks                                        int
	State                                                int
	HitSet                                               map[*Player]bool
}

type World struct {
	Endless                             bool
	BallOvertime                        bool
	Catalog                             *content.CatalogData `json:"-"`
	Minions                             []*Player
	Areas                               []*Area
	Items                               []*Item
	NextCharacterIndex, NextEffectIndex int
	BattleID                            string
	ModeName                            string
	ModeID                              int32
	LocationID                          int32
	LocationName                        string
	TickNum                             int32
	Players                             []*Player
	Projectiles                         []*Projectile
	NextProjIndex                       int
	TeamCount                           int
	TeamPlayerCount                     int
	MaxPlayers                          int
	TeamPoints                          []int
	Tiles                               TileGrid
	IsBigMap                            bool
	IsShowdown                          bool
	IsAscent, IsArenaLobby              bool
	HasCarryable                        bool
	IsBounty                            bool
	IsKOTH                              bool
	DPSEnabled                          bool
	IntroTicks                          int
	DurationTicks                       int
	RespawnTicks                        int
	Finished                            bool
	WinnerTeam                          int
	WinReason                           string
	Ball                                *Player
	RoundResetTicks                     int
	BallWinnerTeam                      int
	GoalsToWin                          int
	NextExecID                          int
	NextPoisonTick                      int32

	lastSeen map[int]map[int]seenEntry

	snapChars []*Player
	snapProjs []*Projectile

	charCache []*Player
	charGen   uint64
	charBuilt uint64
}

func (w *World) TouchMinions() { w.charGen++ }

func (w *World) Characters() []*Player {
	if len(w.Minions) == 0 {
		return w.Players
	}
	if w.charCache != nil && w.charBuilt == w.charGen && len(w.charCache) == len(w.Players)+len(w.Minions) {
		return w.charCache
	}
	all := make([]*Player, 0, len(w.Players)+len(w.Minions))
	all = append(all, w.Players...)
	all = append(all, w.Minions...)
	w.charCache = all
	w.charBuilt = w.charGen
	return all
}

type StatusEffect struct {
	Kind          string
	Value, Source int
	Expires       int32
}
type Area struct {
	SuppressHitProcs                          bool
	Owner                                     *Player      `json:"-"`
	Definition                                content.Spec `json:"-"`
	LocalIndex, OwnerIndex, RosterIndex, Team int
	X, Y                                      float64
	TimeMsLeft, Damage, Heal                  int
	Following, Ultimate                       bool
	NextPulseTick                             int32
}
type Item struct {
	Definition                                content.Spec `json:"-"`
	LocalIndex, OwnerIndex, RosterIndex, Team int
	X, Y, ExitX, ExitY                        float64
	TimeMsLeft, State, StateTicks             int
	UsedAt                                    map[int]int32
}

func (w *World) AliveCount() int {
	n := 0
	for _, p := range w.Players {
		if p.Alive && !p.Abandoned && !p.IsBall() {
			n++
		}
	}
	return n
}

func (w *World) TotalTicks() int { return w.IntroTicks + w.DurationTicks }

func (w *World) FindChar(idx int) *Player {
	for _, p := range w.Characters() {
		if p.ObjectIndex == idx {
			return p
		}
	}
	return nil
}

func (w *World) AddPoint(team, pts int) {
	for len(w.TeamPoints) <= team {
		w.TeamPoints = append(w.TeamPoints, 0)
	}
	w.TeamPoints[team] += pts
	if w.TeamPoints[team] > 1023 {
		w.TeamPoints[team] = 1023
	}
}

func (w *World) GetPoint(team int) int {
	if team < 0 || team >= len(w.TeamPoints) {
		return 0
	}
	return w.TeamPoints[team]
}

func (p *Player) IsBall() bool { return p.HeroType == "laser_ball" }

func clampPos(v float64) int {
	if v < 0 {
		return 0
	}
	if v > MaxWirePosition {
		return MaxWirePosition
	}
	return int(v)
}

func clampInput(v int) int {
	if v < 0 {
		return 0
	}
	if v > MaxWireInput {
		return MaxWireInput
	}
	return v
}

const CarrySpeedPercent = 82

func DisplayedSpeed(p *Player) float64 {
	speed := float64(p.BaseSpeed) + float64(p.BaseSpeed)*float64(p.SpeedAdd+p.EffectsSpeedAdd)/100
	if p.CarryableObjectID != 0 {
		speed = speed * CarrySpeedPercent / 100
	}
	if speed <= 0 && p.BaseSpeed > 0 {
		speed = 100
	}
	if speed < 0 {
		speed = 0
	}
	if speed > 100000 {
		speed = 100000
	}
	return speed
}

func NormalizeAngle(a int) int {
	for a < 0 {
		a += 360
	}
	return a % 360
}

func Dist(aX, aY, bX, bY float64) float64 {
	return math.Hypot(aX-bX, aY-bY)
}
