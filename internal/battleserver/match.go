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
	// PlayerRanks — места по индексу в составе, если режим делит победу не по
	// командам (Кошмар, Заражение). Пусто — места считаются по командам.
	PlayerRanks map[int]int `json:",omitempty"`
	// PlayerRoles — роли в режиме (sim.Role*) по индексу; Highlights — лучшие
	// моменты боя для лога (кнопки перемотки реплея).
	PlayerRoles map[int]int     `json:",omitempty"`
	Highlights  []sim.Highlight `json:",omitempty"`
	// Cheaters — пойманные античитом; главный сервер их блокирует.
	Cheaters []CheatReport `json:",omitempty"`

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
	inputGuards       map[int]*inputGuard
	pendingWalk       map[int]protocol.PlayerAction
	pendingMu         sync.Mutex
	voice             *voiceRoom
	cheats            cheatMeter
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
		inputGuards: map[int]*inputGuard{},
		pendingWalk: map[int]protocol.PlayerAction{},
		peerSeq:     map[int]int64{},
	}
	for _, p := range parts {
		m.parts[p.Index] = p
	}
	mode := ""
	if world != nil {
		mode = world.ModeName
	}
	m.voice = newVoiceRoom(mode, m.parts)
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
	m.voice.resetPlayer(index)
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
	m.actionMu.Lock()
	defer m.actionMu.Unlock()
	m.pendingMu.Lock()
	delete(m.pendingWalk, index)
	delete(m.pendingGeneration, index)
	m.pendingMu.Unlock()
}

func (m *Match) generationOf(index int) int64 {
	m.peerMu.Lock()
	defer m.peerMu.Unlock()
	return m.generations[index]
}

func (m *Match) handleApp(index int, payload []byte) { m.handleAppGeneration(index, payload, 0) }
func (m *Match) handleAppGeneration(index int, payload []byte, generation int64) {
	id, length, ok := protocol.TryReadBattleClientFrame(payload)
	validFrame := ok && length >= 0 && len(payload) >= 6 && length <= len(payload)-6
	// Голос имеет собственные лимиты и не расходует бюджет игровых команд.
	if validFrame {
		body := payload[6 : 6+length]
		switch id {
		case protocol.UdpVoiceFrame:
			m.handleVoiceFrame(index, body, generation)
			return
		case protocol.UdpVoiceState:
			m.handleVoiceState(index, body, generation)
			return
		}
	}
	m.actionMu.Lock()
	defer m.actionMu.Unlock()
	p := m.parts[index]
	if p == nil || p.IsBot || generation != m.generationOf(index) || m.inputBlocked(index) {
		return
	}
	now := time.Now()
	g := m.inputState(index)
	if !g.allowPacket(now, len(payload)) {
		return
	}
	if !validFrame {
		m.inputError(index, "повреждённый заголовок", now)
		return
	}
	if id != protocol.UdpPlayerActions {
		return
	}
	if length > maxActionBodyBytes || len(payload) > 6+maxActionBodyBytes {
		m.inputError(index, "слишком большой пакет действий", now)
		return
	}
	actions, err := protocol.DecodePlayerActions(payload[6 : 6+length])
	if err != nil {
		m.inputError(index, "повреждённые действия", now)
		return
	}
	// Проверяем пакет целиком до любых побочных эффектов: неизвестный тип
	// не должен оставлять в очереди соседние корректные действия.
	for _, a := range actions {
		if a.Type < 0 || a.Type > maxActionType {
			m.inputError(index, "неизвестные действия", now)
			return
		}
	}
	if !g.actions.allow(now, len(actions), inputActionsPerSecond, inputActionBurst) {
		return
	}
	for _, a := range actions {
		if a.Type == protocol.ActionWalk {
			m.pendingMu.Lock()
			m.pendingWalk[index] = a
			m.pendingGeneration[index] = generation
			m.pendingMu.Unlock()
			continue
		}
		m.enqueueInput(inputItem{generation: generation, index: index, action: a})
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
			// Тестовая арена бесконечна: когда игрок ушёл, комната закрывается без итогов.
			if m.World.Endless && !m.hasHumans() {
				m.finishAborted("practice_left")
				return
			}
		}
	}
}

func (m *Match) hasHumans() bool {
	m.peerMu.Lock()
	defer m.peerMu.Unlock()
	for index, part := range m.parts {
		if part != nil && !part.IsBot && !m.departed[index] {
			return true
		}
	}
	return false
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
	m.refreshVoiceProximity()
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

	m.drainInputs()
	m.World.Advance()
}

func (m *Match) drainInputs() {
	m.actionMu.Lock()
	defer m.actionMu.Unlock()
	m.pendingMu.Lock()
	for idx, a := range m.pendingWalk {
		m.applyAction(inputItem{index: idx, generation: m.pendingGeneration[idx], action: a})
	}
	clear(m.pendingWalk)
	clear(m.pendingGeneration)
	m.pendingMu.Unlock()

	for i := 0; i < 512; i++ {
		select {
		case it := <-m.inputs:
			if g := m.inputGuards[it.index]; g != nil && g.queued > 0 {
				g.queued--
			}
			m.applyAction(it)
		default:
			return
		}
	}
}

func (m *Match) findPlayer(index int) *sim.Player {
	for _, p := range m.World.Players {
		if p.Index == index {
			return p
		}
	}
	return nil
}

// applyAction вызывается под worldMu и actionMu.
func (m *Match) applyAction(it inputItem) {
	if m.inputBlocked(it.index) || it.generation != m.generationOf(it.index) {
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
		// В Кошмаре эта кнопка — фонарь: включит ли он, решает режим (батарея).
		if !sim.ModeUltToggle(m.World, p, true) {
			p.UltActive = true
		}
	case protocol.ActionUltOff:
		if !sim.ModeUltToggle(m.World, p, false) {
			p.UltActive = false
		}
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
		Cheaters: m.cheaters(),
	}
	if abortReason == "" {
		r.Highlights = m.World.Highlights()
		if len(m.World.PlayerRoles) > 0 {
			r.PlayerRoles = map[int]int{}
			for index, role := range m.World.PlayerRoles {
				r.PlayerRoles[index] = role
			}
		}
	}
	if abortReason == "" && len(m.World.PlayerRanks) > 0 {
		r.PlayerRanks = map[int]int{}
		for index, rank := range m.World.PlayerRanks {
			r.PlayerRanks[index] = rank
		}
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
