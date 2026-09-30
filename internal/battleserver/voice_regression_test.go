package battleserver

import (
	"encoding/binary"
	"sync"
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

func TestVoiceReconnectRestartsCaptureWithoutResettingListenerSequence(t *testing.T) {
	m := voiceMatch(0, 0)
	a, b := webVoicePeer(t), webVoicePeer(t)
	m.AttachPeer(0, a)
	m.AttachPeer(1, b)
	voiceSend(m, 0, protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
	voiceSend(m, 1, protocol.UdpVoiceState, []byte{voiceListen})
	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(500))
	first := drainVoice(b)
	if len(first) != 1 {
		t.Fatal("initial voice missing")
	}
	oldDSP := m.voice.player(0)
	m.AttachPeer(0, webVoicePeer(t))
	if m.voice.player(0) == oldDSP {
		t.Error("reconnected microphone inherited old DSP/echo history")
	}
	if m.voice.flags[0] != 0 {
		t.Error("reconnected microphone inherited enabled flags")
	}
	voiceSend(m, 0, protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(0))
	second := drainVoice(b)
	if len(second) != 1 {
		t.Fatal("capture starting at zero is dropped after reconnect")
	}
	prev, next := binary.LittleEndian.Uint16(first[0][11:]), binary.LittleEndian.Uint16(second[0][11:])
	if int16(next-prev) <= 0 {
		t.Fatal("listener will discard the restarted sequence")
	}
}

func TestVoiceRejectsMalformedAndDepartedSender(t *testing.T) {
	m := voiceMatch(0, 0)
	a, b := webVoicePeer(t), webVoicePeer(t)
	m.AttachPeer(0, a)
	m.AttachPeer(1, b)
	voiceSend(m, 1, protocol.UdpVoiceState, []byte{voiceListen})
	voiceSend(m, 0, protocol.UdpVoiceFrame, []byte{0, 0, 0, 0})
	if len(drainVoice(b)) != 0 {
		t.Error("wrong-sized frame bypassed DSP and reached listener")
	}
	m.departed[0] = true
	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(2))
	if len(drainVoice(b)) != 0 {
		t.Error("departed sender is still audible")
	}
}

func TestVoiceWebQueueKeepsFreshestFrames(t *testing.T) {
	p := webVoicePeer(t)
	for i := byte(0); i < 20; i++ {
		p.SendVoice([]byte{i})
	}
	frames := drainVoice(p)
	if len(frames) != 8 || frames[0][0] != 12 || frames[7][0] != 19 {
		t.Fatalf("queue retained stale speech: %v", frames)
	}
}

func TestVoiceConcurrentReconnects(t *testing.T) {
	m := voiceMatch(0, 0)
	a, b := webVoicePeer(t), webVoicePeer(t)
	m.AttachPeer(0, a)
	m.AttachPeer(1, b)
	voiceSend(m, 1, protocol.UdpVoiceState, []byte{voiceListen})
	var wg sync.WaitGroup
	for worker := 0; worker < 3; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(uint16(i)))
				voiceSend(m, 0, protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
			}
		}()
	}
	for i := 0; i < 30; i++ {
		m.AttachPeer(0, webVoicePeer(t))
	}
	wg.Wait()
	drainVoice(b)
	fresh := webVoicePeer(t)
	m.AttachPeer(0, fresh)
	voiceSend(m, 0, protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(0))
	if len(drainVoice(b)) != 1 || len(drainVoice(fresh)) != 0 {
		t.Fatal("fresh connection lost its voice or received its own voice")
	}
}

func TestVoiceClosedListenerDoesNotSeedEchoReference(t *testing.T) {
	m := voiceMatch(0, 0)
	a, b := webVoicePeer(t), webVoicePeer(t)
	m.AttachPeer(0, a)
	m.AttachPeer(1, b)
	voiceSend(m, 1, protocol.UdpVoiceState, []byte{voiceListen})
	b.Close()
	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(0))
	if m.voice.dsp[1] != nil {
		t.Fatal("undelivered audio was registered as speaker playback")
	}
}
