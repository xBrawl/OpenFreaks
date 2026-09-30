package voicedsp

import (
	"math"
	"math/rand"
	"testing"
)

func TestLimiterNeverExceedsFullScale(t *testing.T) {
	for _, x := range []float64{-1000, -6, -3, -1, 0, 1, 3, 6, 1000} {
		if y := softClip(x); y < -1 || y > 1 || math.IsNaN(float64(y)) {
			t.Errorf("limiter(%v) = %v", x, y)
		}
	}
}

func TestBackgroundDoesNotKeepSpeechBoost(t *testing.T) {
	s := NewStream()
	s.agc = 3 // A quiet speaker was just amplified; only game/background sound remains.
	random := rand.New(rand.NewSource(19))
	pcm, decoded := make([]float32, FrameSamples), make([]float32, FrameSamples)
	frame, out := make([]byte, FrameBytes), make([]byte, FrameBytes)
	var encoder EncoderState
	var powers FramePowers
	var inputEnergy, outputEnergy float64
	for f := 0; f < 100; f++ {
		for i := range pcm {
			at := float64(f*FrameSamples+i) / SampleRate
			pcm[i] = float32(.025*math.Sin(2*math.Pi*440*at) + .015*(random.Float64()-.5))
		}
		Encode(pcm, &encoder, frame)
		s.Process(frame, uint16(f), int64(f*60), nil, out, &powers)
		Decode(out, decoded)
		if f >= 70 {
			inputEnergy += energy(pcm)
			outputEnergy += energy(decoded)
		}
	}
	if s.agc > 1.2 {
		t.Errorf("background retained amplification: %f", s.agc)
	}
	if outputEnergy > inputEnergy*.3 {
		t.Errorf("stationary background too loud: ratio %f", outputEnergy/inputEnergy)
	}
}

func TestQuietSpeechSurvivesBackgroundAdaptation(t *testing.T) {
	speech := readWav(t, "testdata/near.wav")
	s := NewStream()
	pcm, decoded := make([]float32, FrameSamples), make([]float32, FrameSamples)
	frame, out := make([]byte, FrameBytes), make([]byte, FrameBytes)
	var encoder EncoderState
	var powers FramePowers
	for f := 0; f < 40; f++ {
		for i := range pcm {
			pcm[i] = float32(.005 * math.Sin(float64(f*FrameSamples+i)*.3))
		}
		Encode(pcm, &encoder, frame)
		s.Process(frame, uint16(f), int64(f*60), nil, out, &powers)
	}
	var in, processed float64
	for f := 0; (f+1)*FrameSamples <= len(speech); f++ {
		for i := range pcm {
			pcm[i] = speech[f*FrameSamples+i] * .25
		}
		Encode(pcm, &encoder, frame)
		keep, stats := s.Process(frame, uint16(f+40), int64((f+40)*60), nil, out, &powers)
		if !keep {
			t.Fatal("quiet speech discarded")
		}
		if stats.Gain > 3.001 {
			t.Fatal("excessive voice amplification")
		}
		Decode(out, decoded)
		in += energy(pcm)
		processed += energy(decoded)
	}
	if processed < .3*in {
		t.Errorf("quiet speech suppressed: ratio %f", processed/in)
	}
}

func TestLostPacketDoesNotReplayPreviousOverlap(t *testing.T) {
	s := NewStream()
	s.started, s.lastSeq, s.lastAt = true, 10, 1000
	for i := range s.ola {
		s.ola[i] = .5
		s.prevIn[i] = .5
	}
	frame, out := make([]byte, FrameBytes), make([]byte, FrameBytes)
	var powers FramePowers
	s.Process(frame, 12, 1120, nil, out, &powers)
	pcm := make([]float32, FrameSamples)
	Decode(out, pcm)
	if rms(pcm[:hop]) > .01 {
		t.Fatal("audio before a lost packet leaked into the resumed frame")
	}
}
