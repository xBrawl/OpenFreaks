package battleserver

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/voicedsp"
)

func liveWav(t *testing.T, path string) []float32 {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	at := 12
	for at+8 <= len(b) {
		size := int(binary.LittleEndian.Uint32(b[at+4:]))
		if string(b[at:at+4]) == "data" {
			d := b[at+8 : min(len(b), at+8+size)]
			out := make([]float32, len(d)/2)
			for i := range out {
				out[i] = float32(int16(binary.LittleEndian.Uint16(d[2*i:]))) / 32768
			}
			return out
		}
		at += 8 + size
	}
	t.Fatal("no data")
	return nil
}

func frameRMS(pcm []float32) float64 {
	var e float64
	for _, v := range pcm {
		e += float64(v) * float64(v)
	}
	return math.Sqrt(e / float64(len(pcm)))
}

// Разговор двух союзников без эха в реальном темпе: сервер не должен глотать речь.
func TestLiveVoiceConversationPasses(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	a := liveWav(t, "../voicedsp/testdata/far.wav")
	b := liveWav(t, "../voicedsp/testdata/near.wav")
	m := voiceMatch(0, 0)
	peers := []*Peer{webVoicePeer(t), webVoicePeer(t)}
	for i, p := range peers {
		p.web.voice = make(chan []byte, 4096)
		m.AttachPeer(i, p)
		voiceSend(m, i, protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
	}
	var enc [2]voicedsp.EncoderState
	var seq [2]uint16
	sent := [2]int{}
	phaseOf := [2]map[uint16]int{{}, {}}
	cur := 0
	sentPhase := [2][3]int{}
	gotPhase := [2][3]int{}
	var inRMS, outRMS [2]float64
	send := func(who int, pcm []float32) {
		if frameRMS(pcm) < .012 { // клиентский порог
			return
		}
		body := make([]byte, 2+voicedsp.FrameBytes)
		binary.LittleEndian.PutUint16(body, seq[who])
		phaseOf[who][seq[who]] = cur
		sentPhase[who][cur]++
		seq[who]++
		voicedsp.Encode(pcm, &enc[who], body[2:])
		voiceSend(m, who, protocol.UdpVoiceFrame, body)
		sent[who]++
		inRMS[who] += frameRMS(pcm)
	}
	n := voicedsp.FrameSamples
	frames := min(len(a), len(b)) / n
	// Сначала говорит A, потом B, потом оба сразу.
	for phase := 0; phase < 3; phase++ {
		cur = phase
		for f := 0; f < frames; f++ {
			if phase != 1 {
				send(0, a[f*n:(f+1)*n])
			}
			if phase != 0 {
				send(1, b[f*n:(f+1)*n])
			}
			time.Sleep(60 * time.Millisecond)
		}
	}
	got := [2]int{}
	pcm := make([]float32, n)
	for listener, p := range peers {
		for _, f := range drainVoice(p) {
			speaker := int(f[10])
			if speaker == listener {
				t.Fatal("себя слышно")
			}
			voicedsp.Decode(f[13:], pcm)
			got[speaker]++
			gotPhase[speaker][phaseOf[speaker][binary.LittleEndian.Uint16(f[11:])]]++
			outRMS[speaker] += frameRMS(pcm)
		}
	}
	for who := 0; who < 2; who++ {
		t.Logf("игрок %d: отправил %d, дошло %d, средний уровень %.3f → %.3f", who, sent[who], got[who],
			inRMS[who]/float64(max(1, sent[who])), outRMS[who]/float64(max(1, got[who])))
		t.Logf("  по фазам (A / B / оба): отправлено %v, дошло %v", sentPhase[who], gotPhase[who])
		if got[who] < sent[who]*9/10 {
			t.Errorf("игрок %d: сервер проглотил речь (%d из %d)", who, got[who], sent[who])
		}
	}
}
