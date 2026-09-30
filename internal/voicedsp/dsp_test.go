package voicedsp

import (
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"testing"
)

func readWav(t *testing.T, path string) []float32 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("нет записи речи:", err)
	}
	at := 12
	for at+8 <= len(b) {
		id := string(b[at : at+4])
		size := int(binary.LittleEndian.Uint32(b[at+4:]))
		if id == "data" {
			data := b[at+8 : min(len(b), at+8+size)]
			out := make([]float32, len(data)/2)
			for i := range out {
				out[i] = float32(int16(binary.LittleEndian.Uint16(data[2*i:]))) / 32768
			}
			return out
		}
		at += 8 + size
	}
	t.Fatal("в wav нет данных")
	return nil
}

func TestCodecRoundTrip(t *testing.T) {
	pcm := make([]float32, FrameSamples)
	for i := range pcm {
		pcm[i] = float32(.3 * math.Sin(float64(i)*.07))
	}
	var st EncoderState
	frame := make([]byte, FrameBytes)
	Encode(pcm, &st, frame)
	back := make([]float32, FrameSamples)
	if Decode(frame, back) != FrameSamples {
		t.Fatal("decode")
	}
	var e, n float64
	for i := range pcm {
		e += float64(pcm[i] * pcm[i])
		d := float64(pcm[i] - back[i])
		n += d * d
	}
	if snr := 10 * math.Log10(e/n); snr < 20 {
		t.Fatalf("snr %.1f", snr)
	}
}

func TestStreamPassesCleanSpeech(t *testing.T) {
	speech := readWav(t, "testdata/near.wav")
	s := NewStream()
	var in, out float64
	frame := make([]byte, FrameBytes)
	res := make([]byte, FrameBytes)
	back := make([]float32, FrameSamples)
	var enc EncoderState
	var powers FramePowers
	kept := 0
	total := 0
	for k := 0; (k+1)*FrameSamples <= len(speech); k++ {
		chunk := speech[k*FrameSamples : (k+1)*FrameSamples]
		Encode(chunk, &enc, frame)
		keep, _ := s.Process(frame, uint16(k), int64(k*60), nil, res, &powers)
		total++
		if !keep {
			continue
		}
		kept++
		Decode(res, back)
		for i := range chunk {
			in += float64(chunk[i] * chunk[i])
			out += float64(back[i] * back[i])
		}
	}
	t.Logf("кадров %d/%d, вход %.1f дБ, выход %.1f дБ", kept, total, 10*math.Log10(in/float64(total*FrameSamples)), 10*math.Log10(out/float64(total*FrameSamples)))
	if kept != total {
		t.Fatalf("без дальнего кадры не должны пропадать: %d/%d", kept, total)
	}
	if out < in*.3 {
		t.Fatalf("речь задавлена: %.3f → %.3f", in, out)
	}
}

// echoRig — два игрока: A говорит, B слышит A из динамика телефона, и его
// микрофон возвращает эхо (плюс свою речь и шум).
type echoRig struct {
	t          *testing.T
	a, b       *Stream
	refA, refB Reference
	rnd        *rand.Rand
	gain       float64
}

type echoResult struct {
	echoIn, echoOut     float64 // энергия, пока говорит только A
	nearIn, nearOut     float64 // энергия, пока говорит только B
	doubleKept, doubleN int     // кадры B во время двойного разговора
	nearKept, nearN     int
}

func (r *echoRig) run(far, near []float32, nearStart int, delayMs, jitterMs float64) echoResult {
	const frameMs = 60
	total := max(len(far), nearStart+len(near)) + 2*SampleRate
	micB := make([]float32, total)
	nearOnly := make([]bool, total/FrameSamples+1)
	farOnly := make([]bool, total/FrameSamples+1)

	// 1. A: гейт клиента → сервер → B. Кадр k уходит от A в 60(k+1) мс.
	var encA EncoderState
	frame := make([]byte, FrameBytes)
	res := make([]byte, FrameBytes)
	pcm := make([]float32, FrameSamples)
	var powers FramePowers
	played := make([]float32, total)
	utteranceStart := -1.0
	type refEvent struct {
		at     int64
		powers FramePowers
	}
	var events []refEvent
	firstFrame := 0
	hang := 0
	for k := 0; (k+1)*FrameSamples <= len(far); k++ {
		chunk := far[k*FrameSamples : (k+1)*FrameSamples]
		if rms(chunk) > .012 {
			hang = 6
		} else if hang > 0 {
			hang--
		}
		if hang == 0 {
			utteranceStart = -1
			continue
		}
		Encode(chunk, &encA, frame)
		arrive := float64((k+1)*frameMs) + 30
		keep, _ := r.a.Process(frame, uint16(k), int64(arrive), &r.refA, res, &powers)
		if !keep {
			continue
		}
		events = append(events, refEvent{int64(arrive), powers})
		Decode(res, pcm)
		// Клиент B: джиттер-буфер 120 мс от первого кадра фразы.
		atB := arrive + 40 + r.rnd.Float64()*jitterMs
		if utteranceStart < 0 {
			utteranceStart = atB + 120
			firstFrame = k
		}
		playAt := utteranceStart + float64((k-firstFrame)*frameMs) + delayMs
		start := int(playAt * SampleRate / 1000)
		for i, v := range pcm {
			if start+i < total {
				played[start+i] += v
			}
		}
	}

	// 2. Акустика телефона B: маленький динамик режет низ, немного отражений.
	var hp, lp, prev float64
	for i := range played {
		x := float64(played[i])
		hp = .95 * (hp + x - prev)
		prev = x
		lp += .6 * (hp - lp)
		e := lp * r.gain
		if i >= 400 {
			e += .3 * r.gain * float64(played[i-400])
		}
		micB[i] += float32(e)
	}
	for i := range near {
		micB[nearStart+i] += near[i]
	}
	for i := range micB {
		micB[i] += float32(r.rnd.NormFloat64() * .002)
	}
	for k := range farOnly {
		lo, hi := k*FrameSamples, (k+1)*FrameSamples
		farOnly[k] = lo < len(far) && rms(played[lo:min(hi, total)]) > .01 && (hi <= nearStart || lo >= nearStart+len(near))
		nearOnly[k] = lo >= nearStart && hi <= nearStart+len(near) && rms(played[lo:min(hi, total)]) < .002
	}

	// 3. B: гейт клиента → сервер с опорной лентой B.
	var out echoResult
	var encB EncoderState
	hang = 0
	for k := 0; (k+1)*FrameSamples <= total; k++ {
		chunk := micB[k*FrameSamples : (k+1)*FrameSamples]
		if rms(chunk) > .012 {
			hang = 6
		} else if hang > 0 {
			hang--
		}
		if hang == 0 {
			continue
		}
		Encode(chunk, &encB, frame)
		arrive := float64((k+1)*frameMs) + 40 + r.rnd.Float64()*jitterMs
		// Лента эха заполняется в том же порядке, что на сервере.
		for len(events) > 0 && events[0].at <= int64(arrive) {
			r.refB.Add(events[0].at, &events[0].powers)
			events = events[1:]
		}
		keep, st := r.b.Process(frame, uint16(k), int64(arrive), &r.refB, res, &powers)
		if v := os.Getenv("VDSP_DEBUG"); v == "all" || v != "" && k < 160 {
			Decode(res, pcm)
			r.t.Logf("k=%d delay=%d conf=%v score=%.2f share=%.2f echo=%d gain=%.2f in=%.4f out=%.4f keep=%v", k, st.Delay, st.Confident, st.Score, st.EchoShare, st.EchoBlocks, st.Gain, rms(chunk), rms(pcm), keep)
		}
		e := energy(chunk)
		var o float64
		if keep {
			Decode(res, pcm)
			o = energy(pcm)
		}
		lo := k * FrameSamples
		nearNow := lo+FrameSamples > nearStart && lo < nearStart+len(near) && rms(near[max(0, lo-nearStart):max(0, min(len(near), lo+FrameSamples-nearStart))]) > .01
		switch {
		case k < len(farOnly) && farOnly[k] && k > 50:
			out.echoIn += e
			out.echoOut += o
		case k < len(nearOnly) && nearOnly[k] && nearNow:
			out.nearIn += e
			out.nearOut += o
			out.nearN++
			if keep {
				out.nearKept++
			}
		case nearNow:
			out.doubleN++
			if keep {
				out.doubleKept++
			}
		}
	}
	return out
}

func rms(x []float32) float64 {
	if len(x) == 0 {
		return 0
	}
	return math.Sqrt(energy(x) / float64(len(x)))
}

func energy(x []float32) float64 {
	var e float64
	for _, v := range x {
		e += float64(v) * float64(v)
	}
	return e
}

func TestEchoIsSuppressed(t *testing.T) {
	far := readWav(t, "testdata/far.wav")
	near := readWav(t, "testdata/near.wav")
	for _, c := range []struct {
		name            string
		gain, delay, jt float64
		minERLE         float64
	}{
		{"громкий динамик", .6, 30, 20, 12},
		// Тихое эхо похоже на дальнего слабее (сходство ~0,3–0,5, как у двух
		// независимых речей), поэтому целиком не глушится — только вычитается.
		{"тихий динамик", .15, 10, 10, 8},
		{"плохая сеть", .5, 80, 60, 12},
		// Эха нет совсем: союзники перебивают друг друга, и сервер не должен
		// принимать чужую речь поверх своей за эхо.
		{"наушники", 0, 30, 20, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			rig := &echoRig{t: t, a: NewStream(), b: NewStream(), rnd: rand.New(rand.NewSource(1)), gain: c.gain}
			// Речь B: сначала поверх A (двойной разговор), потом одна.
			nearStart := len(far) - len(near)/2
			r := rig.run(far, near, nearStart, c.delay, c.jt)
			erle := 10 * math.Log10(r.echoIn/(r.echoOut+1e-9))
			t.Logf("эхо подавлено на %.1f дБ; своя речь: кадров %d/%d, двойной разговор %d/%d, уровень %.1f дБ",
				erle, r.nearKept, r.nearN, r.doubleKept, r.doubleN, 10*math.Log10((r.nearOut+1e-12)/(r.nearIn+1e-12)))
			if c.gain > 0 && erle < c.minERLE {
				t.Errorf("эхо подавлено слабо: %.1f дБ", erle)
			}
			if c.gain == 0 && float64(r.doubleKept) < .9*float64(r.doubleN) {
				t.Errorf("без эха двойной разговор режется: %d/%d", r.doubleKept, r.doubleN)
			}
			if r.nearN > 0 && float64(r.nearKept) < .9*float64(r.nearN) {
				t.Errorf("своя речь теряется: %d/%d", r.nearKept, r.nearN)
			}
			if r.nearIn > 0 && r.nearOut < .3*r.nearIn {
				t.Errorf("своя речь задавлена")
			}
		})
	}
}

func BenchmarkProcess(b *testing.B) {
	pcm := make([]float32, FrameSamples)
	for i := range pcm {
		pcm[i] = float32(.2 * math.Sin(float64(i)*.05))
	}
	frame := make([]byte, FrameBytes)
	var enc EncoderState
	Encode(pcm, &enc, frame)
	s := NewStream()
	var ref Reference
	var powers FramePowers
	out := make([]byte, FrameBytes)
	for i := 0; i < 200; i++ {
		ref.Add(int64(i*60), &powers)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Process(frame, uint16(i), int64(i*60), &ref, out, &powers)
	}
}
