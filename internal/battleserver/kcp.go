package battleserver

import (
	"encoding/binary"
	kcp "github.com/xtaci/kcp-go/v5"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	chanReliable         = 1
	chanUnreliable       = 2
	hdrHello             = 1
	hdrPing              = 2
	hdrPong              = 3
	hdrData              = 4
	hdrDisc              = 5
	kcpPUSH              = 81
	kcpACK               = 82
	kcpWASK              = 83
	kcpWINS              = 84
	kcpOverhead          = 24
	kcpMSS               = 1167
	maxUnreliableContent = 1190
	maxReliableMessage   = 128*kcpMSS - 1
)

func nowMs() int64                               { return time.Now().UnixMilli() }
func encodeSessionBE(b []byte, off int, v int64) { binary.BigEndian.PutUint64(b[off:], uint64(v)) }
func decodeSessionBE(b []byte, off int) int64    { return int64(binary.BigEndian.Uint64(b[off:])) }

type Peer struct {
	web                                  *webPeer
	mu                                   sync.Mutex
	SessionID                            int64
	Conv                                 uint32
	Addr                                 *net.UDPAddr
	conn                                 *net.UDPConn
	engine                               *kcp.KCP
	authenticated, closed                bool
	createdAt, lastAppRecv, lastPingSend int64
	timeoutMs, handshakeTimeoutMs        int64
	rttMs                                int64
	pendingSnapshot                      []byte
	onApp                                func(*Peer, []byte, bool)
	onHello                              func(*Peer)
	output                               func([]byte)

	installSeq int64
}

var peerInstallSeq atomic.Int64

func nextPeerInstallSeq() int64 { return peerInstallSeq.Add(1) }

func newPeer(session int64, conv uint32, addr *net.UDPAddr, conn *net.UDPConn) *Peer {
	p := &Peer{SessionID: session, Conv: conv, Addr: addr, conn: conn, createdAt: nowMs(), lastAppRecv: nowMs(), timeoutMs: 8000, handshakeTimeoutMs: 10000, installSeq: nextPeerInstallSeq()}
	p.engine = kcp.NewKCP(conv, func(buf []byte, size int) {
		packet := make([]byte, 9+size)
		packet[0] = chanReliable
		encodeSessionBE(packet, 1, p.SessionID)
		copy(packet[9:], buf[:size])
		p.writeLocked(packet)
	})
	p.engine.SetMtu(kcpMSS + kcpOverhead)
	p.engine.WndSize(512, 512)
	p.engine.NoDelay(1, 10, 2, 1)
	return p
}
func (p *Peer) IsAuthenticated() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.authenticated && !p.closed
}
func (p *Peer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.authenticated = false
	p.pendingSnapshot = nil
	p.engine = nil
	if p.web != nil {
		p.web.cancel()
	}
}
func (p *Peer) writeLocked(packet []byte) {
	if p.closed {
		return
	}
	if p.output != nil {
		p.output(append([]byte(nil), packet...))
	} else if p.conn != nil && p.Addr != nil {
		_, _ = p.conn.WriteToUDP(packet, p.Addr)
	}
}
func (p *Peer) sendLocked(payload []byte) bool {
	if p.closed || p.engine == nil || len(payload) > maxReliableMessage || p.engine.WaitSnd() >= 256 {
		return false
	}
	if p.engine.Send(payload) != 0 {
		return false
	}
	p.engine.Update()
	return true
}
func (p *Peer) SendHello() { p.mu.Lock(); defer p.mu.Unlock(); p.sendLocked([]byte{hdrHello}) }
func (p *Peer) SendReliableData(app []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.web != nil {
		if !p.closed && !p.web.send(app, false) {
			p.closed = true
			p.web.cancel()
		}
		return
	}
	payload := append([]byte{hdrData}, app...)
	if !p.sendLocked(payload) {
		p.closed = true
	}
}

// Full snapshots supersede queued snapshots. Keep only the newest snapshot when
// the connection is recovering, preserving ordered reliable control messages.
func (p *Peer) SendSnapshot(app []byte) {
	if p.web != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		if !p.closed && p.authenticated && !p.web.send(app, true) {
			p.closed = true
			p.web.cancel()
		}
		return
	}
	if len(app) <= maxUnreliableContent {
		p.SendUnreliable(app)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !p.authenticated {
		return
	}
	if len(app)+1 > maxReliableMessage {
		p.closed = true
		return
	}
	p.pendingSnapshot = append(p.pendingSnapshot[:0], app...)
	p.flushSnapshotLocked()
}
func (p *Peer) flushSnapshotLocked() {
	if p.engine == nil || len(p.pendingSnapshot) == 0 || p.engine.WaitSnd() > 32 {
		return
	}
	payload := append([]byte{hdrData}, p.pendingSnapshot...)
	if p.sendLocked(payload) {
		p.pendingSnapshot = nil
	}
}
func (p *Peer) SendUnreliable(app []byte) {
	if p.web != nil {
		p.SendSnapshot(app)
		return
	}
	if len(app) > maxUnreliableContent {
		p.SendSnapshot(app)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.authenticated || p.closed {
		return
	}
	packet := make([]byte, 10+len(app))
	packet[0] = chanUnreliable
	encodeSessionBE(packet, 1, p.SessionID)
	packet[9] = hdrData
	copy(packet[10:], app)
	p.writeLocked(packet)
}

// SendVoice отдаёт кадр голоса без гарантий: по UDP ненадёжным каналом, в
// браузер — отдельной короткой очередью. Опоздавшая речь не нужна, поэтому при
// заторе кадр пропадает, а соединение и снапшоты не страдают.
func (p *Peer) SendVoice(frame []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !p.authenticated || len(frame) > maxUnreliableContent {
		return false
	}
	if p.web != nil {
		return p.web.sendVoice(frame)
	}
	packet := make([]byte, 10+len(frame))
	packet[0] = chanUnreliable
	encodeSessionBE(packet, 1, p.SessionID)
	packet[9] = hdrData
	copy(packet[10:], frame)
	p.writeLocked(packet)
	return !p.closed
}
func (p *Peer) Tick() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := nowMs()
	if p.web != nil {
		return !p.closed && now-p.lastAppRecv < 45000
	}
	if p.closed || p.engine == nil || now-p.lastAppRecv > p.timeoutMs || !p.authenticated && now-p.createdAt > p.handshakeTimeoutMs {
		return false
	}
	if p.authenticated && now-p.lastPingSend >= 1000 {
		p.lastPingSend = now
		payload := make([]byte, 5)
		payload[0] = hdrPing
		binary.LittleEndian.PutUint32(payload[1:], uint32(now))
		if !p.sendLocked(payload) {
			return false
		}
	}
	p.engine.Update()
	p.flushSnapshotLocked()
	return true
}
func (p *Peer) handleReliable(body []byte) {

	if len(body) < kcpOverhead || len(body) > kcpMSS+kcpOverhead {
		return
	}
	off := 0
	for off < len(body) {
		conv, cmd, _, _, _, _, _, length, ok := parseSegment(body, off)
		if !ok || conv != p.Conv || length > kcpMSS || (cmd != kcpPUSH && cmd != kcpACK && cmd != kcpWASK && cmd != kcpWINS) {
			return
		}
		off += kcpOverhead + int(length)
	}
	p.mu.Lock()
	if p.closed || p.engine == nil {
		p.mu.Unlock()
		return
	}
	if p.engine.Input(body, kcp.IKCP_PACKET_REGULAR, true) != 0 {
		p.mu.Unlock()
		return
	}
	messages := [][]byte{}
	for {
		n := p.engine.PeekSize()
		if n < 0 {
			break
		}
		if n == 0 || n > maxReliableMessage {
			p.closed = true
			break
		}
		message := make([]byte, n)
		if p.engine.Recv(message) < 0 {
			break
		}
		messages = append(messages, message)
	}
	p.mu.Unlock()
	for _, message := range messages {
		p.deliverReliableMessage(message)
	}
}
func (p *Peer) deliverReliableMessage(payload []byte) {
	if len(payload) == 0 {
		return
	}
	header, body := payload[0], payload[1:]
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if header == hdrHello && len(body) == 0 {
		first := !p.authenticated
		p.authenticated = true
		p.lastAppRecv = nowMs()
		cb := p.onHello
		p.mu.Unlock()
		if first && cb != nil {
			cb(p)
		}
		return
	}
	if !p.authenticated {
		p.mu.Unlock()
		return
	}
	var callback func(*Peer, []byte, bool)
	switch header {
	case hdrPing:
		if len(body) == 4 {
			p.lastAppRecv = nowMs()
			p.sendLocked(append([]byte{hdrPong}, body...))
		}
	case hdrPong:
		if len(body) == 4 {
			p.lastAppRecv = nowMs()
			sample := int64(uint32(nowMs()) - binary.LittleEndian.Uint32(body))
			if sample <= 8000 {
				if p.rttMs == 0 {
					p.rttMs = sample
				} else {
					p.rttMs = (p.rttMs*7 + sample) / 8
				}
			}
		}
	case hdrData:
		p.lastAppRecv = nowMs()
		callback = p.onApp
	case hdrDisc:
		p.closed = true
	}
	p.mu.Unlock()
	if callback != nil {
		callback(p, append([]byte(nil), body...), true)
	}
}
func (p *Peer) handleUnreliable(header byte, payload []byte) {
	p.mu.Lock()
	if p.closed || !p.authenticated {
		p.mu.Unlock()
		return
	}
	if header == hdrDisc {
		p.closed = true
		p.mu.Unlock()
		return
	}
	if header != hdrData {
		p.mu.Unlock()
		return
	}
	p.lastAppRecv = nowMs()
	callback := p.onApp
	p.mu.Unlock()
	if callback != nil {
		callback(p, append([]byte(nil), payload...), false)
	}
}

func buildSegment(conv uint32, cmd byte, frg byte, wnd uint16, ts, sn, una uint32, payload []byte) []byte {
	out := make([]byte, kcpOverhead+len(payload))
	binary.LittleEndian.PutUint32(out[0:], conv)
	out[4] = cmd
	out[5] = frg
	binary.LittleEndian.PutUint16(out[6:], wnd)
	binary.LittleEndian.PutUint32(out[8:], ts)
	binary.LittleEndian.PutUint32(out[12:], sn)
	binary.LittleEndian.PutUint32(out[16:], una)
	binary.LittleEndian.PutUint32(out[20:], uint32(len(payload)))
	copy(out[24:], payload)
	return out
}

func parseSegment(b []byte, off int) (conv uint32, cmd, frg byte, wnd uint16, ts, sn, una uint32, ln uint32, ok bool) {
	if len(b)-off < kcpOverhead {
		return 0, 0, 0, 0, 0, 0, 0, 0, false
	}
	conv = binary.LittleEndian.Uint32(b[off:])
	cmd = b[off+4]
	frg = b[off+5]
	wnd = binary.LittleEndian.Uint16(b[off+6:])
	ts = binary.LittleEndian.Uint32(b[off+8:])
	sn = binary.LittleEndian.Uint32(b[off+12:])
	una = binary.LittleEndian.Uint32(b[off+16:])
	ln = binary.LittleEndian.Uint32(b[off+20:])
	if ln > 8*1024*1024 || len(b)-off-kcpOverhead < int(ln) {
		return 0, 0, 0, 0, 0, 0, 0, 0, false
	}
	return conv, cmd, frg, wnd, ts, sn, una, ln, true
}
