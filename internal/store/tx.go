package store

import (
	"reflect"
	"sync"
)

type Dirty struct {
	All       bool
	Accounts  map[int64]bool
	Creds     map[string]bool
	Alliances map[int64]bool
	Friends   map[int64]bool
	Battles   map[string]bool
	Bonds     map[string]bool
	Receipts  map[string]bool
	Reactors  map[string]bool

	Core bool
}

func (d *Dirty) Empty() bool {
	if d == nil {
		return true
	}
	return !d.All && !d.Core && len(d.Accounts) == 0 && len(d.Creds) == 0 &&
		len(d.Alliances) == 0 && len(d.Friends) == 0 && len(d.Battles) == 0 &&
		len(d.Bonds) == 0 && len(d.Receipts) == 0 && len(d.Reactors) == 0
}

func FullDirty() *Dirty { return &Dirty{All: true} }

type coreSnapshot struct {
	teams       map[int64]*Team
	arenas      map[string]*ArenaTournament
	allocations map[string]*Allocation
	bonds       map[string]*Bond
	receipts    map[string]int64
	reactors    map[string]*Reactor
	playerSeq   int64
	allianceSeq int64
	teamSeq     int64
}

type txState struct {
	mu sync.Mutex

	accounts    map[int64]*Account
	accountSeen map[int64]bool
	creds       map[string]*Credential
	credSeen    map[string]bool
	alliances   map[int64]*Alliance
	allianceIn  map[int64]bool
	friends     map[int64]map[int64]int32
	friendSeen  map[int64]bool
	metaPlayers map[int64]*PlayerMeta
	metaSeen    map[int64]bool

	battles      []BattleRecord
	battlesTaken bool

	core      coreSnapshot
	ledgerLen int
}

func (s *Store) Begin() {
	s.mu.Lock()
	tx := &txState{
		accounts: map[int64]*Account{}, accountSeen: map[int64]bool{},
		creds: map[string]*Credential{}, credSeen: map[string]bool{},
		alliances: map[int64]*Alliance{}, allianceIn: map[int64]bool{},
		friends: map[int64]map[int64]int32{}, friendSeen: map[int64]bool{},
		metaPlayers: map[int64]*PlayerMeta{}, metaSeen: map[int64]bool{},
		battles: s.battles,
	}
	tx.core = s.snapshotCoreLocked()
	if s.Meta != nil {
		tx.ledgerLen = len(s.Meta.Ledger)
	}
	s.tx = tx
	if s.Meta != nil {
		s.Meta.tx = tx
	}
	s.mu.Unlock()
}

func (s *Store) End() {
	s.mu.Lock()
	s.tx = nil
	if s.Meta != nil {
		s.Meta.tx = nil
	}
	s.mu.Unlock()
}

func (s *Store) snapshotCoreLocked() coreSnapshot {
	c := coreSnapshot{
		teams:       make(map[int64]*Team, len(s.teams)),
		playerSeq:   s.playerSeq,
		allianceSeq: s.allianceSeq,
		teamSeq:     s.teamSeq,
	}
	for id, t := range s.teams {
		c.teams[id] = cloneTeam(t)
	}
	m := s.Meta
	if m == nil {
		return c
	}
	c.arenas = make(map[string]*ArenaTournament, len(m.Arenas))
	for id, a := range m.Arenas {
		c.arenas[id] = cloneArena(a)
	}
	c.allocations = make(map[string]*Allocation, len(m.Allocations))
	for id, a := range m.Allocations {
		c.allocations[id] = cloneAllocation(a)
	}
	c.bonds = make(map[string]*Bond, len(m.Bonds))
	for k, b := range m.Bonds {
		cp := *b
		c.bonds[k] = &cp
	}
	c.receipts = make(map[string]int64, len(m.Receipts))
	for k, v := range m.Receipts {
		c.receipts[k] = v
	}
	c.reactors = make(map[string]*Reactor, len(m.Reactors))
	for k, r := range m.Reactors {
		c.reactors[k] = cloneReactor(r)
	}
	return c
}

func (s *Store) noteAccount(pid int64, a *Account) {
	tx := s.tx
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.accountSeen[pid] {
		return
	}
	tx.accountSeen[pid] = true
	if a != nil {
		tx.accounts[pid] = cloneAccount(a)
	}
}

func (s *Store) noteCred(key string, c *Credential) {
	tx := s.tx
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.credSeen[key] {
		return
	}
	tx.credSeen[key] = true
	if c != nil {
		cp := *c
		cp.Salt = cloneBytes(c.Salt)
		cp.Hash = cloneBytes(c.Hash)
		tx.creds[key] = &cp
	}
}

func (s *Store) noteAlliance(id int64, al *Alliance) {
	tx := s.tx
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.allianceIn[id] {
		return
	}
	tx.allianceIn[id] = true
	if al != nil {
		tx.alliances[id] = cloneAlliance(al)
	}
}

func (s *Store) noteFriends(pid int64) {
	tx := s.tx
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.friendSeen[pid] {
		return
	}
	tx.friendSeen[pid] = true
	if row, ok := s.friends[pid]; ok {
		cp := make(map[int64]int32, len(row))
		for k, v := range row {
			cp[k] = v
		}
		tx.friends[pid] = cp
	}
}

func (s *Store) noteBattles() {
	tx := s.tx
	if tx == nil {
		return
	}
	tx.mu.Lock()
	tx.battlesTaken = true
	tx.mu.Unlock()
}

func (m *MetaState) notePlayer(pid int64, p *PlayerMeta) {
	tx := m.tx
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.metaSeen[pid] {
		return
	}
	tx.metaSeen[pid] = true
	if p != nil {
		tx.metaPlayers[pid] = clonePlayerMeta(p)
	}
}

// Rollback возвращает состояние к предобразу. Записи восстанавливаются на
// месте, по прежним указателям: ссылки, взятые до отката, остаются валидными.
func (s *Store) Rollback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.tx
	if tx == nil {
		return
	}
	s.tx = nil
	if s.Meta != nil {
		s.Meta.tx = nil
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()

	for pid := range tx.accountSeen {
		before, existed := tx.accounts[pid]
		live, alive := s.accounts[pid]
		switch {
		case !existed:
			if alive {
				delete(s.accounts, pid)
				delete(s.byName, upperKey(live.Name))
			}
		case alive:
			*live = *before
		default:
			s.accounts[pid] = before
		}
	}
	for key := range tx.credSeen {
		before, existed := tx.creds[key]
		live, alive := s.creds[key]
		switch {
		case !existed:
			delete(s.creds, key)
			delete(s.byName, key)
		case alive:
			*live = *before
		default:
			s.creds[key] = before
		}
	}
	for id := range tx.allianceIn {
		before, existed := tx.alliances[id]
		live, alive := s.allianceByID[id]
		switch {
		case !existed:
			delete(s.allianceByID, id)
		case alive:
			*live = *before
		default:
			s.allianceByID[id] = before
		}
	}
	for pid := range tx.friendSeen {
		if before, ok := tx.friends[pid]; ok {
			s.friends[pid] = before
		} else {
			delete(s.friends, pid)
		}
	}
	if tx.battlesTaken {
		s.battles = tx.battles
	}
	if m := s.Meta; m != nil {
		for pid := range tx.metaSeen {
			before, existed := tx.metaPlayers[pid]
			live, alive := m.Players[pid]
			switch {
			case !existed:
				delete(m.Players, pid)
			case alive:
				*live = *before
			default:
				m.Players[pid] = before
			}
		}
		m.Arenas = tx.core.arenas
		m.Allocations = tx.core.allocations
		m.Bonds = tx.core.bonds
		m.Receipts = tx.core.receipts
		m.Reactors = tx.core.reactors
		if tx.ledgerLen <= len(m.Ledger) {
			m.Ledger = m.Ledger[:tx.ledgerLen]
		}
	}
	s.teams = tx.core.teams
	s.playerSeq = tx.core.playerSeq
	s.allianceSeq = tx.core.allianceSeq
	s.teamSeq = tx.core.teamSeq
}

func (s *Store) DirtySet() *Dirty {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tx := s.tx
	if tx == nil {
		return FullDirty()
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	d := &Dirty{
		Accounts: map[int64]bool{}, Creds: map[string]bool{},
		Alliances: map[int64]bool{}, Friends: map[int64]bool{},
		Battles: map[string]bool{}, Bonds: map[string]bool{},
		Receipts: map[string]bool{}, Reactors: map[string]bool{},
	}
	for pid := range tx.accountSeen {
		before := tx.accounts[pid]
		live := s.accounts[pid]
		if before == nil || live == nil || !reflect.DeepEqual(before, live) {
			d.Accounts[pid] = true
		}
	}

	if m := s.Meta; m != nil {
		for pid := range tx.metaSeen {
			before := tx.metaPlayers[pid]
			live := m.Players[pid]
			if before == nil || live == nil || !reflect.DeepEqual(before, live) {
				d.Accounts[pid] = true
			}
		}
	}
	for key := range tx.credSeen {
		before := tx.creds[key]
		live := s.creds[key]
		if before == nil || live == nil || !reflect.DeepEqual(before, live) {
			d.Creds[key] = true
			if live != nil {
				d.Accounts[live.PlayerID] = true
			}
		}
	}
	for id := range tx.allianceIn {
		before := tx.alliances[id]
		live := s.allianceByID[id]
		if before == nil || live == nil || !reflect.DeepEqual(before, live) {
			d.Alliances[id] = true
		}
	}
	for pid := range tx.friendSeen {
		before, hadBefore := tx.friends[pid]
		live, hasNow := s.friends[pid]
		if hadBefore != hasNow || !reflect.DeepEqual(before, live) {
			d.Friends[pid] = true
		}
	}
	if tx.battlesTaken {
		known := make(map[string]bool, len(tx.battles))
		for _, b := range tx.battles {
			known[b.BattleID] = true
		}
		for _, b := range s.battles {
			if !known[b.BattleID] {
				d.Battles[b.BattleID] = true
			}
		}
	}
	if m := s.Meta; m != nil {
		diffKeyed(tx.core.bonds, m.Bonds, d.Bonds)
		diffKeyed(tx.core.reactors, m.Reactors, d.Reactors)
		for k, v := range m.Receipts {
			if old, ok := tx.core.receipts[k]; !ok || old != v {
				d.Receipts[k] = true
			}
		}
		for k := range tx.core.receipts {
			if _, ok := m.Receipts[k]; !ok {
				d.Receipts[k] = true
			}
		}
		if !reflect.DeepEqual(tx.core.arenas, m.Arenas) || !reflect.DeepEqual(tx.core.allocations, m.Allocations) {
			d.Core = true
		}
	}
	if s.playerSeq != tx.core.playerSeq || s.allianceSeq != tx.core.allianceSeq || s.teamSeq != tx.core.teamSeq {
		d.Core = true
	}
	if !d.Core && !reflect.DeepEqual(tx.core.teams, s.teams) {
		d.Core = true
	}
	return d
}

func diffKeyed[V any](before, live map[string]*V, out map[string]bool) {
	for k, v := range live {
		if old, ok := before[k]; !ok || !reflect.DeepEqual(old, v) {
			out[k] = true
		}
	}
	for k := range before {
		if _, ok := live[k]; !ok {
			out[k] = true
		}
	}
}

func cloneAccount(a *Account) *Account {
	if a == nil {
		return nil
	}
	cp := *a
	cp.Characters = clonePtrMap(a.Characters)
	cp.OwnedSkins = cloneMap(a.OwnedSkins)
	cp.TokenHash = cloneBytes(a.TokenHash)
	if a.DeviceTokens != nil {
		cp.DeviceTokens = make([][]byte, 0, len(a.DeviceTokens))
		for _, t := range a.DeviceTokens {
			cp.DeviceTokens = append(cp.DeviceTokens, cloneBytes(t))
		}
	}
	return &cp
}

func cloneAlliance(al *Alliance) *Alliance {
	if al == nil {
		return nil
	}
	cp := *al
	cp.Members = cloneMap(al.Members)
	cp.JoinRequests = cloneMap(al.JoinRequests)
	cp.Messages = cloneSlice(al.Messages)
	return &cp
}

func cloneTeam(t *Team) *Team {
	if t == nil {
		return nil
	}
	cp := *t
	cp.Members = clonePtrMap(t.Members)
	cp.Pending = cloneMap(t.Pending)
	cp.PendingAt = cloneMap(t.PendingAt)
	return &cp
}

func cloneArena(a *ArenaTournament) *ArenaTournament {
	if a == nil {
		return nil
	}
	cp := *a
	if a.Players != nil {
		cp.Players = make(map[int64]*ArenaPlayer, len(a.Players))
		for k, v := range a.Players {
			p := *v
			p.Slot.AugmentIDs = cloneSlice(v.Slot.AugmentIDs)
			p.Augments = cloneSlice(v.Augments)
			p.Choices = cloneSlice(v.Choices)
			p.Rerolls = cloneSlice(v.Rerolls)
			cp.Players[k] = &p
		}
	}
	cp.Teams = clonePtrMap(a.Teams)
	cp.Battles = cloneMap(a.Battles)
	if a.PendingTeams != nil {
		cp.PendingTeams = make([][]int, 0, len(a.PendingTeams))
		for _, row := range a.PendingTeams {
			cp.PendingTeams = append(cp.PendingTeams, cloneSlice(row))
		}
	}
	return &cp
}

func cloneAllocation(a *Allocation) *Allocation {
	if a == nil {
		return nil
	}
	cp := *a
	cp.FinalRanks = cloneMap(a.FinalRanks)
	if a.Players != nil {
		cp.Players = make([]BattleParticipant, len(a.Players))
		for i, p := range a.Players {
			cp.Players[i] = p
			cp.Players[i].AugmentIDs = cloneSlice(p.AugmentIDs)
		}
	}
	return &cp
}

func cloneReactor(r *Reactor) *Reactor {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Members = clonePtrMap(r.Members)
	cp.Unlocks = cloneMap(r.Unlocks)
	return &cp
}

func clonePlayerMeta(p *PlayerMeta) *PlayerMeta {
	if p == nil {
		return nil
	}
	cp := *p
	cp.TestBalance = cloneMap(p.TestBalance)
	cp.OwnedStars = cloneMap(p.OwnedStars)
	cp.OwnedDashes = cloneMap(p.OwnedDashes)
	cp.Completed = cloneMap(p.Completed)
	cp.Milestones = cloneMap(p.Milestones)
	cp.Offers = cloneMap(p.Offers)
	cp.ReactorClaims = cloneMap(p.ReactorClaims)
	cp.Mastery = clonePtrMap(p.Mastery)
	cp.Missions = clonePtrMap(p.Missions)
	if p.ResultRanks != nil {
		cp.ResultRanks = make(map[int]map[int]int, len(p.ResultRanks))
		for k, v := range p.ResultRanks {
			cp.ResultRanks[k] = cloneMap(v)
		}
	}
	return &cp
}

func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	if in == nil {
		return nil
	}
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func clonePtrMap[K comparable, V any](in map[K]*V) map[K]*V {
	if in == nil {
		return nil
	}
	out := make(map[K]*V, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		cp := *v
		out[k] = &cp
	}
	return out
}

func cloneSlice[V any](in []V) []V {
	if in == nil {
		return nil
	}
	return append(make([]V, 0, len(in)), in...)
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	return append(make([]byte, 0, len(in)), in...)
}
