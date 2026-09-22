package mainserver

import (
	"bufio"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/freaks/openfreaks/internal/auth"
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/config"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

type BattleAllocator interface {
	Allocate(req *battleserver.AllocateRequest) (sessions map[int]int64, host string, port int, allocErr string)
}

type Server struct {
	emoteLast       map[int64]time.Time
	leaveCheckedAt  time.Time
	arenaCheckedAt  time.Time
	arenaMissing    map[string]time.Time
	listener        net.Listener
	shutdown        chan struct{}
	closeOnce       sync.Once
	deliveryMu      sync.Mutex
	staging         bool
	pendingDelivery []queuedMessage
	storageFailed   atomic.Bool
	cfg             config.Node
	tokenSecret     string
	store           *store.Store
	catalog         *content.Catalog
	catData         *content.CatalogData
	log             *slog.Logger
	battle          BattleAllocator
	advHost         string
	advPort         int

	mu       sync.RWMutex
	conns    map[int64]*Conn
	byPlayer map[int64]*Conn
	connSeq  int64

	loginLimit *loginLimiter
	linkCodes  *linkCodes

	mm *Matchmaking
}

func NewServer(cfg config.Node, st *store.Store, cat *content.Catalog, catData *content.CatalogData, battle BattleAllocator, advHost string, advPort int, log *slog.Logger, onBattleResult func(*battleserver.Result)) *Server {
	if log == nil {
		log = slog.Default()
	}
	if catData == nil {
		catData = content.SyntheticCatalog()
	}
	heroes := []int32{}
	for _, hero := range catData.Heroes() {
		heroes = append(heroes, int32(hero.Id))
	}
	st.ConfigureHeroes(heroes)
	st.ResetPresence()
	s := &Server{
		shutdown: make(chan struct{}),
		cfg:      cfg, tokenSecret: cfg.TokenSecret(),
		store: st, catalog: cat, catData: catData, battle: battle,
		advHost: advHost, advPort: advPort, log: log,
		conns: map[int64]*Conn{}, byPlayer: map[int64]*Conn{},
		loginLimit: newLoginLimiter(),
		linkCodes:  newLinkCodes(),
	}
	if granted, err := s.grantStarterUpgradeTokens(); err != nil {
		log.Error("starter upgrade-token grant failed", "err", err)
	} else if granted > 0 {
		log.Info("starter upgrade-token grant applied", "accounts", granted, "amount", store.StarterUpgradeTokens)
	}
	s.mm = NewMatchmaking(s)
	_ = onBattleResult
	return s
}

func (s *Server) Store() *store.Store { return s.store }

type Conn struct {
	ArenaInfoBattle string
	AuthedAt        time.Time
	done            chan struct{}
	doneOnce        sync.Once
	queuedBytes     atomic.Int64
	ID              int64
	Conn            net.Conn
	Server          *Server
	SendCh          chan []byte
	closed          atomic.Bool
	PlayerID        int64
	Authed          atomic.Bool
	TeamID          int64
	BattleID        string
	lastWrite       atomic.Int64
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Network.TCPBind)
	if err != nil {
		return err
	}
	s.mu.Lock()
	select {
	case <-s.shutdown:
		s.mu.Unlock()
		_ = ln.Close()
		return nil
	default:
	}
	s.listener = ln
	s.mu.Unlock()
	s.log.Info("openfreaks main listening", "bind", s.cfg.Network.TCPBind)
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-s.shutdown:
				return nil
			default:
				return err
			}
		}
		go s.handleRaw(c)
	}
}

func (s *Server) handleRaw(nc net.Conn) {
	id := atomic.AddInt64(&s.connSeq, 1)
	capacity := s.cfg.Network.MaxPendingSendMessages
	if capacity <= 0 {
		capacity = 256
	}
	capacity = min(capacity, 1024)
	c := &Conn{ID: id, Conn: nc, Server: s, SendCh: make(chan []byte, capacity), done: make(chan struct{})}
	s.mu.Lock()
	select {
	case <-s.shutdown:
		s.mu.Unlock()
		_ = nc.Close()
		return
	default:
	}
	if len(s.conns) >= 128 {
		s.mu.Unlock()
		_ = nc.Close()
		return
	}
	s.conns[id] = c
	s.mu.Unlock()
	defer s.removeConn(c)
	go c.writeLoop()
	c.readLoop()
}

func (s *Server) removeConn(c *Conn) {
	defer s.store.LockState()()
	if c.closed.Swap(true) {
		return
	}
	c.Conn.Close()
	c.doneOnce.Do(func() {
		if c.done != nil {
			close(c.done)
		}
	})
	s.mu.Lock()
	delete(s.conns, c.ID)
	currentSession := false
	if c.PlayerID != 0 {
		if cur, ok := s.byPlayer[c.PlayerID]; ok && cur == c {
			currentSession = true
			delete(s.byPlayer, c.PlayerID)
		}
	}
	s.mu.Unlock()
	if currentSession {
		s.store.SetOnline(c.PlayerID, false)
		if s.mm != nil {
			s.mm.Cancel(c.PlayerID)
		}
		if t := s.store.TeamForPlayer(c.PlayerID); t != nil {
			t.Members[c.PlayerID].State = 0
			s.broadcastTeamUpdate(t.ID)
		}
	}
	select {
	case <-c.SendCh:
	default:
	}
}

func (s *Server) bindSession(c *Conn, playerID int64) {
	s.mu.Lock()
	if old, ok := s.byPlayer[playerID]; ok && old != c {
		old.Send(protocol.SDisconnected, protocol.EncodeSingleIgnoredByte())
		if old.Conn != nil {
			old.Conn.Close()
		}
	}

	if c.PlayerID != 0 && c.PlayerID != playerID {
		if cur, ok := s.byPlayer[c.PlayerID]; ok && cur == c {
			delete(s.byPlayer, c.PlayerID)
		}
	}
	s.byPlayer[playerID] = c
	s.mu.Unlock()
	c.PlayerID = playerID
	c.AuthedAt = time.Now()
	c.Authed.Store(true)
	if t := s.store.TeamForPlayer(playerID); t != nil {
		c.TeamID = t.ID
		t.Members[playerID].State = 1
	}
	s.store.SetOnline(playerID, true)
}

func (s *Server) ConnByPlayer(pid int64) (*Conn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.byPlayer[pid]
	return c, ok
}

func (c *Conn) Send(id int16, payload []byte) bool {
	if c.closed.Load() {
		return false
	}
	pkt := protocol.EncodeTCPFrame(id, payload)
	if c.Server != nil {
		s := c.Server
		s.deliveryMu.Lock()
		if s.staging {
			if len(s.pendingDelivery) >= 8192 {
				s.deliveryMu.Unlock()
				return false
			}
			s.pendingDelivery = append(s.pendingDelivery, queuedMessage{c, pkt})
			s.deliveryMu.Unlock()
			return true
		}
		s.deliveryMu.Unlock()
	}
	return c.enqueue(pkt)
}
func (c *Conn) enqueue(pkt []byte) bool {
	if c.closed.Load() {
		return false
	}
	limit := int64(4 << 20)
	if c.Server != nil && c.Server.cfg.Network.MaxPendingSendBytes > 0 {
		limit = c.Server.cfg.Network.MaxPendingSendBytes
	}
	if c.queuedBytes.Add(int64(len(pkt))) > limit {
		c.queuedBytes.Add(-int64(len(pkt)))
		if c.Conn != nil {
			_ = c.Conn.Close()
		}
		return false
	}
	select {
	case c.SendCh <- pkt:
		return true
	case <-c.done:
		c.queuedBytes.Add(-int64(len(pkt)))
		return false
	default:
		c.queuedBytes.Add(-int64(len(pkt)))
		if c.Conn != nil {
			_ = c.Conn.Close()
		}
		return false
	}
}

func (c *Conn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case pkt := <-c.SendCh:
			c.queuedBytes.Add(-int64(len(pkt)))
			_ = c.Conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			for len(pkt) > 0 {
				n, err := c.Conn.Write(pkt)
				if err != nil || n == 0 {
					_ = c.Conn.Close()
					return
				}
				pkt = pkt[n:]
			}
		}
	}
}

func (s *Server) Close() {
	s.closeOnce.Do(func() {
		if s.shutdown != nil {
			close(s.shutdown)
		}
		if s.mm != nil {
			s.mm.Stop()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.listener != nil {
			_ = s.listener.Close()
		}
		for _, c := range s.conns {
			if c.Conn != nil {
				_ = c.Conn.Close()
			}
		}
	})
}

func (c *Conn) readLoop() {
	s := c.Server
	nc := c.Conn
	_ = nc.SetDeadline(time.Now().Add(time.Duration(s.cfg.Network.TCPHandshakeTimeoutSec) * time.Second))
	rd := bufio.NewReaderSize(nc, 64*1024)
	header := make([]byte, protocol.TCPHeaderSize)
	first := true
	maxFrame := s.cfg.Network.TCPMaxFrameBytes
	if maxFrame <= 0 {
		maxFrame = protocol.MaxTCPFramePayload
	}
	for {
		if _, err := readFull(rd, header); err != nil {
			s.log.Info("main connection closed", "reason", err.Error())
			return
		}
		id := int16(binary.BigEndian.Uint16(header[0:2]))
		n := int(binary.BigEndian.Uint32(header[2:6]))
		if n < 0 || n > maxFrame || n > protocol.MaxTCPFramePayload {
			return
		}
		payload := make([]byte, n)
		if _, err := readFull(rd, payload); err != nil {
			return
		}
		if first {
			first = false
			_ = nc.SetDeadline(time.Now().Add(time.Duration(s.cfg.Network.TCPReadTimeoutSec) * time.Second))
		} else {
			_ = nc.SetDeadline(time.Now().Add(time.Duration(s.cfg.Network.TCPReadTimeoutSec) * time.Second))
		}
		if !s.dispatch(c, id, payload) {
			return
		}
	}
}

func readFull(r *bufio.Reader, b []byte) (int, error) {
	off := 0
	for off < len(b) {
		n, err := r.Read(b[off:])
		off += n
		if err != nil {
			return off, err
		}
	}
	return off, nil
}

func validTokenCompare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

var _ = auth.GenerateToken
var _ = fmt.Sprint
var _ = validTokenCompare
