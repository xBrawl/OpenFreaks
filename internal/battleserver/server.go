package battleserver

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/sim"
)

type Config struct {
	TrustedProxyCIDRs []string
	AllowMegaBossBots bool
	ReplayDir         string
	UDPBind           string
	AdvertisedHost    string
	AdvertisedPort    int
	MaxPeers          int
	MaxPerAddress     int
	HandshakeTimeout  time.Duration
	PeerTimeout       time.Duration
	TickMs            int
	IntroTicks        int
	ReadinessTimeout  time.Duration
	MaxMatches        int
	MaxCatchUpTicks   int
}

type Grant struct {
	SessionID int64
	Index     int
	ExpiresAt time.Time
}

type Server struct {
	workers  sync.WaitGroup
	draining atomic.Bool
	replays  map[int64]*ReplaySession
	cfg      Config
	conn     *net.UDPConn
	log      *slog.Logger
	mu       sync.Mutex
	peers    map[int64]*Peer
	byAddr   map[string]int
	matches  map[string]*Match
	grants   map[int64]*Grant
	onResult func(*Result)
	closed   chan struct{}
	cat      *content.CatalogData
}

func NewServer(cfg Config, log *slog.Logger, onResult func(*Result)) *Server {
	return NewServerWithCatalog(cfg, log, onResult, nil)
}

func NewServerWithCatalog(cfg Config, log *slog.Logger, onResult func(*Result), cat *content.CatalogData) *Server {
	if log == nil {
		log = slog.Default()
	}
	if cat == nil {
		cat = content.SyntheticCatalog()
	}
	return &Server{
		replays: map[int64]*ReplaySession{},
		cfg:     cfg, log: log,
		peers: map[int64]*Peer{}, byAddr: map[string]int{},
		matches: map[string]*Match{}, grants: map[int64]*Grant{},
		onResult: onResult, closed: make(chan struct{}),
		cat: cat,
	}
}

func NextSessionID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	v := int64(binary.BigEndian.Uint64(b[:])) & (1<<63 - 1)
	if v == 0 {
		v = 1
	}
	return v
}

func (s *Server) GrantSessions(indexes []int, ttl time.Duration) map[int]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int]int64{}
	now := time.Now()
	for _, idx := range indexes {
		sid := NextSessionID()
		s.grants[sid] = &Grant{SessionID: sid, Index: idx, ExpiresAt: now.Add(ttl)}
		out[idx] = sid
	}
	return out
}

type AllocateRequest struct {
	ArenaRound    bool
	BattleID      string
	LocationID    int
	LocationName  string
	ModeID        int32
	DurationTicks int
	Participants  []*Participant
}

func (s *Server) Allocate(req *AllocateRequest) (sessions map[int]int64, host string, port int, allocErr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return nil, "", 0, "battle_closed"
	default:
	}
	if s.draining.Load() {
		return nil, "", 0, "battle_draining"
	}
	if req == nil || len(req.BattleID) < 1 || len(req.BattleID) > 128 {
		return nil, "", 0, "invalid_battle_id"
	}
	if existing := s.matches[req.BattleID]; existing != nil {
		return existing.SessionIDs, s.cfg.AdvertisedHost, s.cfg.AdvertisedPort, ""
	}
	maximum := s.cfg.MaxMatches
	if maximum <= 0 {
		maximum = 16
	}
	if len(s.matches) >= maximum {
		return nil, "", 0, "battle_capacity"
	}
	loc := s.cat.LocByID[req.LocationID]
	if loc == nil || (loc.Hidden && !req.ArenaRound) {
		return nil, "", 0, "unknown_location"
	}
	mode := s.cat.ModeByName[loc.Mode]
	if req.ArenaRound && (mode == nil || !s.isArenaLocation(loc.Name)) {
		return nil, "", 0, "invalid_arena_round"
	}
	if mode == nil || req.ModeID > 0 && int(req.ModeID) != mode.Id || len(req.Participants) == 0 || len(req.Participants) > mode.MaxPlayers {
		return nil, "", 0, "invalid_roster"
	}
	bp := make([]sim.BuildPlayer, 0, len(req.Participants))
	humanIDs := map[int64]bool{}
	teamSizes := map[int]int{}
	for i, p := range req.Participants {
		if p == nil || !p.TestBalance.Valid() || p.Index != i || p.Team < 0 || p.Team >= max(1, mode.TeamCount) || p.HPLevel < 0 || p.HPLevel > 16 || p.DamageLevel < 0 || p.DamageLevel > 16 || p.SpeedLevel < 0 || p.SpeedLevel > 16 {
			return nil, "", 0, "invalid_participant"
		}
		ch := s.cat.CharByID[int(p.ModelID)]

		if ch == nil || (!ch.IsHero() && !(p.IsBot && ch.Name == practiceBossModel)) {
			return nil, "", 0, "invalid_hero"
		}
		teamSizes[p.Team]++
		if teamSizes[p.Team] > max(1, mode.TeamPlayerCount) {
			return nil, "", 0, "team_capacity"
		}
		if !p.IsBot {
			if p.PlayerID <= 0 || humanIDs[p.PlayerID] {
				return nil, "", 0, "duplicate_player"
			}
			humanIDs[p.PlayerID] = true
		}
		for _, ability := range []struct {
			table string
			id    int32
		}{{"dashes", p.DashID}, {"star_powers", p.StarPowerID}, {"ultimate_upgrades", p.UltID}} {
			if ability.id > 0 && s.cat.SpecByID(ability.table, int(ability.id)).String("target_character_name") != ch.Name {
				return nil, "", 0, "invalid_loadout"
			}
		}
		for _, id := range p.AugmentIDs {
			if !s.cat.SpecByID("augments", int(id)).Exists() {
				return nil, "", 0, "invalid_augment"
			}
		}
		bp = append(bp, sim.BuildPlayer{TestBalance: p.TestBalance, AugmentIDs: p.AugmentIDs, PlayerID: p.PlayerID, IsBot: p.IsBot, Index: p.Index, Team: p.Team, Name: p.Name, CharacterID: int(p.ModelID), HPLevel: int(p.HPLevel), DamageLevel: int(p.DamageLevel), SpeedLevel: int(p.SpeedLevel), DashID: int(p.DashID), StarPowerID: int(p.StarPowerID), UltUpgradeID: int(p.UltID)})
	}
	if mode.Name == "mega_boss" {
		if len(humanIDs) < 1 || len(humanIDs) > 6 || teamSizes[0] < 3 || teamSizes[0] > 6 || (!s.cfg.AllowMegaBossBots && (len(humanIDs) < 3 || len(req.Participants) != len(humanIDs)+1)) {
			return nil, "", 0, "mega_boss_requires_three_to_six_humans"
		}
		for _, p := range req.Participants {
			ch := s.cat.CharByID[int(p.ModelID)]
			if p.Team == 1 && (!p.IsBot || ch.Name != practiceBossModel) || p.Team == 0 && (!ch.IsHero() || p.IsBot && !s.cfg.AllowMegaBossBots) {
				return nil, "", 0, "invalid_mega_boss_roster"
			}
		}
		if teamSizes[1] != 1 {
			return nil, "", 0, "invalid_mega_boss_teams"
		}
	}
	world, err := sim.BuildWorld(s.cat, sim.BuildParams{ArenaRound: req.ArenaRound, BattleID: req.BattleID, LocationID: req.LocationID, LocationName: req.LocationName, IntroTicks: s.cfg.IntroTicks, Players: bp})
	if err != nil {
		return nil, "", 0, fmt.Sprint(err)
	}
	sessions = map[int]int64{}
	expiry := time.Now().Add(time.Duration(world.TotalTicks())*50*time.Millisecond + s.cfg.ReadinessTimeout + 120*time.Second)
	for _, p := range req.Participants {
		sid := NextSessionID()
		for s.grants[sid] != nil {
			sid = NextSessionID()
		}
		sessions[p.Index] = sid
		s.grants[sid] = &Grant{SessionID: sid, Index: p.Index, ExpiresAt: expiry}
	}
	if world.Endless {
		for _, p := range world.Players {

			if p.IsBot && p.Definition.String("name") != practiceBossModel {
				p.Abandoned = true
				p.Alive = false
				p.HP = 0
			}
		}
	}
	match := NewMatch(req.BattleID, world, req.Participants, sessions, s.onResult)
	if s.cfg.ReplayDir != "" && !world.Endless {
		match.replay = &ReplayData{Version: 1, BattleID: req.BattleID, LocationID: req.LocationID, LocationName: world.LocationName, ModeID: world.ModeID, Players: req.Participants, Fingerprint: s.cat.Fingerprint}
		match.onReplay = func(data *ReplayData) {
			if err := saveReplay(s.cfg.ReplayDir, data); err != nil {
				s.log.Error("replay archive failed", "err", err)
			}
		}
	}
	s.matches[req.BattleID] = match
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		tickMs := s.cfg.TickMs
		if tickMs <= 0 {
			tickMs = 50
		}
		match.Run(s.cfg.ReadinessTimeout, tickMs, s.cfg.IntroTicks, s.cfg.MaxCatchUpTicks)

		if !s.draining.Load() {
			timer := time.NewTimer(resultLinger)
			select {
			case <-timer.C:
			case <-s.closed:
				timer.Stop()
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.matches, match.ID)
		for _, sid := range match.SessionIDs {
			delete(s.grants, sid)
			if peer := s.peers[sid]; peer != nil {
				peer.Close()
				s.byAddr[peer.Addr.IP.String()]--
				delete(s.peers, sid)
			}
		}
	}()
	return sessions, s.cfg.AdvertisedHost, s.cfg.AdvertisedPort, ""
}

const resultLinger = 15 * time.Second

func teamCount(parts []*Participant) int {
	max := 0
	seen := map[int]bool{}
	for _, p := range parts {
		if !seen[p.Team] {
			seen[p.Team] = true
			max++
		}
	}
	if max < 1 {
		return 1
	}
	return max
}

func (s *Server) SetOnResult(fn func(*Result)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onResult = fn
}

func (s *Server) MatchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.matches)
}

func (s *Server) ListenAndServe() error {
	addr, err := net.ResolveUDPAddr("udp", s.cfg.UDPBind)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	select {
	case <-s.closed:
		s.mu.Unlock()
		_ = conn.Close()
		return nil
	default:
	}
	s.conn = conn
	s.mu.Unlock()

	_ = conn.SetReadBuffer(512 * 1024)
	_ = conn.SetWriteBuffer(128 * 1024)
	go s.tickLoop()
	buf := make([]byte, 2048)
	for {
		select {
		case <-s.closed:
			return nil
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, raddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.closed:
				return nil
			default:
			}
			if e, ok := err.(net.Error); ok && e.Timeout() {
				continue
			}
			return err
		}
		if n < 10 {
			continue
		}
		dat := make([]byte, n)
		copy(dat, buf[:n])
		s.handleDatagram(dat, raddr)
	}
}

func (s *Server) Healthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return false
	default:
	}
	return s.conn != nil && !s.draining.Load()
}

func (s *Server) Drain() { s.draining.Store(true) }

// Shutdown stops admission, lets active matches finish during grace, then sends terminal results for the rest.
func (s *Server) Shutdown(grace time.Duration) {
	s.Drain()
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		running := 0
		for _, match := range s.matches {
			if !match.Finished() {
				running++
			}
		}
		s.mu.Unlock()
		if running == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.mu.Lock()
	pending := make([]*Match, 0, len(s.matches))
	for _, match := range s.matches {
		pending = append(pending, match)
	}
	s.mu.Unlock()
	for _, match := range pending {
		match.Abort("server_shutdown")
	}

	settle := time.Now().Add(2 * time.Second)
	for time.Now().Before(settle) {
		done := true
		for _, match := range pending {
			if !match.Finished() {
				done = false
				break
			}
		}
		if done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.Close()
}

func (s *Server) Wait() { s.workers.Wait() }

func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return
	default:
		close(s.closed)
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	for _, peer := range s.peers {
		peer.Close()
	}
	for _, match := range s.matches {
		match.Stop()
	}
	for _, replay := range s.replays {
		replay.stop()
	}
}

func (s *Server) tickLoop() {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-t.C:

			s.mu.Lock()
			sessions := make([]int64, 0, len(s.peers))
			peers := make([]*Peer, 0, len(s.peers))
			for sid, p := range s.peers {
				sessions = append(sessions, sid)
				peers = append(peers, p)
			}
			s.mu.Unlock()

			var dead []int
			for i, p := range peers {
				if !p.Tick() {
					dead = append(dead, i)
				}
			}

			s.mu.Lock()
			for _, i := range dead {

				if s.peers[sessions[i]] != peers[i] {
					continue
				}
				peers[i].Close()
				s.byAddr[peers[i].Addr.IP.String()]--
				delete(s.peers, sessions[i])
			}

			now := time.Now()
			for sid, g := range s.grants {
				if now.After(g.ExpiresAt) {
					delete(s.grants, sid)
				}
			}
			s.mu.Unlock()
		}
	}
}

func helloDatagram(dat []byte) (uint32, bool) {
	if len(dat) < 9+kcpOverhead+1 || dat[0] != chanReliable {
		return 0, false
	}
	body := dat[9:]
	off := 0
	for off < len(body) {
		conv, cmd, frg, _, _, sn, _, n, ok := parseSegment(body, off)
		if !ok {
			return 0, false
		}
		if cmd == kcpPUSH && sn == 0 && frg == 0 && n == 1 && body[off+kcpOverhead] == hdrHello {
			return conv, true
		}
		off += kcpOverhead + int(n)
	}
	return 0, false
}
func (s *Server) handleDatagram(dat []byte, raddr *net.UDPAddr) {
	if len(dat) < 10 || raddr == nil {
		return
	}
	channel := dat[0]
	if channel != chanReliable && channel != chanUnreliable {
		return
	}
	session := decodeSessionBE(dat, 1)
	helloConv, isHello := helloDatagram(dat)
	s.mu.Lock()
	peer := s.peers[session]
	grant := s.grants[session]
	replacement := peer != nil && isHello && (peer.web != nil || helloConv != peer.Conv)
	if peer != nil && !replacement && peer.Addr.String() != raddr.String() {
		s.mu.Unlock()
		return
	}
	if peer == nil || replacement {
		if !isHello || grant == nil || time.Now().After(grant.ExpiresAt) {
			s.mu.Unlock()
			return
		}
		var match *Match
		for _, m := range s.matches {
			if id, ok := m.SessionIDs[grant.Index]; ok && id == session {
				match = m
				break
			}
		}
		replay := s.replays[session]
		if match == nil && replay == nil {
			s.mu.Unlock()
			return
		}
		maxPeers := s.cfg.MaxPeers
		if maxPeers <= 0 {
			maxPeers = 64
		}
		maxAddress := s.cfg.MaxPerAddress
		if maxAddress <= 0 {
			maxAddress = 16
		}
		key := raddr.IP.String()
		used := s.byAddr[key]
		if replacement && peer.Addr.IP.String() == key {
			used--
		}
		if (!replacement && len(s.peers) >= maxPeers) || used >= maxAddress {
			s.mu.Unlock()
			return
		}
		if peer != nil {
			peer.Close()
			s.byAddr[peer.Addr.IP.String()]--
		}
		peer = newPeer(session, helloConv, raddr, s.conn)
		if s.cfg.PeerTimeout > 0 {
			peer.timeoutMs = s.cfg.PeerTimeout.Milliseconds()
		}
		if s.cfg.HandshakeTimeout > 0 {
			peer.handshakeTimeoutMs = s.cfg.HandshakeTimeout.Milliseconds()
		}
		s.peers[session] = peer
		s.byAddr[key]++
		s.mu.Unlock()
		if match != nil {

			if !match.AttachPeer(grant.Index, peer) {
				peer.Close()
				s.mu.Lock()
				if s.peers[session] == peer {
					delete(s.peers, session)
					s.byAddr[peer.Addr.IP.String()]--
				}
				s.mu.Unlock()
				return
			}
		} else {
			replay.attach(peer)
		}
		peer.SendHello()
	} else {
		s.mu.Unlock()
	}
	switch channel {
	case chanReliable:
		peer.handleReliable(dat[9:])
	case chanUnreliable:
		if len(dat) < 14 || binary.LittleEndian.Uint32(dat[9:]) != peer.Conv {
			return
		}
		peer.handleUnreliable(dat[13], dat[14:])
	}
}

func (s *Server) findMatchBySession(sess int64) *Match {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.matches {
		for _, sid := range m.SessionIDs {
			if sid == sess {
				return m
			}
		}
	}
	return nil
}

func (s *Server) isArenaLocation(name string) bool {
	for _, l := range s.cat.Locations {
		for _, round := range l.RoundLocations {
			if round == name {
				return true
			}
		}
	}
	return false
}
