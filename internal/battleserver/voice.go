package battleserver

import (
	"encoding/binary"
	"sync"

	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/voicedsp"
)

// Голосовой чат боя. В тик матча голос не лезет: кадр речи приходит в
// горутину приёма, чистится (voicedsp: шумодав, подавление эха, громкость —
// сервер знает, что сам отправил игроку, и вычитает это из его микрофона),
// получает спереди номер говорящего и уходит слушателям — союзникам, а в
// режимах «каждый сам за себя» всем.
// По UDP кадр идёт ненадёжным каналом (потерянный кусок речи не ждут и не
// переотправляют), в браузер — своей очередью, которая при заторе роняет
// кадры, а не соединение.

const (
	voiceListen = 1 << 0 // игрок слушает голос
	voiceMic    = 1 << 1 // у игрока включён микрофон
	voiceFlags  = voiceListen | voiceMic

	// Больше двух десятков кадров в секунду живой речи не бывает. Ведро на
	// 12 кадров гасит рывок после подвисания сети, а не поток.
	voiceBurst    = 12
	voicePerSecMs = 30
)

type voiceBucket struct {
	tokens float64
	at     int64
}

type voiceRoom struct {
	mu       sync.Mutex
	flags    map[int]byte
	bucket   map[int]voiceBucket
	dsp      map[int]*voicePlayer
	relaySeq map[int]uint16
	// Голос слышат все: каждый в своей команде (одиночное выживание, дуэль)
	// или футбол — там по просьбе Дениса обе команды слышат друг друга.
	everyone bool
	// proximity: слышимость меняется в бою (Кошмар, Заражение).
	proximity bool
	near      [16][16]bool
}

// voicePlayer — обработка речи игрока и лента того, что ему отправлено
// (по ней из его микрофона вычитается эхо союзников).
type voicePlayer struct {
	mu           sync.Mutex
	stream       *voicedsp.Stream
	ref          voicedsp.Reference
	relayStarted bool
	relayOffset  uint16
}

func (v *voiceRoom) resetPlayer(index int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.flags, index)
	delete(v.bucket, index)
	delete(v.dsp, index)
	// Relay numbering survives reconnects so listeners do not reject the new microphone's sequence zero.
}

func (v *voiceRoom) player(index int) *voicePlayer {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dsp == nil {
		v.dsp = map[int]*voicePlayer{}
	}
	p := v.dsp[index]
	if p == nil {
		p = &voicePlayer{stream: voicedsp.NewStream()}
		v.dsp[index] = p
	}
	return p
}

// voiceEveryoneModes — командные режимы, где голос всё равно общий.
var voiceEveryoneModes = map[string]bool{"ball": true, "ball_3": true}

// voiceProximityModes — голос по близости или по сторонам: кто кого слышит,
// решает мир (sim: NightmareHears, InfectionHears), таблица пересчитывается
// после каждого тика и шлётся тем, у кого она поменялась.
var voiceProximityModes = map[string]bool{"nightmare": true, "infection": true}

func newVoiceRoom(mode string, parts map[int]*Participant) *voiceRoom {
	teams := map[int]bool{}
	everyone := true
	if voiceEveryoneModes[mode] {
		return &voiceRoom{flags: map[int]byte{}, bucket: map[int]voiceBucket{}, everyone: true}
	}
	if voiceProximityModes[mode] {
		return &voiceRoom{flags: map[int]byte{}, bucket: map[int]voiceBucket{}, proximity: true}
	}
	for _, p := range parts {
		if teams[p.Team] {
			everyone = false
		}
		teams[p.Team] = true
	}
	return &voiceRoom{flags: map[int]byte{}, bucket: map[int]voiceBucket{}, everyone: everyone}
}

// hears — слышит ли listener игрока speaker (и видит ли его значок голоса).
func (m *Match) hears(listener, speaker int) bool {
	if listener == speaker {
		return false
	}
	if m.voice.everyone {
		return true
	}
	if m.voice.proximity {
		if listener < 0 || speaker < 0 || listener >= 16 || speaker >= 16 {
			return false
		}
		m.voice.mu.Lock()
		defer m.voice.mu.Unlock()
		return m.voice.near[listener][speaker]
	}
	a, b := m.parts[listener], m.parts[speaker]
	return a != nil && b != nil && a.Team == b.Team
}

func (v *voiceRoom) allow(index int, now int64) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	b, ok := v.bucket[index]
	if !ok {
		b = voiceBucket{tokens: voiceBurst, at: now}
	}
	b.tokens += float64(now-b.at) * voicePerSecMs / 1000
	if b.tokens > voiceBurst {
		b.tokens = voiceBurst
	}
	b.at = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	v.bucket[index] = b
	return allowed
}

// handleVoiceFrame пересылает кадр речи игрока index его слушателям.
func (m *Match) handleVoiceFrame(index int, body []byte, generation int64) {
	if len(body) != 2+voicedsp.FrameBytes || index < 0 || index >= 16 || m.finished.Load() {
		return
	}
	m.peerMu.Lock()
	sender := m.peers[index]
	if sender == nil || m.departed[index] || (generation != 0 && generation != m.generations[index]) {
		m.peerMu.Unlock()
		return
	}
	player := m.voice.player(index)
	m.peerMu.Unlock()
	player.mu.Lock()
	defer player.mu.Unlock()
	if !m.voice.allow(index, nowMs()) {
		return
	}

	var listening uint64
	m.voice.mu.Lock()
	for idx, f := range m.voice.flags {
		if f&voiceListen != 0 && idx >= 0 && idx < 64 {
			listening |= 1 << uint(idx)
		}
	}
	m.voice.mu.Unlock()
	if listening == 0 {
		return
	}

	type target struct {
		index int
		peer  *Peer
	}
	var targets [16]target
	n := 0
	m.peerMu.Lock()
	for idx, peer := range m.peers {
		if idx < 0 || idx >= 64 || listening&(1<<uint(idx)) == 0 || m.departed[idx] || !m.hears(idx, index) {
			continue
		}
		if n < len(targets) {
			targets[n] = target{idx, peer}
			n++
		}
	}
	m.peerMu.Unlock()
	if n == 0 {
		return
	}

	now := nowMs()
	var powers voicedsp.FramePowers
	clean := make([]byte, len(body))
	keep, _ := player.stream.Process(body[2:], binary.LittleEndian.Uint16(body), now,
		&player.ref, clean[2:], &powers)
	if !keep {
		return
	}
	// Recheck after DSP: a reconnect or departure may have happened while processing.
	m.peerMu.Lock()
	defer m.peerMu.Unlock()
	if m.peers[index] != sender || m.departed[index] || m.finished.Load() || (generation != 0 && generation != m.generations[index]) {
		return
	}
	m.voice.mu.Lock()
	if m.voice.relaySeq == nil {
		m.voice.relaySeq = make(map[int]uint16)
	}
	inputSeq := binary.LittleEndian.Uint16(body)
	if !player.relayStarted {
		if last, seen := m.voice.relaySeq[index]; seen {
			player.relayOffset = last + 1 - inputSeq
		}
		player.relayStarted = true
	}
	seq := inputSeq + player.relayOffset
	m.voice.relaySeq[index] = seq
	m.voice.mu.Unlock()
	binary.LittleEndian.PutUint16(clean, seq)
	body = clean

	// Один кадр на всех: заголовок боевого кадра, номер говорящего, звук.
	frame := make([]byte, 10+1+len(body))
	binary.BigEndian.PutUint16(frame[0:2], uint16(protocol.UdpVoiceFrame))
	binary.BigEndian.PutUint32(frame[2:6], uint32(1+len(body)))
	frame[10] = byte(index)
	copy(frame[11:], body)
	for _, t := range targets[:n] {
		if m.peers[t.index] != t.peer || m.departed[t.index] {
			continue
		}
		m.voice.mu.Lock()
		listens := m.voice.flags[t.index]&voiceListen != 0
		m.voice.mu.Unlock()
		if listens && t.peer.SendVoice(frame) {
			m.voice.player(t.index).ref.Add(now, &powers)
		}
	}
}

// handleVoiceState запоминает, слушает ли игрок и включён ли у него микрофон.
// Отвечаем ему таблицей голосов союзников всегда (клиент переспрашивает раз в
// несколько секунд — так таблица переживает переподключения), а союзникам —
// только когда что-то поменялось.
func (m *Match) handleVoiceState(index int, body []byte, generation int64) {
	if len(body) < 1 {
		return
	}
	m.peerMu.Lock()
	if m.peers[index] == nil || m.departed[index] || (generation != 0 && generation != m.generations[index]) {
		m.peerMu.Unlock()
		return
	}
	flags := body[0] & voiceFlags
	m.voice.mu.Lock()
	changed := m.voice.flags[index] != flags
	m.voice.flags[index] = flags
	m.voice.mu.Unlock()
	m.peerMu.Unlock()

	m.sendVoiceTable(index)
	if !changed {
		return
	}
	m.peerMu.Lock()
	others := make([]int, 0, len(m.peers))
	for idx := range m.peers {
		if m.hears(idx, index) {
			others = append(others, idx)
		}
	}
	m.peerMu.Unlock()
	for _, idx := range others {
		m.sendVoiceTable(idx)
	}
}

// sendVoiceTable шлёт игроку listener флаги голоса тех, кого он слышит.
// Отключившийся игрок в таблице выключен, даже если флаги от него остались.
func (m *Match) sendVoiceTable(listener int) {
	type entry struct{ index, flags byte }
	var entries [16]entry
	n := 0

	m.voice.mu.Lock()
	flags := make(map[int]byte, len(m.voice.flags))
	for idx, f := range m.voice.flags {
		flags[idx] = f
	}
	m.voice.mu.Unlock()

	m.peerMu.Lock()
	peer := m.peers[listener]
	for idx, part := range m.parts {
		if n >= len(entries) || idx < 0 || idx > 255 || part.IsBot || !m.hears(listener, idx) {
			continue
		}
		f := flags[idx]
		if other := m.peers[idx]; other == nil || m.departed[idx] {
			f = 0
		}
		entries[n] = entry{byte(idx), f}
		n++
	}
	m.peerMu.Unlock()
	// До приветствия KCP клиент принимает только его: данные раньше рвут связь.
	if peer == nil || !peer.IsAuthenticated() {
		return
	}

	payload := make([]byte, 1+2*n)
	payload[0] = byte(n)
	for i, e := range entries[:n] {
		payload[1+2*i] = e.index
		payload[2+2*i] = e.flags
	}
	peer.SendReliableData(protocol.EncodeBattleServerFrame(protocol.UdpVoiceState, 0, payload))
}
