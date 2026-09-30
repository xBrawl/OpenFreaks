// Package voicedsp — обработка голоса боя на сервере: шумодав, подавление
// эха и выравнивание громкости. Кадры приходят от клиентов в IMA ADPCM
// (Freaks.Voice.VoiceCodec); сервер раскодирует кадр, чистит его и кодирует
// обратно тем же кодеком — клиентам формат не меняется.
package voicedsp

const (
	SampleRate   = 16000
	FrameSamples = 960
	HeaderBytes  = 3
	FrameBytes   = HeaderBytes + FrameSamples/2
)

var indexTable = [16]int{-1, -1, -1, -1, 2, 4, 6, 8, -1, -1, -1, -1, 2, 4, 6, 8}

var stepTable = [89]int{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31, 34, 37, 41, 45, 50, 55, 60, 66, 73, 80,
	88, 97, 107, 118, 130, 143, 157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408, 449, 494, 544, 598,
	658, 724, 796, 876, 963, 1060, 1166, 1282, 1411, 1552, 1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327,
	3660, 4026, 4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442, 11487, 12635, 13899, 15289,
	16818, 18500, 20350, 22385, 24623, 27086, 29794, 32767,
}

// EncoderState — состояние кодера между кадрами; каждый кадр несёт его в заголовке.
type EncoderState struct {
	Predictor int
	Index     int
}

// Decode раскодирует кадр в pcm (−1..1); возвращает число отсчётов, 0 — битый кадр.
func Decode(in []byte, pcm []float32) int {
	if len(in) < HeaderBytes+1 {
		return 0
	}
	value := int(int16(uint16(in[0]) | uint16(in[1])<<8))
	index := int(in[2])
	if index > 88 {
		return 0
	}
	n := min((len(in)-HeaderBytes)*2, len(pcm))
	for i := 0; i < n; i++ {
		packed := in[HeaderBytes+i>>1]
		nibble := int(packed & 0x0F)
		if i&1 == 1 {
			nibble = int(packed >> 4)
		}
		value = decodeSample(nibble, value, &index)
		pcm[i] = float32(value) / 32768
	}
	return n
}

// Encode кодирует FrameSamples отсчётов в out (длиной не меньше FrameBytes).
func Encode(pcm []float32, st *EncoderState, out []byte) int {
	out[0] = byte(st.Predictor)
	out[1] = byte(st.Predictor >> 8)
	out[2] = byte(st.Index)
	predictor, index := st.Predictor, st.Index
	at := HeaderBytes
	for i := 0; i+1 < FrameSamples; i += 2 {
		low := encodeSample(toShort(pcm[i]), &predictor, &index)
		high := encodeSample(toShort(pcm[i+1]), &predictor, &index)
		out[at] = byte(low | high<<4)
		at++
	}
	st.Predictor, st.Index = predictor, index
	return FrameBytes
}

func toShort(x float32) int {
	v := int(x * 32767)
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return v
}

func encodeSample(sample int, predictor, index *int) int {
	step := stepTable[*index]
	diff := sample - *predictor
	nibble := 0
	if diff < 0 {
		nibble = 8
		diff = -diff
	}
	delta := step >> 3
	if diff >= step {
		nibble |= 4
		diff -= step
		delta += step
	}
	step >>= 1
	if diff >= step {
		nibble |= 2
		diff -= step
		delta += step
	}
	step >>= 1
	if diff >= step {
		nibble |= 1
		delta += step
	}
	if nibble&8 != 0 {
		*predictor -= delta
	} else {
		*predictor += delta
	}
	*predictor = max(-32768, min(32767, *predictor))
	*index = max(0, min(88, *index+indexTable[nibble]))
	return nibble
}

func decodeSample(nibble, predictor int, index *int) int {
	step := stepTable[*index]
	delta := step >> 3
	if nibble&4 != 0 {
		delta += step
	}
	if nibble&2 != 0 {
		delta += step >> 1
	}
	if nibble&1 != 0 {
		delta += step >> 2
	}
	if nibble&8 != 0 {
		predictor -= delta
	} else {
		predictor += delta
	}
	predictor = max(-32768, min(32767, predictor))
	*index = max(0, min(88, *index+indexTable[nibble]))
	return predictor
}
