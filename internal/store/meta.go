package store

import (
	"fmt"
	"github.com/freaks/openfreaks/internal/content"
)

// MetaState is mutated under LockState alongside wallets, so rewards, receipts
// and their progression are committed together.
type MetaState struct {
	Arenas      map[string]*ArenaTournament
	Allocations map[string]*Allocation
	Players     map[int64]*PlayerMeta
	Bonds       map[string]*Bond
	Reactors    map[string]*Reactor
	Receipts    map[string]int64
	Ledger      []LedgerEntry

	tx *txState `json:"-"`
}
type Allocation struct {
	FinalRanks         map[int]int
	ArenaID            string
	ArenaLobby         bool
	LocationID, ModeID int32
	StartedAt          int64
	Players            []BattleParticipant
}
type BattleParticipant struct {
	TestBalance                                                                    content.TestBalance
	LeaveDelivered                                                                 bool
	AugmentIDs                                                                     []int32
	Abandoned                                                                      bool
	PlayerID                                                                       int64
	Name                                                                           string
	Index, Team                                                                    int
	CharacterModelID, HPLevel, DamageLevel, SpeedLevel, StarPowerID, DashID, UltID int32
	Trophies, Rank                                                                 int32
	IsBot                                                                          bool
	// ModeRole — роль в режиме (убийца, детектив, зомби…), sim.Role*.
	ModeRole int32 `json:",omitempty"`
}
type PlayerMeta struct {
	TestBalance                       map[int32]content.TestBalance
	ResultRanks                       map[int]map[int]int
	OwnedStars, OwnedDashes           map[int]bool
	Mastery                           map[int]*Mastery
	Completed                         map[string]int64
	Streak, Best, LastVisit, LastSpin int
	Milestones                        map[int]int64
	Offers                            map[int]int64
	Pass                              PassProgress
	Missions                          map[string]*MissionProgress
	ReactorClaims                     map[int]int64
	// Career — счётчики для профиля; копятся с 2026-09-24.
	Career Career `json:",omitempty"`
}

// Career — итог всех боёв игрока (боты и брошенные бои не считаются).
type Career struct {
	Kills, Deaths, Battles int
	PlaySeconds            int64
}
type Mastery struct {
	Points, Matches, Wins, RoleScore, StyleScore int
	Damage                                       int64
}
type PassProgress struct {
	Season                                                int
	EndsAt                                                int64
	Tier, ClaimedFree, ClaimedPremium, ClaimedPremiumPlus int
}
type MissionProgress struct {
	ModelID, DurationType, ModeID, CharacterID, Progress, Target, Reward int
	Start, End                                                           int64
	Claimed                                                              bool
}
type Bond struct {
	Low, High, Requester, CreatedAt int64
	Type, Matches, Wins             int
	Title, PinnedBattle             string
}
type Reactor struct {
	AllianceID     int64
	Season, Points int
	Members        map[int64]*ReactorMember
	Unlocks        map[int]int64
}
type ReactorMember struct {
	Points, Matches int
	Claimed         bool
}
type LedgerEntry struct {
	ID                       string
	PlayerID                 int64
	Reason                   string
	Currency, Delta, Balance int32
	Reference                string
	CreatedAt                int64
}

func NewMeta() *MetaState {
	return &MetaState{Arenas: map[string]*ArenaTournament{}, Allocations: map[string]*Allocation{}, Players: map[int64]*PlayerMeta{}, Bonds: map[string]*Bond{}, Reactors: map[string]*Reactor{}, Receipts: map[string]int64{}}
}

func (m *MetaState) Player(id int64) *PlayerMeta {
	if m.Players == nil {
		m.Players = map[int64]*PlayerMeta{}
	}
	p := m.Players[id]
	m.notePlayer(id, p)
	if p == nil {
		p = &PlayerMeta{Mastery: map[int]*Mastery{}, Completed: map[string]int64{}, LastVisit: -1, LastSpin: -1, Milestones: map[int]int64{}, Offers: map[int]int64{}, Missions: map[string]*MissionProgress{}, ReactorClaims: map[int]int64{}}
		m.Players[id] = p
	}
	if p.OwnedStars == nil {
		p.OwnedStars = map[int]bool{}
	}
	if p.OwnedDashes == nil {
		p.OwnedDashes = map[int]bool{}
	}
	if p.Mastery == nil {
		p.Mastery = map[int]*Mastery{}
	}
	if p.Completed == nil {
		p.Completed = map[string]int64{}
	}
	if p.Milestones == nil {
		p.Milestones = map[int]int64{}
	}
	if p.Offers == nil {
		p.Offers = map[int]int64{}
	}
	if p.Missions == nil {
		p.Missions = map[string]*MissionProgress{}
	}
	if p.ReactorClaims == nil {
		p.ReactorClaims = map[int]int64{}
	}
	return p
}

func (m *MetaState) PlayerIfExists(id int64) *PlayerMeta {
	if m.Players == nil {
		return nil
	}
	return m.Players[id]
}

func (m *MetaState) Reactor(id int64, season int) *Reactor {
	key := fmt.Sprintf("%d:%d", id, season)
	r := m.Reactors[key]
	if r == nil {
		r = &Reactor{AllianceID: id, Season: season, Members: map[int64]*ReactorMember{}, Unlocks: map[int]int64{}}
		m.Reactors[key] = r
	}
	return r
}
func (r *Reactor) Member(pid int64) *ReactorMember {
	p := r.Members[pid]
	if p == nil {
		p = &ReactorMember{}
		r.Members[pid] = p
	}
	return p
}
func BondKey(a, b int64) string { return fmt.Sprintf("%d:%d", min(a, b), max(a, b)) }

func cloneMeta(m *MetaState) *MetaState {
	if m == nil {
		return NewMeta()
	}
	v := &MetaState{
		Arenas:      make(map[string]*ArenaTournament, len(m.Arenas)),
		Allocations: make(map[string]*Allocation, len(m.Allocations)),
		Players:     make(map[int64]*PlayerMeta, len(m.Players)),
		Bonds:       make(map[string]*Bond, len(m.Bonds)),
		Reactors:    make(map[string]*Reactor, len(m.Reactors)),
		Receipts:    make(map[string]int64, len(m.Receipts)),
		Ledger:      cloneSlice(m.Ledger),
	}
	for k, a := range m.Arenas {
		v.Arenas[k] = cloneArena(a)
	}
	for k, a := range m.Allocations {
		v.Allocations[k] = cloneAllocation(a)
	}
	for k, r := range m.Reactors {
		v.Reactors[k] = cloneReactor(r)
	}
	for k, b := range m.Bonds {
		cp := *b
		v.Bonds[k] = &cp
	}
	for k, r := range m.Receipts {
		v.Receipts[k] = r
	}
	for id, p := range m.Players {
		v.Players[id] = clonePlayerMeta(p)
	}

	for id := range v.Players {
		v.Player(id)
	}
	return v
}
