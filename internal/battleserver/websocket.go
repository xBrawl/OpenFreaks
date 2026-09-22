package battleserver

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/coder/websocket"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/webtransport"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const maxWebBattleFrame = 64 << 10
const webBattleWriteTimeout = 15 * time.Second

type webPeer struct {
	conn     *websocket.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	control  chan []byte
	snapshot chan []byte
}

func (w *webPeer) send(data []byte, snapshot bool) bool {
	if len(data) > maxReliableMessage {
		return false
	}
	select {
	case <-w.ctx.Done():
		return false
	default:
	}
	copy := append([]byte(nil), data...)
	if snapshot {
		select {
		case <-w.snapshot:
		default:
		}
		select {
		case w.snapshot <- copy:
			return true
		default:
			return false
		}
	}
	select {
	case w.control <- copy:
		return true
	default:
		return false
	}
}
func (w *webPeer) writeLoop(p *Peer) {
	defer w.cancel()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		var data []byte
		select {
		case data = <-w.control:
		default:
		}
		if data == nil {
			select {
			case <-w.ctx.Done():
				return
			case data = <-w.control:
			case data = <-w.snapshot:
			case <-ticker.C:
				ping, cancel := context.WithTimeout(w.ctx, webBattleWriteTimeout)
				err := w.conn.Ping(ping)
				cancel()
				if err != nil {
					return
				}
				p.mu.Lock()
				p.lastAppRecv = nowMs()
				p.mu.Unlock()
				continue
			}
		}
		ctx, cancel := context.WithTimeout(w.ctx, webBattleWriteTimeout)
		err := w.conn.Write(ctx, websocket.MessageBinary, data)
		cancel()
		if err != nil {
			slog.Info("battle websocket write closed", "reason", err.Error())
			return
		}
	}
}
func (s *Server) attachWeb(session int64, addr *net.UDPAddr, transport *webPeer) (*Peer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return nil, errors.New("closed")
	default:
	}
	if s.draining.Load() {
		return nil, errors.New("draining")
	}
	grant := s.grants[session]
	if grant == nil || time.Now().After(grant.ExpiresAt) {
		return nil, errors.New("invalid grant")
	}
	var match *Match
	for _, m := range s.matches {
		if m.SessionIDs[grant.Index] == session {
			match = m
			break
		}
	}
	replay := s.replays[session]
	if match == nil && replay == nil {
		return nil, errors.New("expired room")
	}
	old := s.peers[session]
	maximum := s.cfg.MaxPeers
	if maximum <= 0 {
		maximum = 64
	}
	perIP := s.cfg.MaxPerAddress
	if perIP <= 0 {
		perIP = 32
	}
	used := s.byAddr[addr.IP.String()]
	if old != nil && old.Addr.IP.Equal(addr.IP) {
		used--
	}
	if (old == nil && len(s.peers) >= maximum) || used >= perIP {
		return nil, errors.New("capacity")
	}
	if old != nil {
		old.Close()
		s.byAddr[old.Addr.IP.String()]--
	}
	peer := &Peer{SessionID: session, Addr: addr, web: transport, createdAt: nowMs(), lastAppRecv: nowMs(), installSeq: nextPeerInstallSeq()}
	s.peers[session] = peer
	s.byAddr[addr.IP.String()]++
	if match != nil {
		if !match.AttachPeer(grant.Index, peer) {
			peer.Close()
			delete(s.peers, session)
			s.byAddr[addr.IP.String()]--
			return nil, errors.New("superseded")
		}
	} else {
		replay.attach(peer)
	}
	return peer, nil
}
func (s *Server) detachWeb(peer *Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer.Close()
	if s.peers[peer.SessionID] == peer {
		delete(s.peers, peer.SessionID)
		s.byAddr[peer.Addr.IP.String()]--
	}
}

// First binary message: the signed-positive session ID encoded as 8-byte BE.
// Reply: one byte 0x01. Then raw existing battle application frames, without
// UDP envelope or KCP headers. Never put a bearer session ID in the URL.
func (s *Server) WebSocketHandler(parent context.Context, origins []string) http.Handler {
	accept := &webtransport.Acceptor{Origins: origins}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, ctx, cleanup, ok := accept.Accept(parent, w, r, "openfreaks.battle.v1")
		if !ok {
			return
		}
		defer cleanup()
		ws.SetReadLimit(maxWebBattleFrame)
		auth, cancel := context.WithTimeout(ctx, 5*time.Second)
		kind, data, err := ws.Read(auth)
		cancel()
		if err != nil || kind != websocket.MessageBinary || len(data) != 8 {
			return
		}
		session := int64(binary.BigEndian.Uint64(data))
		if session <= 0 {
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return
		}
		webCtx, webCancel := context.WithCancel(ctx)
		defer webCancel()
		closeOnCancel := context.AfterFunc(webCtx, func() { ws.CloseNow() })
		defer closeOnCancel()
		transport := &webPeer{conn: ws, ctx: webCtx, cancel: webCancel, control: make(chan []byte, 64), snapshot: make(chan []byte, 1)}
		peer, err := s.attachWeb(session, &net.UDPAddr{IP: ip}, transport)
		if err != nil {
			return
		}
		defer s.detachWeb(peer)
		ack, ackCancel := context.WithTimeout(webCtx, 5*time.Second)
		err = ws.Write(ack, websocket.MessageBinary, []byte{1})
		ackCancel()
		if err != nil {
			return
		}
		peer.mu.Lock()
		if peer.closed {
			peer.mu.Unlock()
			return
		}
		peer.authenticated = true
		peer.mu.Unlock()
		go transport.writeLoop(peer)
		for {
			kind, data, err = ws.Read(webCtx)
			if err != nil {
				slog.Info("battle websocket read closed", "reason", err.Error())
				return
			}
			if kind != websocket.MessageBinary || len(data) < 6 {
				return
			}
			id, size, valid := protocol.TryReadBattleClientFrame(data)
			if !valid || size != len(data)-6 {
				return
			}

			if id != protocol.UdpPlayerActions {
				return
			}
			if _, err = protocol.DecodePlayerActions(data[6:]); err != nil {
				return
			}
			peer.mu.Lock()
			callback := peer.onApp
			closed := peer.closed
			peer.lastAppRecv = nowMs()
			peer.mu.Unlock()
			if closed {
				return
			}
			if callback != nil {
				callback(peer, data, true)
			}
		}
	})
}
