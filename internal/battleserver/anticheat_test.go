package battleserver

import (
	"sync"
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/protocol"
)

func TestProtocolErrorsBlockInputWithoutAccountBan(t *testing.T) {
	m := voiceMatch(0, 1)
	for i := 0; i < 50; i++ {
		voiceSend(m, 0, protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{{Type: protocol.ActionWalk, X: 100, Y: 200}}))
	}
	for i := 0; i < inputErrorThreshold; i++ {
		if i%2 == 0 {
			voiceSend(m, 1, protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{{Type: 9}}))
		} else {
			voiceSend(m, 1, protocol.UdpPlayerActions, []byte{0xFF})
		}
	}
	if m.inputBlocked(0) || !m.inputBlocked(1) {
		t.Fatal("input block must affect only the offending player")
	}
	if got := m.cheaters(); len(got) != 0 {
		t.Fatalf("protocol errors must not cause account bans: %+v", got)
	}
	voiceSend(m, 1, protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{{Type: protocol.ActionWalk, X: 500}}))
	if _, ok := m.pendingWalk[1]; ok {
		t.Fatal("blocked input was accepted")
	}
}

func TestInvalidActionPacketIsAtomic(t *testing.T) {
	m := voiceMatch(0)
	actions := []protocol.PlayerAction{{Type: protocol.ActionWalk, X: 100}, {Type: protocol.ActionUltOn}}
	for i := 0; i < 10; i++ {
		actions = append(actions, protocol.PlayerAction{Type: 9})
	}
	voiceSend(m, 0, protocol.UdpPlayerActions, protocol.EncodePlayerActions(actions))
	g := m.inputState(0)
	if g.errorNext != 1 || m.inputBlocked(0) {
		t.Fatal("multiple unknown actions must count as one bad packet")
	}
	if len(m.inputs) != 0 || len(m.pendingWalk) != 0 {
		t.Fatal("invalid packet partially enqueued actions")
	}
}

func TestInputErrorSlidingWindow(t *testing.T) {
	m := voiceMatch(0)
	now := time.Now()
	// Five recent errors straddle the first error's ten-second boundary.
	for _, d := range []time.Duration{0, 9 * time.Second, 9100 * time.Millisecond, 9200 * time.Millisecond, 10100 * time.Millisecond} {
		m.inputError(0, "test", now.Add(d))
	}
	if m.inputBlocked(0) {
		t.Fatal("only four errors remain in the window")
	}
	m.inputError(0, "test", now.Add(10200*time.Millisecond))
	if !m.inputBlocked(0) {
		t.Fatal("five recent errors must block input across a window boundary")
	}
}

func TestIsolatedInputErrorsDoNotAccumulate(t *testing.T) {
	m := voiceMatch(0)
	now := time.Now()
	for i := 0; i < 20; i++ {
		m.inputError(0, "test", now.Add(time.Duration(i)*inputErrorWindow))
	}
	if m.inputBlocked(0) || len(m.cheaters()) != 0 {
		t.Fatal("isolated errors accumulated across the match")
	}
}

func TestInputBucketBurstRefillAndClockRollback(t *testing.T) {
	var b inputBucket
	now := time.Now()
	if !b.allow(now, inputActionBurst, inputActionsPerSecond, inputActionBurst) {
		t.Fatal("initial network burst rejected")
	}
	if b.allow(now, 1, inputActionsPerSecond, inputActionBurst) || b.allow(now.Add(-time.Second), 1, inputActionsPerSecond, inputActionBurst) {
		t.Fatal("exhausted budget accepted input")
	}
	if !b.allow(now.Add(time.Second), inputActionsPerSecond, inputActionsPerSecond, inputActionBurst) {
		t.Fatal("budget did not refill")
	}
	if b.allow(now.Add(time.Second), 1, inputActionsPerSecond, inputActionBurst) {
		t.Fatal("clock rollback created extra tokens")
	}
	if !b.allow(now.Add(time.Hour), inputActionBurst, inputActionsPerSecond, inputActionBurst) || b.allow(now.Add(time.Hour), 1, inputActionsPerSecond, inputActionBurst) {
		t.Fatal("idle budget must be capped at burst capacity")
	}
}

func TestInputPacketAndByteLimits(t *testing.T) {
	now := time.Now()
	var packets inputGuard
	for i := 0; i < inputPacketBurst; i++ {
		if !packets.allowPacket(now, 6) {
			t.Fatalf("packet %d rejected before limit", i)
		}
	}
	if packets.allowPacket(now, 6) {
		t.Fatal("empty packets bypassed packet budget")
	}
	var bytes inputGuard
	if !bytes.allowPacket(now, inputByteBurst) || bytes.allowPacket(now, 1) {
		t.Fatal("byte budget not enforced")
	}
	if !bytes.allowPacket(now.Add(time.Second), inputBytesPerSecond) {
		t.Fatal("byte budget did not refill")
	}
}

func TestAnticheatIgnoresRepeatedNetworkBursts(t *testing.T) {
	m := voiceMatch(0)
	now := time.Now()
	g := m.inputState(0)
	for i := 0; i < 20; i++ {
		at := now.Add(time.Duration(i) * 5 * time.Second)
		if !g.actions.allow(at, inputActionBurst, inputActionsPerSecond, inputActionBurst) {
			t.Fatal("spaced burst rejected")
		}
		if g.actions.allow(at, 1, inputActionsPerSecond, inputActionBurst) {
			t.Fatal("flood exceeded action budget")
		}
	}
	if m.inputBlocked(0) || len(m.cheaters()) != 0 {
		t.Fatal("network bursts caused a penalty")
	}
}

func TestActionBudgetRejectsPacketBeforeEnqueue(t *testing.T) {
	m := voiceMatch(0)
	g := m.inputState(0)
	g.actions = inputBucket{at: time.Now().Add(time.Hour)}
	voiceSend(m, 0, protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{{Type: protocol.ActionWalk}, {Type: protocol.ActionUltOn}}))
	if len(m.inputs) != 0 || len(m.pendingWalk) != 0 || m.inputBlocked(0) {
		t.Fatal("rate-limited packet must be dropped without enqueueing or punishment")
	}
}

func TestInputQueueReservesOtherPlayersCapacity(t *testing.T) {
	m := voiceMatch(0, 1)
	m.inputs = make(chan inputItem, 4)
	for i := 0; i < 100; i++ {
		m.enqueueInput(inputItem{index: 0, action: protocol.PlayerAction{X: i}})
	}
	m.enqueueInput(inputItem{index: 1, action: protocol.PlayerAction{X: 42}})
	if len(m.inputs) != 3 || m.inputState(0).queued != 2 || m.inputState(1).queued != 1 {
		t.Fatal("one player exhausted another player's queue capacity")
	}
	if first := <-m.inputs; first.index != 0 || first.action.X != 0 {
		t.Fatal("queue overflow evicted previously accepted input")
	}
}

func TestFullInputQueueDoesNotEvict(t *testing.T) {
	m := voiceMatch(0)
	m.inputs = make(chan inputItem, 1)
	m.inputs <- inputItem{index: 99}
	m.enqueueInput(inputItem{index: 0})
	if got := <-m.inputs; got.index != 99 || m.inputState(0).queued != 0 {
		t.Fatal("full queue evicted an existing item or leaked quota")
	}
}

func TestBlockedPlayerQueuedInputIsDiscarded(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "protocol", true: "confirmed"}[confirmed], func(t *testing.T) {
			m, p := auditMatch()
			voiceSend(m, 0, protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{
				{Type: protocol.ActionWalk, X: 900}, {Type: protocol.ActionUltOn},
			}))
			if len(m.inputs) != 1 || len(m.pendingWalk) != 1 {
				t.Fatal("test input was not queued")
			}
			if confirmed {
				m.cheat(0, "confirmed test violation", cheatThreshold)
			} else {
				for i := 0; i < inputErrorThreshold-1; i++ {
					voiceSend(m, 0, protocol.UdpPlayerActions, []byte{0xFF})
				}
				// Reaching the threshold inside this packet must not enqueue its valid actions.
				voiceSend(m, 0, protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{
					{Type: 9}, {Type: protocol.ActionUltOn}, {Type: protocol.ActionWalk, X: 1200},
				}))
			}
			m.doTick()
			if p.HasMoveTarget || p.UltActive || len(m.pendingWalk) != 0 || len(m.inputs) != 0 || m.inputState(0).queued != 0 {
				t.Fatal("blocked player's queued input executed or quota was not released")
			}
			want := 0
			if confirmed {
				want = 1
			}
			if len(m.cheaters()) != want {
				t.Fatal("account reports did not distinguish confirmed violations")
			}
		})
	}
}

func TestStalePeerCannotPenalizeReplacement(t *testing.T) {
	m, _ := auditMatch()
	m.AttachPeer(0, auditPeer())
	old := m.generationOf(0)
	m.AttachPeer(0, auditPeer())
	for i := 0; i < inputErrorThreshold; i++ {
		m.handleAppGeneration(0, []byte{0xFF}, old)
	}
	if m.inputGuards[0] != nil || m.inputBlocked(0) {
		t.Fatal("stale peer consumed budget or penalized replacement")
	}
}

func TestReconnectPreservesInputGuard(t *testing.T) {
	m, _ := auditMatch()
	g := m.inputState(0)
	g.packets = inputBucket{at: time.Now(), tokens: 7}
	g.blocked = true
	m.AttachPeer(0, auditPeer())
	if m.inputState(0) != g || !m.inputBlocked(0) || g.packets.tokens != 7 {
		t.Fatal("reconnect reset match input protection")
	}
}

func TestMalformedFramesAndOversizedActions(t *testing.T) {
	for name, payload := range map[string][]byte{
		"short":     {1},
		"truncated": protocol.EncodeBattleClientFrame(protocol.UdpPlayerActions, []byte{1})[:6],
		"oversized": protocol.EncodeBattleClientFrame(protocol.UdpPlayerActions, make([]byte, maxActionBodyBytes+1)),
	} {
		t.Run(name, func(t *testing.T) {
			m := voiceMatch(0)
			for i := 0; i < inputErrorThreshold; i++ {
				m.handleAppGeneration(0, payload, 0)
			}
			if !m.inputBlocked(0) || len(m.cheaters()) != 0 {
				t.Fatal("invalid frames must block match input, not ban the account")
			}
		})
	}
}

func TestUnknownParticipantDoesNotAllocateInputState(t *testing.T) {
	m := voiceMatch(0)
	m.handleAppGeneration(999, []byte{1}, 0)
	if len(m.inputGuards) != 0 {
		t.Fatal("unknown participant allocated input state")
	}
}

func TestInputGuardConcurrentReceiveAndTick(t *testing.T) {
	m, _ := auditMatch()
	payload := protocol.EncodeBattleClientFrame(protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{
		{Type: protocol.ActionWalk, X: 900}, {Type: protocol.ActionUltOn},
	}))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			m.handleAppGeneration(0, payload, 0)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			m.doTick()
		}
	}()
	wg.Wait()
	m.doTick()
	if m.inputState(0).queued != 0 {
		t.Fatal("queue quota leaked")
	}
}

// Честный клиент на мониторе 144 Гц шлёт ходьбу каждый кадр и иногда атаку —
// ни один пакет не должен упереться в бюджет.
func TestHighRefreshClientIsNeverThrottled(t *testing.T) {
	var g inputGuard
	now := time.Now()
	for frame := 0; frame < 144*10; frame++ {
		at := now.Add(time.Duration(frame) * time.Second / 144)
		actions := 1
		if frame%10 == 0 {
			actions = 2
		}
		if !g.allowPacket(at, 6+12*actions) || !g.actions.allow(at, actions, inputActionsPerSecond, inputActionBurst) {
			t.Fatalf("frame %d throttled", frame)
		}
	}
}
