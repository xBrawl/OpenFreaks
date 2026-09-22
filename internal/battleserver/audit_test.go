package battleserver

import (
	"net"
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/sim"
)

func auditMatch() (*Match, *sim.Player) {
	p := &sim.Player{Index: 0, PlayerID: 42, Team: 0, ObjectIndex: 0, HP: 3600, MaxHP: 3600,
		Alive: true, HeroType: "hero", BaseSpeed: 700, CollisionRadius: 100}
	w := &sim.World{Players: []*sim.Player{p}, DurationTicks: 10000, TeamCount: 2,
		MaxPlayers: 2, TeamPoints: make([]int, 2),
		Tiles: sim.TileGrid{W: 20, H: 20, PixW: 6000, PixH: 6000,
			Blocked: make([]bool, 400), BlocksProj: make([]bool, 400),
			Destructible: make([]bool, 400), Destroyed: make([]bool, 400)}}
	m := NewMatch("battle", w, []*Participant{{Index: 0, PlayerID: 42, Team: 0}}, map[int]int64{0: 123}, nil)
	return m, p
}

func auditPeer() *Peer {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1000}
	peer := newPeer(1, 1, addr, nil)
	peer.authenticated = true
	return peer
}

func TestQueuedActionFromReplacedPeerIsDiscarded(t *testing.T) {
	m, p := auditMatch()
	old := auditPeer()
	if !m.AttachPeer(0, old) {
		t.Fatal("первое соединение не встало")
	}
	generation := m.generationOf(0)

	m.inputs <- inputItem{generation: generation, index: 0, action: protocol.PlayerAction{
		Type: protocol.ActionWalk, X: 600, Y: 0}}

	fresh := auditPeer()
	if !m.AttachPeer(0, fresh) {
		t.Fatal("замена соединения не прошла")
	}
	if m.generationOf(0) == generation {
		t.Fatal("поколение команд не изменилось при смене соединения")
	}

	before := p.HasMoveTarget
	m.doTick()
	if !before && p.HasMoveTarget {
		t.Fatal("команда прошлого соединения применилась после переключения")
	}
}

func TestPendingWalkIsClearedOnPeerReplacement(t *testing.T) {
	m, _ := auditMatch()
	old := auditPeer()
	m.AttachPeer(0, old)

	m.pendingMu.Lock()
	m.pendingWalk[0] = protocol.PlayerAction{Type: protocol.ActionWalk, X: 900}
	m.pendingGeneration[0] = m.generationOf(0)
	m.pendingMu.Unlock()

	m.AttachPeer(0, auditPeer())

	m.pendingMu.Lock()
	_, present := m.pendingWalk[0]
	m.pendingMu.Unlock()
	if present {
		t.Fatal("движение прошлого соединения пережило замену peer")
	}
}

func TestStaleAttachDoesNotSupersedeNewerPeer(t *testing.T) {
	m, _ := auditMatch()
	first := auditPeer()
	second := auditPeer()

	if !m.AttachPeer(0, second) {
		t.Fatal("новое соединение не встало")
	}
	if m.AttachPeer(0, first) {
		t.Fatal("старое соединение вытеснило новое")
	}

	m.peerMu.Lock()
	current := m.peers[0]
	m.peerMu.Unlock()
	if current != second {
		t.Fatal("в слоте оказался устаревший peer")
	}
}

func TestAbortProducesTerminalResult(t *testing.T) {
	m, _ := auditMatch()
	results := make(chan *Result, 1)
	m.onResult = func(r *Result) { results <- r }

	done := make(chan struct{})
	go func() {
		m.Run(10*time.Millisecond, 50, 0, 0)
		close(done)
	}()

	time.Sleep(120 * time.Millisecond)
	m.Abort("server_shutdown")

	select {
	case r := <-results:
		if !r.Aborted || r.AbortReason != "server_shutdown" {
			t.Fatalf("результат не помечен как аварийный: %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("остановленный матч не отдал результат")
	}
	<-done
	if !m.Finished() {
		t.Fatal("матч не считается завершённым")
	}
}

func TestAbortIsIdempotent(t *testing.T) {
	m, _ := auditMatch()
	count := 0
	m.onResult = func(*Result) { count++ }
	m.finishAborted("first")
	m.finishAborted("second")
	if count != 1 {
		t.Fatalf("результат отправлен %d раз(а)", count)
	}
}

func TestKillEventsSurviveSeveralSnapshots(t *testing.T) {
	m, p := auditMatch()
	p.PendingKills = append(p.PendingKills, sim.KillEvent{VictimIndex: 1, Points: 1, Tick: m.World.TickNum})

	for i := 0; i < sim.KillRepeatTicks; i++ {
		m.sendSnapshots()
		if len(p.PendingKills) == 0 {
			t.Fatalf("событие убийства исчезло после %d-й рассылки", i+1)
		}
		m.World.TickNum++
	}

	m.sendSnapshots()
	if len(p.PendingKills) != 0 {
		t.Fatal("событие убийства повторяется дольше отведённого окна")
	}
}
