package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Wallet struct {
	Karit, Sparks, VoidGems, UpgradeTokens, PassPoints int32
}

const StarterUpgradeTokens int32 = 150
const StarterUpgradeGrantKey = "starter-upgrade-tokens-150-v1"

type OwnedCharacter struct {
	CharacterModelID  int32
	HPLevel           int32
	DamageLevel       int32
	SpeedLevel        int32
	IsSelected        bool
	StarPowerID       int32
	DashID            int32
	UltID             int32
	IsUltUpgraded     bool
	Trophies          int32
	ProgressionPoints int
}

type Account struct {
	PlayerID   int64
	Name       string
	Onboarding int32
	Trophies   int32
	AllianceID int64
	CreatedAt  int64
	TokenHash  []byte

	DeviceTokens   [][]byte
	Wallet         Wallet
	Characters     map[int32]*OwnedCharacter
	OwnedSkins     map[int32]bool
	SelectedChar   int32
	LastOnlineUnix int64
	Online         bool
}

type Credential struct {
	LoginKey   string
	Nickname   string
	PlayerID   int64
	Salt       []byte
	Hash       []byte
	Iterations int
}

type Alliance struct {
	ID           int64
	Name         string
	Description  string
	Type         int32
	CreatedAt    int64
	Members      map[int64]int32
	JoinRequests map[int64]int64
	Messages     []AllianceMessage
}

type AllianceMessage struct {
	Type         int32
	PlayerID     int64
	PlayerName   string
	RoleModelID  int32
	Message      string
	CreationTime int64
}

type Team struct {
	PendingAt  map[int64]int64
	ID         int64
	LocationID int32
	OwnerID    int64
	Members    map[int64]*TeamMemberState
	Pending    map[int64]int32
	State      int32
}

type TeamMemberState struct {
	PlayerID int64
	State    int32
}

type BattleRecord struct {
	Players      []BattleParticipant
	BattleID     string
	LocationID   int32
	ModeID       int32
	StartedAt    int64
	TotalTicks   int32
	Participants []int64
	TrophyDeltas map[int64]int32
	Ranks        map[int64]int32
}

type Store struct {
	commit   func(*Dump) error
	deferred bool
	Meta     *MetaState
	// Serializes whole main-server operations using mutable account pointers.
	// The map mutex alone cannot protect fields after GetAccount returns.
	stateMu      sync.Mutex
	mu           sync.RWMutex
	accounts     map[int64]*Account
	byName       map[string]int64
	creds        map[string]*Credential
	allianceByID map[int64]*Alliance
	allianceSeq  int64
	teams        map[int64]*Team
	teamSeq      int64
	playerSeq    int64
	battles      []BattleRecord
	friends      map[int64]map[int64]int32
	dataDir      string
	heroIDs      []int32
	// tx — журнал открытой операции. Ненулевой только между Begin и End/Rollback,
	// которые вызываются под stateMu, поэтому вложенных транзакций не бывает.
	tx *txState
}

func (s *Store) LockState() func() { s.stateMu.Lock(); return s.stateMu.Unlock }

func (s *Store) SetCommitter(commit func(*Dump) error) { s.commit = commit }
func (s *Store) DeferSaves(value bool)                 { s.deferred = value }
func (s *Store) Commit(d *Dump) error {
	if s.commit != nil {
		if err := s.commit(d); err != nil {
			return err
		}
		s.Meta.Ledger = nil
		return nil
	}
	return s.saveFile(d)
}

func (s *Store) SaveAndSnapshot() (*Dump, error) {
	defer s.LockState()()
	if err := s.Save(); err != nil {
		return nil, err
	}
	return s.DumpState(), nil
}

func New(dataDir string) *Store {
	s := &Store{
		Meta:         NewMeta(),
		accounts:     map[int64]*Account{},
		byName:       map[string]int64{},
		creds:        map[string]*Credential{},
		allianceByID: map[int64]*Alliance{},
		teams:        map[int64]*Team{},
		friends:      map[int64]map[int64]int32{},
		playerSeq:    1000,
		dataDir:      dataDir,
	}
	s.load()
	return s
}

func (s *Store) nextPlayerID() int64   { return atomic.AddInt64(&s.playerSeq, 1) }
func (s *Store) NextAllianceID() int64 { return atomic.AddInt64(&s.allianceSeq, 1) }
func (s *Store) NextTeamID() int64     { return atomic.AddInt64(&s.teamSeq, 1) + 100000 }

func upperKey(nick string) string { return strings.ToUpper(strings.TrimSpace(nick)) }

func (s *Store) CreateAccount(name string, tokenHash []byte) *Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid := s.nextPlayerID()
	now := time.Now().UnixMilli()
	a := &Account{
		PlayerID: pid, Name: name, Trophies: 0, AllianceID: -1,
		CreatedAt: now, TokenHash: tokenHash,
		Characters: map[int32]*OwnedCharacter{},
		OwnedSkins: map[int32]bool{},
		Online:     true, LastOnlineUnix: now,
		Wallet:       Wallet{Karit: 500, Sparks: 100, UpgradeTokens: StarterUpgradeTokens},
		SelectedChar: 1,
	}
	s.Meta.Player(pid).Completed[StarterUpgradeGrantKey] = now
	ids := s.heroIDs
	if len(ids) == 0 {
		ids = []int32{1, 2, 3}
	}
	a.SelectedChar = ids[0]
	for _, id := range ids {
		a.Characters[id] = &OwnedCharacter{CharacterModelID: id, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, StarPowerID: -1, UltID: -1, IsSelected: id == a.SelectedChar}
	}
	s.accounts[pid] = a
	s.noteAccount(pid, nil)
	s.Meta.notePlayer(pid, nil)
	return a
}

func (s *Store) ConfigureHeroes(ids []int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heroIDs = append([]int32(nil), ids...)
	if len(ids) == 0 {
		return
	}
	for pid, a := range s.accounts {
		s.noteAccount(pid, a)
	}
	valid := map[int32]bool{}
	for _, id := range ids {
		valid[id] = true
	}
	for _, a := range s.accounts {
		for id := range a.Characters {
			if !valid[id] {
				delete(a.Characters, id)
			}
		}
		if !valid[a.SelectedChar] {
			a.SelectedChar = ids[0]
		}
		for _, id := range ids {
			if a.Characters[id] == nil {
				a.Characters[id] = &OwnedCharacter{CharacterModelID: id, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, StarPowerID: -1, UltID: -1}
			}
			a.Characters[id].IsSelected = id == a.SelectedChar
		}
	}
}

func (s *Store) GetAccount(pid int64) (*Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[pid]
	if ok {
		s.noteAccount(pid, a)
	}
	return a, ok
}

func (s *Store) PeekAccount(pid int64) (*Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[pid]
	return a, ok
}

func (s *Store) SetTokenHash(pid int64, h []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.accounts[pid]; ok {
		s.noteAccount(pid, a)
		a.TokenHash = h
	}
}

const MaxDeviceTokens = 5

func (s *Store) AddDeviceToken(pid int64, h []byte) {
	if len(h) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[pid]
	if !ok {
		return
	}
	s.noteAccount(pid, a)
	for _, existing := range a.DeviceTokens {
		if bytes.Equal(existing, h) {
			return
		}
	}
	a.DeviceTokens = append(a.DeviceTokens, h)
	if len(a.DeviceTokens) > MaxDeviceTokens {
		a.DeviceTokens = a.DeviceTokens[len(a.DeviceTokens)-MaxDeviceTokens:]
	}
}

func (s *Store) DeviceTokenHashes(pid int64) [][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[pid]
	if !ok {
		return nil
	}
	out := make([][]byte, 0, len(a.DeviceTokens))
	for _, h := range a.DeviceTokens {
		cp := make([]byte, len(h))
		copy(cp, h)
		out = append(out, cp)
	}
	return out
}

func (s *Store) SetOnline(pid int64, online bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.accounts[pid]; ok {
		s.noteAccount(pid, a)
		a.Online = online
		a.LastOnlineUnix = time.Now().UnixMilli()
	}
}

// Socket presence/readiness cannot survive a process restart. Party membership
// remains durable and players become ready again only after connecting.
func (s *Store) ResetPresence() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid, a := range s.accounts {
		s.noteAccount(pid, a)
		a.Online = false
	}
	for _, team := range s.teams {
		team.State = 0
		if team.Pending == nil {
			team.Pending = map[int64]int32{}
		}
		if team.PendingAt == nil {
			team.PendingAt = map[int64]int64{}
		}
		for _, member := range team.Members {
			member.State = 0
		}
	}
}

func (s *Store) FindByNickname(nick string) (*Credential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := upperKey(nick)
	c, ok := s.creds[key]
	if ok {
		s.noteCred(key, c)
	}
	return c, ok
}
func (s *Store) HasCredential(pid int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, credential := range s.creds {
		if credential.PlayerID == pid {
			return true
		}
	}
	return false
}

func (s *Store) PutCredential(c *Credential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := upperKey(c.Nickname)
	s.noteCred(key, s.creds[key])
	s.creds[key] = c
	if a, ok := s.accounts[c.PlayerID]; ok {
		s.noteAccount(c.PlayerID, a)
		a.Name = c.Nickname
	}
	s.byName[key] = c.PlayerID
}

func (s *Store) IsNicknameTaken(nick string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.creds[upperKey(nick)]
	return ok
}

const (
	FriendNotFriend int32 = 0
	FriendInReq     int32 = 1
	FriendOutReq    int32 = 2
	FriendFriend    int32 = 3
)

func (s *Store) FriendStatus(a, b int64) int32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.friends[a]; ok {
		if st, ok := m[b]; ok {
			return st
		}
	}
	return 0
}

func (s *Store) AddFriendReq(a, b int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteFriends(a)
	s.noteFriends(b)
	if s.friends[a] == nil {
		s.friends[a] = map[int64]int32{}
	}
	if s.friends[b] == nil {
		s.friends[b] = map[int64]int32{}
	}
	if s.friends[a][b] == 3 {
		return
	}
	if s.friends[a][b] == 1 && s.friends[b][a] == 2 {
		s.friends[a][b], s.friends[b][a] = 3, 3
		return
	}
	s.friends[a][b] = 2
	s.friends[b][a] = 1
}

func (s *Store) RemoveFriend(a, b int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteFriends(a)
	s.noteFriends(b)
	delete(s.friends[a], b)
	delete(s.friends[b], a)
}

func (s *Store) FriendList(a int64) []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []int64
	for id := range s.friends[a] {
		out = append(out, id)
	}
	return out
}

func (s *Store) CreateAlliance(name, desc string, typ int32, ownerID int64) *Alliance {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.NextAllianceID()
	al := &Alliance{ID: id, Name: name, Description: desc, Type: typ,
		CreatedAt: time.Now().UnixMilli(), Members: map[int64]int32{ownerID: 3}, JoinRequests: map[int64]int64{}}
	s.allianceByID[id] = al
	s.noteAlliance(id, nil)
	if a, ok := s.accounts[ownerID]; ok {
		s.noteAccount(ownerID, a)
		a.AllianceID = id
	}
	return al
}

func (s *Store) GetAlliance(id int64) (*Alliance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	al, ok := s.allianceByID[id]
	if ok {
		s.noteAlliance(id, al)
	}
	return al, ok
}
func (s *Store) DeleteAlliance(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteAlliance(id, s.allianceByID[id])
	delete(s.allianceByID, id)
}

func (s *Store) ClearJoinRequests(pid int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, al := range s.allianceByID {
		if _, ok := al.JoinRequests[pid]; !ok {
			continue
		}
		s.noteAlliance(id, al)
		delete(al.JoinRequests, pid)
	}
}

func (s *Store) SearchAlliance(q string) []*Alliance {
	s.mu.Lock()
	defer s.mu.Unlock()
	q = strings.ToLower(q)
	var out []*Alliance
	for id, al := range s.allianceByID {
		if q == "" || strings.Contains(strings.ToLower(al.Name), q) {
			s.noteAlliance(id, al)
			out = append(out, al)
		}
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func (s *Store) LeaveAlliance(pid int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[pid]
	if !ok || a.AllianceID <= 0 {
		return
	}
	if al, ok := s.allianceByID[a.AllianceID]; ok {
		s.noteAlliance(a.AllianceID, al)
		delete(al.Members, pid)
	}
	s.noteAccount(pid, a)
	a.AllianceID = -1
}

func (s *Store) CreateTeam(ownerID int64, locationID int32) *Team {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &Team{ID: s.nextTeamIDLocked(), LocationID: locationID, OwnerID: ownerID,
		PendingAt: map[int64]int64{},
		Members:   map[int64]*TeamMemberState{ownerID: {PlayerID: ownerID, State: 1}},
		Pending:   map[int64]int32{}}
	s.teams[t.ID] = t
	return t
}

func (s *Store) nextTeamIDLocked() int64 {
	s.teamSeq++
	return s.teamSeq + 100000
}

func (s *Store) GetTeam(id int64) (*Team, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.teams[id]
	return t, ok
}
func (s *Store) TeamForPlayer(pid int64) *Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.teams {
		if _, ok := t.Members[pid]; ok {
			return t
		}
	}
	return nil
}

func (s *Store) ListTeams() []*Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Team
	for _, t := range s.teams {
		out = append(out, t)
	}
	return out
}

func (s *Store) DeleteTeam(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.teams, id)
}

func (s *Store) AddBattle(rec BattleRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteBattles()
	s.battles = append(s.battles, rec)
	if len(s.battles) > 200 {
		s.battles = s.battles[len(s.battles)-200:]
	}
}

func (s *Store) BattlesFor(pid int64, limit int) []BattleRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []BattleRecord
	for i := len(s.battles) - 1; i >= 0 && len(out) < limit; i-- {
		for _, p := range s.battles[i].Participants {
			if p == pid {
				out = append(out, s.battles[i])
				break
			}
		}
	}
	return out
}

type Dump struct {
	Teams       map[int64]*Team           `json:"teams"`
	Meta        *MetaState                `json:"meta"`
	Accounts    map[int64]*Account        `json:"accounts"`
	Creds       map[string]*Credential    `json:"creds"`
	Alliances   map[int64]*Alliance       `json:"alliances"`
	Friends     map[int64]map[int64]int32 `json:"friends"`
	Battles     []BattleRecord            `json:"battles"`
	PlayerSeq   int64                     `json:"playerSeq"`
	AllianceSeq int64                     `json:"allianceSeq"`
	TeamSeq     int64                     `json:"teamSeq"`

	Dirty *Dirty `json:"-"`
}

func (s *Store) LiveDump(dirty *Dirty) *Dump {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &Dump{
		Teams: s.teams, Meta: s.Meta, Accounts: s.accounts, Creds: s.creds,
		Alliances: s.allianceByID, Friends: s.friends, Battles: s.battles,
		PlayerSeq: s.playerSeq, AllianceSeq: s.allianceSeq, TeamSeq: s.teamSeq,
		Dirty: dirty,
	}
}

func (s *Store) DumpState() *Dump {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d := &Dump{
		Teams:    map[int64]*Team{},
		Meta:     cloneMeta(s.Meta),
		Accounts: map[int64]*Account{}, Creds: map[string]*Credential{},
		Alliances: map[int64]*Alliance{}, Friends: map[int64]map[int64]int32{},
		PlayerSeq: s.playerSeq, AllianceSeq: s.allianceSeq, TeamSeq: s.teamSeq,
	}
	for id, a := range s.accounts {
		cp := *a
		cp.Characters = map[int32]*OwnedCharacter{}
		for cid, ch := range a.Characters {
			chcp := *ch
			cp.Characters[cid] = &chcp
		}
		cp.OwnedSkins = map[int32]bool{}
		for sk := range a.OwnedSkins {
			cp.OwnedSkins[sk] = true
		}
		th := make([]byte, len(a.TokenHash))
		copy(th, a.TokenHash)
		cp.TokenHash = th
		cp.DeviceTokens = make([][]byte, 0, len(a.DeviceTokens))
		for _, dt := range a.DeviceTokens {
			dtcp := make([]byte, len(dt))
			copy(dtcp, dt)
			cp.DeviceTokens = append(cp.DeviceTokens, dtcp)
		}
		d.Accounts[id] = &cp
	}
	for k, c := range s.creds {
		cp := *c
		d.Creds[k] = &cp
	}
	for id, al := range s.allianceByID {
		cp := *al
		cp.Members = map[int64]int32{}
		for pid, r := range al.Members {
			cp.Members[pid] = r
		}
		cp.JoinRequests = map[int64]int64{}
		for pid, ts := range al.JoinRequests {
			cp.JoinRequests[pid] = ts
		}
		cp.Messages = append([]AllianceMessage(nil), al.Messages...)
		d.Alliances[id] = &cp
	}
	for a, m := range s.friends {
		cp := map[int64]int32{}
		for b, st := range m {
			cp[b] = st
		}
		d.Friends[a] = cp
	}
	d.Battles = append([]BattleRecord(nil), s.battles...)
	for id, t := range s.teams {
		b, _ := json.Marshal(t)
		var copy Team
		_ = json.Unmarshal(b, &copy)
		d.Teams[id] = &copy
	}
	return d
}

func (s *Store) RestoreState(d *Dump) {
	if d == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Meta = cloneMeta(d.Meta)
	if d.Teams != nil {
		s.teams = d.Teams
	} else {
		s.teams = map[int64]*Team{}
	}
	if d.Accounts != nil {
		s.accounts = d.Accounts
	}
	if d.Creds != nil {
		s.creds = d.Creds
	}
	if d.Alliances != nil {
		s.allianceByID = d.Alliances
	}
	if d.Friends != nil {
		s.friends = d.Friends
	}
	if d.Battles != nil {
		s.battles = d.Battles
	}
	if d.PlayerSeq > s.playerSeq {
		s.playerSeq = d.PlayerSeq
	}
	for id := range s.allianceByID {
		if id > s.allianceSeq {
			s.allianceSeq = id
		}
	}
	if d.AllianceSeq > s.allianceSeq {
		s.allianceSeq = d.AllianceSeq
	}
	if d.TeamSeq > s.teamSeq {
		s.teamSeq = d.TeamSeq
	}
	if s.teams == nil {
		s.teams = map[int64]*Team{}
	}
	if s.friends == nil {
		s.friends = map[int64]map[int64]int32{}
	}
}

type snapshot struct {
	Accounts  map[int64]*Account     `json:"accounts"`
	Creds     map[string]*Credential `json:"creds"`
	Alliances map[int64]*Alliance    `json:"alliances"`
	PlayerSeq int64                  `json:"playerSeq"`
}

func (s *Store) Save() error {
	if s.deferred {
		return nil
	}
	return s.Commit(s.DumpState())
}
func (s *Store) saveFile(d *Dump) error {
	if s.dataDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dataDir, "openfreaks.json.tmp")
	dst := filepath.Join(s.dataDir, "openfreaks.json")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func (s *Store) load() {
	if s.dataDir == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(s.dataDir, "openfreaks.json"))
	if err != nil {
		return
	}

	var d Dump
	if err := json.Unmarshal(b, &d); err == nil && (d.Accounts != nil || d.PlayerSeq != 0) {
		s.RestoreState(&d)
		return
	}
	var snap snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return
	}
	if snap.Accounts != nil {
		s.accounts = snap.Accounts
	}
	if snap.Creds != nil {
		s.creds = snap.Creds
	}
	if snap.Alliances != nil {
		s.allianceByID = snap.Alliances
	}
	if snap.PlayerSeq > s.playerSeq {
		s.playerSeq = snap.PlayerSeq
	}
	for id := range s.allianceByID {
		if id > s.allianceSeq {
			s.allianceSeq = id
		}
	}
}
