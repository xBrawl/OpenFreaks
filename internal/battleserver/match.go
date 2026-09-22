package battleserver

import (
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/sim"
)

type Participant struct {
	TestBalance content.TestBalance
	AugmentIDs  []int32
	StarPowerID int32
	PlayerID    int64
	IsBot       bool
	Index       int
	Team        int
	Name        string
	ModelID     int32
	HPLevel     int32
	DamageLevel int32
	SpeedLevel  int32
	UltID       int32
	DashID      int32
}

type Result struct {
	BattleID   string
	ModeID     int32
	FinalTick  int32
	WinnerTeam int
	TeamPoints []int
	Players    []*sim.Player
	DurationMs int64

	Aborted bool

	AbortReason string
}

type inputItem struct {
	generation int64
	index      int
	action     protocol.PlayerAction
}

type Match struct {
	worldMu           sync.Mutex
	generations       map[int]int64
	pendingGeneration map[int]int64
	departed          map[int]bool
	actionMu          sync.Mutex
	replay            *ReplayData
	onReplay          func(*ReplayData)
	stopOnce          sync.Once
	peerSeq           map[int]int64
	aborting          atomic.Bool
	abortReason       atomic.Value
	finished          atomic.Bool
	tickDebt          atomic.Int64
	snapshotWriter    *protocol.BitWriter
	ID                string
	SessionIDs        map[int]int64
	World             *sim.World
	parts             map[int]*Participant
	peers             map[int]*Peer
	peerMu            sync.Mutex
	inputs            chan inputItem
	done              chan struct{}
	result            *Result
	onResult          func(*Result)
	started           time.Time
	rateWindow        map[int][2]int64
	pendingWalk       map[int]protocol.PlayerAction
	pendingMu         sync.Mutex
}

func NewMatch(id string, world *sim.World, parts []*Participant, sessions map[int]int64, onResult func(*Result)) *Match {
	m := &Match{
		ID: id, World: world, generations: map[int]int64{}, pendingGeneration: map[int]int64{},
		departed:    map[int]bool{},
		SessionIDs:  sessions,
		parts:       map[int]*Participant{},
		peers:       map[int]*Peer{},
		inputs:      make(chan inputItem, 4096),
		done:        make(chan struct{}),
		onResult:    onResult,
		rateWindow:  map[int][2]int64{},
		pendingWalk: map[int]protocol.PlayerAction{},
		peerSeq:     map[int]int64{},
	}
	for _, p := range parts {
		m.parts[p.Index] = p
	}
	return m
}

func (m *Match) AttachPeer(index int, peer *Peer) bool {
	m.peerMu.Lock()
	if m.departed[index] {
		m.peerMu.Unlock()
		peer.Close()
		return false
	}
	if peer.installSeq > 0 && peer.installSeq < m.peerSeq[index] {
		m.peerMu.Unlock()
		return false
	}
	m.peerSeq[index] = peer.installSeq

	m.generations[index]++
	generation := m.generations[index]
	m.peers[index] = peer
	m.peerMu.Unlock()
	m.resetInputState(index)
	peer.mu.Lock()
	peer.onApp = func(pp *Peer, payload []byte, reliable bool) {
		m.handleAppGeneration(index, payload, generation)
	}
	peer.mu.Unlock()
	return true
}

func (m *Match) resetInputState(index int) {
	m.pendingMu.Lock()
	delete(m.pendingWalk, index)
	delete(m.pendingGeneration, index)
	m.pendingMu.Unlock()
	m.actionMu.Lock()
	delete(m.rateWindow, index)
	m.actionMu.Unlock()
}

func (m *Match) generationOf(index int) int64 {
	m.peerMu.Lock()
	defer m.peerMu.Unlock()
	return m.generations[index]
}

func (m *Match) handleApp(index int, payload []byte) { m.handleAppGeneration(index, payload, 0) }
func (m *Match) handleAppGeneration(index int, payload []byte, generation int64) {
	m.actionMu.Lock()
	defer m.actionMu.Unlock()
	if len(payload) < 6 {
		return
	}
	id, length, ok := protocol.TryReadBattleClientFrame(payload)
	if !ok {
		return
	}
	body := payload[6:]
	if len(body) < length {
		return
	}
	body = body[:length]
	switch id {
	case protocol.UdpClientHello:

	case protocol.UdpPlayerActions:
		actions, err := protocol.DecodePlayerActions(body)
		if err != nil {
			return
		}
		n := nowMs()
		for _, a := range actions {

			rw := m.rateWindow[index]
			if n-rw[0] > 50 {
				rw = [2]int64{n, 0}
			}
			if rw[1] >= 16 {
				continue
			}
			rw[1]++
			m.rateWindow[index] = rw
			if a.Type == protocol.ActionWalk {

				m.pendingMu.Lock()
				m.pendingWalk[index] = a
				m.pendingGeneration[index] = generation
				m.pendingMu.Unlock()
				continue
			}
			select {
			case m.inputs <- inputItem{generation: generation, index: index, action: a}:
			default:

				select {
				case <-m.inputs:
				default:
				}
				select {
				case m.inputs <- inputItem{generation: generation, index: index, action: a}:
				default:
				}
			}
		}
	}
}

// The catch-up cap prevents lost ticker events from slowing game time or causing an unbounded simulation burst.
const catchUpTicksDefault = 4

func (m *Match) Run(readinessTimeout time.Duration, tickMs int, introTicks int, maxCatchUpTicks int) {
	m.started = time.Now()

	deadline := time.Now().Add(readinessTimeout)
	for {
		ready := true
		m.peerMu.Lock()
		for idx, part := range m.parts {
			if part.IsBot {
				continue
			}
			peer, ok := m.peers[idx]
			if !ok || !peer.IsAuthenticated() {
				ready = false
				break
			}
		}
		m.peerMu.Unlock()
		if ready || time.Now().After(deadline) {
			break
		}
		select {
		case <-m.done:
			// Main closes the durable allocation only after receiving a terminal result.
			if m.aborting.Load() {
				m.finishAborted(m.reason())
			}
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	if maxCatchUpTicks <= 0 {
		maxCatchUpTicks = catchUpTicksDefault
	}
	interval := time.Duration(tickMs) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	due := time.Now()
	for {
		select {
		case <-m.done:

			if m.aborting.Load() {
				m.finishAborted(m.reason())
			}
			return
		case <-ticker.C:
			due = due.Add(interval)
			steps := 1
			if lag := time.Since(due); lag >= interval {
				extra := int(lag / interval)
				if extra > maxCatchUpTicks-1 {

					dropped := extra - (maxCatchUpTicks - 1)
					m.tickDebt.Add(int64(dropped))
					due = due.Add(time.Duration(dropped) * interval)
					extra = maxCatchUpTicks - 1
				}
				due = due.Add(time.Duration(extra) * interval)
				steps += extra
			}
			if !m.tickOnce(steps) {
				m.finishAborted(m.reason())
				return
			}
			if m.World.Finished {
				m.finish()
				return
			}
		}
	}
}

func (m *Match) TickDebt() int64 { return m.tickDebt.Load() }

func (m *Match) tickOnce(steps int) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("match tick panicked", "battle", m.ID, "tick", m.World.TickNum, "panic", r, "stack", string(debug.Stack()))
			m.abortReason.Store("tick_panic")
			ok = false
		}
	}()
	for i := 0; i < steps; i++ {
		m.doTick()
		if m.World.Finished {
			break
		}
	}
	m.sendSnapshots()
	return true
}

func (m *Match) reason() string {
	if v, _ := m.abortReason.Load().(string); v != "" {
		return v
	}
	return "shutdown"
}

func (m *Match) doTick() {
	m.worldMu.Lock()
	defer m.worldMu.Unlock()
	m.peerMu.Lock()
	for _, p := range m.World.Players {
		if p.IsBot {
			continue
		}
		peer := m.peers[p.Index]
		if m.departed[p.Index] {
			if p.Alive {
				sim.Kill(m.World, nil, p)
			}
			p.Abandoned = true
			p.BotControlled = false
			continue
		}
		if peer == nil || !peer.IsAuthenticated() {
			p.DisconnectedTicks++
			if p.DisconnectedTicks == 1 {
				p.HasMoveTarget = false
			}
			p.BotControlled = !m.World.Endless && p.DisconnectedTicks >= 40
			if m.World.Endless && p.DisconnectedTicks > 300 {
				m.departed[p.Index] = true
			}
		} else {
			if p.BotControlled {
				p.HasMoveTarget = false
			}
			p.BotControlled = false
			p.DisconnectedTicks = 0
		}
	}
	m.peerMu.Unlock()

	m.pendingMu.Lock()
	for idx, a := range m.pendingWalk {
		if p := m.findPlayer(idx); p != nil && p.Alive && m.pendingGeneration[idx] == m.generationOf(idx) {
			p.ApplyWalk(a.X, a.Y)
		}
	}
	clear(m.pendingWalk)
	m.pendingMu.Unlock()

	for i := 0; i < 512; i++ {
		select {
		case it := <-m.inputs:
			m.applyAction(it)
		default:
			goto drained
		}
	}
drained:
	m.World.Advance()
}

func (m *Match) findPlayer(index int) *sim.Player {
	for _, p := range m.World.Players {
		if p.Index == index {
			return p
		}
	}
	return nil
}

func (m *Match) applyAction(it inputItem) {
	if it.generation != m.generationOf(it.index) {
		return
	}
	p := m.findPlayer(it.index)
	if p == nil || !p.Alive || p.Abandoned || p.IsBall() {
		return
	}
	a := it.action
	switch a.Type {
	case protocol.ActionWalk:
		p.ApplyWalk(a.X, a.Y)
	case protocol.ActionWeapon, protocol.ActionUlt:
		ultimate := a.Type == protocol.ActionUlt

		if m.World.Ball != nil && p.CarryableObjectID == m.World.Ball.ObjectIndex {
			sim.ThrowBall(m.World, p, a, ultimate)
			return
		}
		slot := 0
		if ultimate && len(p.Skills) > 1 {
			slot = 1
		}
		if slot < len(p.Skills) {
			sk := p.Skills[slot]

			sim.TryCast(m.World, p, sk, a, ultimate)
		}
	case protocol.ActionUltOn:
		p.UltActive = true
	case protocol.ActionUltOff:
		p.UltActive = false
	case protocol.ActionDash:
		sim.TryDash(m.World, p, a)
	}
}

func (m *Match) sendSnapshots() {
	m.worldMu.Lock()
	defer m.worldMu.Unlock()
	m.peerMu.Lock()
	defer m.peerMu.Unlock()
	tick := m.World.TickNum
	if !m.World.Endless && m.replay != nil && len(m.replay.Frames) < maxReplayFrames {
		m.replay.Frames = append(m.replay.Frames, ReplayFrame{tick, sim.EncodeSnapshot(m.World, 0, protocol.ParticipantReplayWatcher)})
	}
	for idx, peer := range m.peers {
		if !peer.IsAuthenticated() {
			continue
		}

		if m.snapshotWriter == nil {
			m.snapshotWriter = protocol.NewBitWriter(8192)
		}
		payload := sim.EncodeSnapshotInto(m.World, idx, protocol.ParticipantPlayer, m.snapshotWriter)
		frame := protocol.EncodeBattleServerFrame(protocol.UdpBattleState, int32(tick), payload)
		peer.SendSnapshot(frame)
	}
	for _, p := range m.World.Characters() {

		kept := p.PendingKills[:0]
		for _, k := range p.PendingKills {
			if tick-k.Tick < sim.KillRepeatTicks {
				kept = append(kept, k)
			}
		}
		clear(p.PendingKills[len(kept):])
		p.PendingKills = kept

		p.DamageDeltas = p.DamageDeltas[:0]
	}
}

func (m *Match) finish() { m.emitResult("") }

// finishAborted sends a terminal result so Main can release the durable allocation without rewards.
func (m *Match) finishAborted(reason string) {
	if reason == "" {
		reason = "aborted"
	}
	m.emitResult(reason)
}

func (m *Match) emitResult(abortReason string) {
	if !m.finished.CompareAndSwap(false, true) {
		return
	}
	if abortReason != "" {
		slog.Warn("match aborted", "battle", m.ID, "reason", abortReason, "tick", m.World.TickNum)
	}
	r := &Result{
		BattleID: m.ID, ModeID: m.World.ModeID, FinalTick: m.World.TickNum,
		WinnerTeam: m.World.WinnerTeam, TeamPoints: append([]int(nil), m.World.TeamPoints...),
		Players: m.World.Players, DurationMs: int64(time.Since(m.started).Milliseconds()),
		Aborted: abortReason != "", AbortReason: abortReason,
	}
	m.result = r
	if m.replay != nil && m.onReplay != nil {
		m.onReplay(m.replay)
	}
	if m.onResult != nil {
		m.onResult(r)
	}
}

func (m *Match) Stop() {
	m.stopOnce.Do(func() { close(m.done) })
}

func (m *Match) Abort(reason string) {
	if reason != "" {
		m.abortReason.Store(reason)
	}
	m.aborting.Store(true)
	m.stopOnce.Do(func() { close(m.done) })
}

func (m *Match) Finished() bool { return m.finished.Load() }
