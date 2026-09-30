package voicedsp

import (
	"math"
	"sync"
)

// Как чистится голос. Кадр 60 мс режется на четыре блока по 15 мс; каждый
// блок — окно 30 мс (корень из Ханна, перекрытие вдвое), спектр по 512 точкам.
// В спектре три множителя:
//
//   - шумодав: шум — минимум сглаженного спектра за последнюю секунду (в речи
//     паузы между слогами есть всегда), усиление — Винер с решением по прошлому
//     блоку (decision-directed), не глубже −14 дБ, чтобы не было «бульканья»;
//   - эхо: сервер знает, что сам отправил этому игроку. Всё, что ушло игроку,
//     ложится в его опорную ленту (Reference) по 15 мс; задержку «ушло → вернулось
//     в микрофон» ищем корреляцией огибающих по полосам, а коэффициент связи
//     динамика с микрофоном — по полосам в моменты, когда говорит только дальний.
//     Эхо в полосе вычитается; блок, где почти всё — эхо, глушится, а кадр из
//     одного эха не пересылается вовсе;
//   - срез ниже ~90 Гц (гул, стук по корпусу телефона).
//
// После — автоматическая громкость (ровная речь около −20 дБ от полной шкалы,
// тихий телефонный микрофон подтягивается до ×3) и мягкий ограничитель.
// Задержка обработки — 15 мс.

const (
	hop            = 240
	window         = 2 * hop
	bins           = fftSize/2 + 1
	blocksPerFrame = FrameSamples / hop
	bandCount      = 20

	slotMs   = 15
	refSlots = 128
	// Эхо возвращается не раньше 30 мс и не позже 1,5 с после отправки.
	minDelay = 2
	maxDelay = 100
	// Разброс сети: огибающую дальнего берём максимумом по ±2 блокам.
	delaySpread = 2

	powerFloor = 1e-4
)

var bandEdgesHz = [bandCount + 1]float64{0, 100, 200, 300, 400, 510, 630, 770, 920, 1080, 1270,
	1480, 1720, 2000, 2320, 2700, 3150, 3700, 4400, 5300, 8001}

var (
	analysis [window]float64
	bandOf   [bins]int
)

func init() {
	for i := range analysis {
		analysis[i] = math.Sqrt(.5 - .5*math.Cos(2*math.Pi*float64(i)/window))
	}
	for k := range bandOf {
		f := float64(k) * SampleRate / fftSize
		b := 0
		for b+1 < bandCount && f >= bandEdgesHz[b+1] {
			b++
		}
		bandOf[k] = b
	}
}

// FramePowers — мощность обработанного кадра по блокам и полосам: так кадр
// ложится в опорную ленту эха у каждого, кто его услышит.
type FramePowers [blocksPerFrame][bandCount]float32

// Reference — что сервер отправил игроку за последние ~2 секунды.
type Reference struct {
	mu    sync.Mutex
	slot  [refSlots]int64
	power [refSlots][bandCount]float32
}

// Add кладёт отправленный игроку кадр в ленту: блок j — в момент nowMs + 15·j.
func (r *Reference) Add(nowMs int64, p *FramePowers) {
	base := nowMs / slotMs
	r.mu.Lock()
	defer r.mu.Unlock()
	for j := range p {
		s := base + int64(j)
		i := s % refSlots
		if r.slot[i] != s {
			r.slot[i] = s
			r.power[i] = [bandCount]float32{}
		}
		for b, v := range p[j] {
			r.power[i][b] += v
		}
	}
}

func (r *Reference) copyRange(from int64, out [][bandCount]float64) (active bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for n := range out {
		s := from + int64(n)
		i := ((s % refSlots) + refSlots) % refSlots
		if s < 0 || r.slot[i] != s {
			out[n] = [bandCount]float64{}
			continue
		}
		for b, v := range r.power[i] {
			out[n][b] = float64(v)
			if v > powerFloor*10 {
				active = true
			}
		}
	}
	return active
}

// Stream — обработка речи одного игрока. Состояние шумодава, громкости и
// эха живёт весь бой: второй фразе уже не нужно заново учиться.
type Stream struct {
	mu sync.Mutex

	started bool
	lastSeq uint16
	lastAt  int64
	prevIn  [hop]float64
	ola     [hop]float64
	enc     EncoderState

	smooth    [bins]float64
	curMin    [bins]float64
	subMin    [4][bins]float64
	minBlocks int
	warm      int
	prevGain  [bins]float64
	prevGamma [bins]float64

	agc float64

	nearMean  [bandCount]float64
	nearWarm  bool
	corr      [maxDelay + 1]float64
	varFar    [maxDelay + 1]float64
	varNear   float64
	delay     int
	estimated int
	updates   int
	lastScore float64
	lastShare float64
	learned   [bandCount]int
	logEcho   [bandCount]float64
	// logFloor — нижняя огибающая отношения микрофон/дальний: эхо не бывает
	// тише самого себя, а своя речь только поднимает отношение.
	logFloor [bandCount]float64

	pcm    [FrameSamples]float32
	out    [FrameSamples]float32
	far    [maxDelay + blocksPerFrame + 2*delaySpread][bandCount]float64
	farLog [maxDelay + blocksPerFrame + 2*delaySpread][bandCount]float64
	re     [fftSize]float64
	im     [fftSize]float64
	power  [bins]float64
	gain   [bins]float64
}

// Stats — что обработка сделала с кадром (для тестов и логов).
type Stats struct {
	EchoBlocks int
	Delay      int
	Confident  bool
	Gain       float64
	Score      float64
	EchoShare  float64
}

func NewStream() *Stream {
	s := &Stream{agc: 1}
	for b := range s.logEcho {
		s.logEcho[b] = math.Log(.05)
		s.logFloor[b] = math.Log(.05)
	}
	for k := range s.curMin {
		s.curMin[k] = math.Inf(1)
		for w := range s.subMin {
			s.subMin[w][k] = math.Inf(1)
		}
	}
	return s
}

// Process чистит кадр frame (ADPCM, FrameBytes) игрока, чья опорная лента
// эха — ref (nil — эхо не ищем). Обработанный кадр пишется в out, мощности
// по полосам — в powers. keep=false — кадр целиком эхо, пересылать нечего.
func (s *Stream) Process(frame []byte, seq uint16, nowMs int64, ref *Reference, out []byte, powers *FramePowers) (keep bool, st Stats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(frame) != FrameBytes || len(out) < FrameBytes || Decode(frame, s.pcm[:]) != FrameSamples {
		return false, st
	}
	if s.started {
		ahead := int16(seq - s.lastSeq)
		if ahead <= 0 {
			// Опоздавший дубль: окна уже ушли вперёд.
			return false, st
		}
		if ahead > 1 || nowMs-s.lastAt > 400 {
			s.prevIn = [hop]float64{}
			s.ola = [hop]float64{}
		}
	}
	s.started, s.lastSeq, s.lastAt = true, seq, nowMs

	nearSlot := nowMs/slotMs - blocksPerFrame
	farFrom := nearSlot - maxDelay - delaySpread
	farActive := false
	if ref != nil {
		farActive = ref.copyRange(farFrom, s.far[:])
	}
	if farActive {
		for n := range s.far {
			for b, v := range s.far[n] {
				s.farLog[n][b] = math.Log(v + powerFloor)
			}
		}
	}

	for j := 0; j < blocksPerFrame; j++ {
		echo := s.block(j, farActive)
		if echo {
			st.EchoBlocks++
		}
		for b := range powers[j] {
			powers[j][b] = 0
		}
		for k := 0; k < bins; k++ {
			powers[j][bandOf[k]] += float32(s.power[k] * s.gain[k] * s.gain[k] * s.agc * s.agc)
		}
	}
	st.Delay = s.delay
	st.Confident = s.estimated > 0
	st.Gain = s.agc
	st.Score = s.lastScore
	st.EchoShare = s.lastShare
	Encode(s.out[:], &s.enc, out)
	// Кадр, где эхо в двух блоках из четырёх, — эхо целиком: остальные
	// блоки — его же хвосты, которые оценка не поймала.
	return st.EchoBlocks < blocksPerFrame/2, st
}

// block обрабатывает блок j кадра: спектр, множители, синтез. true — блок почти целиком эхо.
func (s *Stream) block(j int, farActive bool) bool {
	for i := 0; i < hop; i++ {
		s.re[i] = s.prevIn[i] * analysis[i]
		cur := float64(s.pcm[j*hop+i])
		s.re[hop+i] = cur * analysis[hop+i]
		s.prevIn[i] = cur
	}
	for i := window; i < fftSize; i++ {
		s.re[i] = 0
	}
	s.im = [fftSize]float64{}
	fft(&s.re, &s.im, false)

	var near [bandCount]float64
	var total, noiseTotal float64
	s.warm++
	for k := 0; k < bins; k++ {
		p := s.re[k]*s.re[k] + s.im[k]*s.im[k]
		s.power[k] = p
		near[bandOf[k]] += p
		total += p

		// Шум — минимум сглаженного спектра за ~1 с (4 окна по 16 блоков).
		if s.warm == 1 {
			s.smooth[k] = p
		} else {
			s.smooth[k] = .6*s.smooth[k] + .4*p
		}
		s.curMin[k] = math.Min(s.curMin[k], s.smooth[k])
		noise := s.curMin[k]
		for w := range s.subMin {
			noise = math.Min(noise, s.subMin[w][k])
		}
		if s.warm < 64 {
			// Первая фраза: минимум ещё не видел пауз — давим осторожно.
			noise *= .5
		} else {
			noise *= 1.6
		}
		noise = math.Max(noise, 1e-9)
		noiseTotal += noise

		gamma := p / noise
		xi := .98*s.prevGain[k]*s.prevGain[k]*s.prevGamma[k] + .02*math.Max(gamma-1, 0)
		g := xi / (1 + xi)
		floor := .2
		if s.warm >= 64 && gamma < 1.8 {
			floor = .06
		}
		g = math.Max(g, floor)
		s.prevGain[k], s.prevGamma[k] = g, math.Min(gamma, 1e4)
		switch {
		case k <= 2:
			g *= .05
		case k == 3:
			g *= .4
		}
		s.gain[k] = g
	}
	s.minBlocks++
	if s.minBlocks == 16 {
		s.minBlocks = 0
		copy(s.subMin[1:], s.subMin[:3])
		s.subMin[0] = s.curMin
		for k := range s.curMin {
			s.curMin[k] = math.Inf(1)
		}
	}

	echo, share := false, 0.0
	if farActive {
		echo, share = s.suppressEcho(j, &near, total)
	}

	// Громкость: подстраиваемся только по речи, эхо и шум её не раскачивают.
	var clean float64
	for k := 0; k < bins; k++ {
		clean += s.power[k] * s.gain[k] * s.gain[k]
	}
	// Пока союзник говорит, а связь его голоса с этим микрофоном не
	// выучена (или эха в блоке заметно), громкость не трогаем: иначе она
	// подстроится под тихое эхо и раздует его.
	trusted := !farActive || (s.estimated >= 6 && share < .12)
	if trusted && total > 3*noiseTotal && clean > 0 {
		rms := math.Sqrt(2 * clean / (fftSize * float64(hop)))
		want := math.Max(.5, math.Min(3, .1/math.Max(rms, 1e-6)))
		if want < s.agc {
			s.agc += (want - s.agc) * .25
		} else if !farActive {
			// Подтягиваем громкость только когда союзники молчат: так ей
			// не на чем раскачать остатки эха.
			s.agc += (want - s.agc) * .04
		}
	} else if !farActive && total <= 3*noiseTotal && s.agc > 1 {
		// Do not retain speech boost during sustained background/game sound.
		s.agc += (1 - s.agc) * .02
	}

	for k := 0; k < bins; k++ {
		g := s.gain[k]
		s.re[k] *= g
		s.im[k] *= g
		if k > 0 && k < fftSize/2 {
			s.re[fftSize-k] *= g
			s.im[fftSize-k] *= g
		}
	}
	fft(&s.re, &s.im, true)
	for i := 0; i < hop; i++ {
		y := (s.ola[i] + s.re[i]*analysis[i]) * s.agc
		s.ola[i] = s.re[hop+i] * analysis[hop+i]
		s.out[j*hop+i] = softClip(y)
	}
	return echo
}

// suppressEcho ищет задержку эха, учит связь динамика с микрофоном и
// снижает множители полос на оценку эха. true — блок почти целиком эхо.
func (s *Stream) suppressEcho(j int, near *[bandCount]float64, total float64) (bool, float64) {
	// s.far[0] — слот nearSlot−maxDelay−spread; блок j — слот nearSlot+j.
	at := j + maxDelay + delaySpread
	var dx [bandCount]float64
	for b, p := range near {
		x := math.Log(p + powerFloor)
		if !s.nearWarm {
			s.nearMean[b] = x
		}
		s.nearMean[b] += (x - s.nearMean[b]) * .02
		dx[b] = x - s.nearMean[b]
	}
	s.nearWarm = true

	// Средняя огибающая дальнего по всему окну поиска.
	var mean [bandCount]float64
	for d := minDelay; d <= maxDelay; d++ {
		for b := 0; b < bandCount; b++ {
			mean[b] += s.farLog[at-d][b]
		}
	}
	for b := range mean {
		mean[b] /= maxDelay - minDelay + 1
	}

	// Сначала — среднее по всему увиденному, дальше — память ~1,5 с речи:
	// у речи свой ритм слогов, и на коротком окне похожих задержек много.
	s.updates++
	rate := math.Max(.01, 1/float64(s.updates))
	var nearVar float64
	for _, v := range dx {
		nearVar += v * v
	}
	s.varNear += (nearVar - s.varNear) * rate
	best, bestScore := s.delay, math.Inf(-1)
	for d := minDelay; d <= maxDelay; d++ {
		var c, v float64
		for b := 0; b < bandCount; b++ {
			y := s.farLog[at-d][b] - mean[b]
			c += dx[b] * y
			v += y * y
		}
		s.corr[d] += (c - s.corr[d]) * rate
		s.varFar[d] += (v - s.varFar[d]) * rate
		score := s.corr[d] / math.Sqrt(s.varNear*s.varFar[d]+1e-9)
		if score > bestScore {
			best, bestScore = d, score
		}
	}
	if s.delay >= minDelay {
		current := s.corr[s.delay] / math.Sqrt(s.varNear*s.varFar[s.delay]+1e-9)
		if bestScore < current+.08 {
			best, bestScore = s.delay, current
		}
	}
	s.lastScore = bestScore
	// Две независимые речи тоже слегка похожи ритмом слогов (≈0,3), так
	// что задержку фиксируем только по заметному сходству.
	if bestScore > lockScore {
		s.delay = best
		s.estimated++
	} else if bestScore < .2 {
		s.estimated = 0
	}
	if s.estimated < 6 || s.updates < 30 {
		return false, 1
	}

	// Огибающая дальнего около найденной задержки: сеть дрожит на ±30 мс.
	var farBand [bandCount]float64
	for d := s.delay - delaySpread; d <= s.delay+delaySpread; d++ {
		if d < 0 || at-d >= len(s.far) {
			continue
		}
		for b := range farBand {
			farBand[b] = math.Max(farBand[b], s.far[at-d][b])
		}
	}

	var echoTotal, predicted float64
	var bandGain [bandCount]float64
	for b := range farBand {
		bandGain[b] = 1
		f := farBand[b]
		center := s.far[at-s.delay][b]
		if f < powerFloor*10 {
			continue
		}
		// Связь динамик → микрофон — среднее отношения в логарифмах, но только
		// по блокам у нижней огибающей: там микрофон слышит одно эхо. Когда
		// игрок говорит поверх союзника, отношение уходит вверх, и такие блоки
		// связь не раздувают: иначе двойная речь сама себя объявляла эхом.
		// Учим по слоту ровно на задержке: максимум по соседним слотам
		// завышал бы дальнего, и связь выходила бы втрое меньше настоящей.
		r := math.Log(near[b]/math.Max(center, powerFloor*10) + 1e-9)
		s.learned[b]++
		// Нижний квантиль (20 %) отношения: у эха он рядом со средним, а при
		// своей речи поверх союзника его тянут вниз паузы между словами.
		if s.learned[b] == 1 {
			s.logFloor[b] = r
		} else if r < s.logFloor[b] {
			s.logFloor[b] -= floorStep * (1 - floorQuantile)
		} else {
			s.logFloor[b] += floorStep * floorQuantile
		}
		switch {
		case s.learned[b] == 1:
			s.logEcho[b] = r
		case r < s.logFloor[b]+floorSpan:
			s.logEcho[b] += (r - s.logEcho[b]) * .08
		}
		s.logEcho[b] = math.Min(s.logFloor[b]+floorSpan, s.logEcho[b])
		s.logEcho[b] = math.Max(math.Log(1e-4), math.Min(math.Log(30), s.logEcho[b]))
		e := math.Exp(s.logEcho[b]) * f
		predicted += e
		echoTotal += math.Min(e, near[b])
		if near[b] > 0 {
			bandGain[b] = math.Max(.04, 1-2.5*e/near[b])
		}
	}
	// Своя речь поверх союзника проходит, только если она заметно громче
	// ожидаемого эха; всё остальное в такой момент — эхо, и блок глушится.
	// Глушим блок целиком, только когда сходство с дальним несомненно: при
	// слабом сходстве это может быть союзник, перебитый своей речью, а не
	// эхо, — там хватает вычитания по полосам.
	echo := bestScore >= gateScore && (echoTotal > .4*total || total < 3*predicted)
	s.lastShare = predicted / (total + 1e-12)
	for k := 0; k < bins; k++ {
		g := bandGain[bandOf[k]]
		if echo {
			g = .04
		}
		s.gain[k] *= g
	}
	return echo, s.lastShare
}

const (
	lockScore     = .45
	gateScore     = .5
	floorQuantile = .2
	floorStep     = .12
)

var floorSpan = math.Log(6)

func softClip(x float64) float32 {
	const knee = .85
	switch {
	case x > knee:
		x = knee + (x-knee)/(1+(x-knee)/(1-knee))
	case x < -knee:
		x = -knee + (x+knee)/(1-(x+knee)/(1-knee))
	}
	return float32(x)
}
