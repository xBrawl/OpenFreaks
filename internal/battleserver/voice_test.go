package battleserver

import (
	"context"
	"encoding/binary"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/sim"
)

// voiceMatch — матч без мира: для голоса нужны только составы и соединения.
func voiceMatch(teams ...int) *Match { return voiceMatchIn("", teams...) }

func voiceMatchIn(mode string, teams ...int) *Match {
	parts := make([]*Participant, len(teams))
	sessions := map[int]int64{}
	for i, team := range teams {
		parts[i] = &Participant{Index: i, PlayerID: int64(100 + i), Team: team}
		sessions[i] = int64(1000 + i)
	}
	return NewMatch("voice", &sim.World{ModeName: mode}, parts, sessions, nil)
}

func webVoicePeer(t *testing.T) *Peer {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Peer{authenticated: true, installSeq: nextPeerInstallSeq(), web: &webPeer{ctx: ctx, cancel: cancel,
		control: make(chan []byte, 64), snapshot: make(chan []byte, 1), voice: make(chan []byte, 8)}}
}

func voiceSend(m *Match, index int, id int16, body []byte) {
	m.handleAppGeneration(index, protocol.EncodeBattleClientFrame(id, body), m.generationOf(index))
}

func voiceBody(seq uint16) []byte {
	body := make([]byte, 2+3+480)
	binary.LittleEndian.PutUint16(body, seq)
	return body
}

func drainVoice(p *Peer) (frames [][]byte) {
	for {
		select {
		case f := <-p.web.voice:
			frames = append(frames, f)
		default:
			return frames
		}
	}
}

func lastTable(t *testing.T, p *Peer) map[int]byte {
	t.Helper()
	var table map[int]byte
	for {
		select {
		case f := <-p.web.control:
			if len(f) < 11 || int16(binary.BigEndian.Uint16(f[:2])) != protocol.UdpVoiceState ||
				int(binary.BigEndian.Uint32(f[2:6])) != len(f)-10 {
				continue
			}
			body := f[10:]
			table = map[int]byte{}
			for i := 0; i < int(body[0]); i++ {
				table[int(body[1+2*i])] = body[2+2*i]
			}
		default:
			return table
		}
	}
}

func TestVoiceReachesListeningTeammatesOnly(t *testing.T) {
	m := voiceMatch(0, 0, 0, 1)
	peers := make([]*Peer, 4)
	for i := range peers {
		peers[i] = webVoicePeer(t)
		if !m.AttachPeer(i, peers[i]) {
			t.Fatal("attach", i)
		}
	}
	voiceSend(m, 0, protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
	voiceSend(m, 1, protocol.UdpVoiceState, []byte{voiceListen})
	voiceSend(m, 3, protocol.UdpVoiceState, []byte{voiceListen}) // соперник слушает — не должен слышать
	// Игрок 2 голос не включал.

	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(7))

	got := drainVoice(peers[1])
	if len(got) != 1 {
		t.Fatalf("союзник получил %d кадров", len(got))
	}
	f := got[0]
	if id := int16(binary.BigEndian.Uint16(f)); id != protocol.UdpVoiceFrame || f[10] != 0 || binary.LittleEndian.Uint16(f[11:]) != 7 || len(f) != 10+1+485 {
		t.Fatalf("кадр искажён: id=%d говорящий=%d длина=%d", id, f[10], len(f))
	}
	if n := len(drainVoice(peers[0])); n != 0 {
		t.Fatal("говорящий слышит себя")
	}
	if n := len(drainVoice(peers[2])); n != 0 {
		t.Fatal("кадр ушёл тому, кто голос не включал")
	}
	if n := len(drainVoice(peers[3])); n != 0 {
		t.Fatal("соперник слышит чужую команду")
	}

	table := lastTable(t, peers[1])
	if table[0] != voiceListen|voiceMic || table[2] != 0 {
		t.Fatalf("таблица союзника: %v", table)
	}
	if _, ok := table[3]; ok {
		t.Fatal("в таблицу попал соперник")
	}
}

func TestVoiceEveryoneHearsWhenEachPlayerIsATeam(t *testing.T) {
	m := voiceMatch(0, 1, 2)
	peers := make([]*Peer, 3)
	for i := range peers {
		peers[i] = webVoicePeer(t)
		m.AttachPeer(i, peers[i])
		voiceSend(m, i, protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
	}
	voiceSend(m, 2, protocol.UdpVoiceFrame, voiceBody(1))
	if len(drainVoice(peers[0])) != 1 || len(drainVoice(peers[1])) != 1 {
		t.Fatal("в режиме без команд голос слышат не все")
	}
}

func TestVoiceFloodIsCappedAndWebQueueDropsInsteadOfClosing(t *testing.T) {
	m := voiceMatch(0, 0)
	speaker, listener := webVoicePeer(t), webVoicePeer(t)
	m.AttachPeer(0, speaker)
	m.AttachPeer(1, listener)
	voiceSend(m, 1, protocol.UdpVoiceState, []byte{voiceListen})
	for i := 0; i < 200; i++ {
		voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(uint16(i)))
	}
	if n := len(drainVoice(listener)); n == 0 || n > 8 {
		t.Fatalf("в очереди браузера %d кадров", n)
	}
	if listener.closed {
		t.Fatal("затор голоса закрыл соединение")
	}
	if !m.voice.allow(0, nowMs()+1000) {
		t.Fatal("ведро не наполняется со временем")
	}
	m.voice.bucket[0] = voiceBucket{at: nowMs()}
	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(1))
	if len(drainVoice(listener)) != 0 {
		t.Fatal("пустое ведро пропустило кадр")
	}
}

func TestVoiceFromReplacedConnectionIsDropped(t *testing.T) {
	m := voiceMatch(0, 0)
	old, listener := webVoicePeer(t), webVoicePeer(t)
	m.AttachPeer(0, old)
	m.AttachPeer(1, listener)
	voiceSend(m, 1, protocol.UdpVoiceState, []byte{voiceListen})
	stale := m.generationOf(0)
	m.AttachPeer(0, webVoicePeer(t))
	m.handleAppGeneration(0, protocol.EncodeBattleClientFrame(protocol.UdpVoiceFrame, voiceBody(1)), stale)
	if len(drainVoice(listener)) != 0 {
		t.Fatal("кадр старого соединения переслан")
	}
}

func TestVoiceOverUDPUsesUnreliableChannel(t *testing.T) {
	m := voiceMatch(0, 0)
	speaker := webVoicePeer(t)
	listener := auditPeer()
	var sent [][]byte
	listener.output = func(b []byte) { sent = append(sent, b) }
	m.AttachPeer(0, speaker)
	m.AttachPeer(1, listener)
	m.voice.flags[1] = voiceListen
	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(3))
	if len(sent) != 1 || sent[0][0] != chanUnreliable || sent[0][9] != hdrData || sent[0][10+10] != 0 {
		t.Fatalf("UDP-кадр голоса: %d пакетов", len(sent))
	}
}

func TestVoiceInFootballReachesBothTeams(t *testing.T) {
	m := voiceMatchIn("ball", 0, 0, 1, 1)
	peers := make([]*Peer, 4)
	for i := range peers {
		peers[i] = webVoicePeer(t)
		m.AttachPeer(i, peers[i])
		voiceSend(m, i, protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
	}
	voiceSend(m, 0, protocol.UdpVoiceFrame, voiceBody(1))
	for i := 1; i < 4; i++ {
		if len(drainVoice(peers[i])) != 1 {
			t.Fatalf("в футболе игрок %d не слышит говорящего", i)
		}
	}
	if table := lastTable(t, peers[3]); table[0] != voiceListen|voiceMic {
		t.Fatalf("соперник в футболе не видит голос: %v", table)
	}
}

// Сквозной путь браузера: настоящий обработчик WebSocket, два игрока футбола
// в разных командах — кадр одного доходит до другого с номером говорящего.
func TestVoiceTravelsThroughWebSocketHandler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := NewServer(Config{}, nil, nil)
	defer s.Close()
	m := voiceMatchIn("ball", 0, 1)
	s.matches[m.ID] = m
	for index, sid := range m.SessionIDs {
		s.grants[sid] = &Grant{SessionID: sid, Index: index, ExpiresAt: time.Now().Add(time.Minute)}
	}
	server := httptest.NewServer(s.WebSocketHandler(ctx, nil))
	defer server.Close()
	dial := func(sid int64) *websocket.Conn {
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{Subprotocols: []string{"openfreaks.battle.v1"}})
		if err != nil {
			t.Fatal(err)
		}
		auth := make([]byte, 8)
		binary.BigEndian.PutUint64(auth, uint64(sid))
		if err = ws.Write(ctx, websocket.MessageBinary, auth); err != nil {
			t.Fatal(err)
		}
		if _, ack, err := ws.Read(ctx); err != nil || len(ack) != 1 {
			t.Fatal("auth", err)
		}
		return ws
	}
	a, b := dial(m.SessionIDs[0]), dial(m.SessionIDs[1])
	defer a.CloseNow()
	defer b.CloseNow()
	state := protocol.EncodeBattleClientFrame(protocol.UdpVoiceState, []byte{voiceListen | voiceMic})
	for _, ws := range []*websocket.Conn{a, b} {
		if err := ws.Write(ctx, websocket.MessageBinary, state); err != nil {
			t.Fatal(err)
		}
	}
	// Состояние доходит асинхронно: ждём, пока сервер запомнит, что B слушает.
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		m.voice.mu.Lock()
		ready := m.voice.flags[1]&voiceListen != 0
		m.voice.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("состояние голоса не дошло")
		}
	}
	if err := a.Write(ctx, websocket.MessageBinary, protocol.EncodeBattleClientFrame(protocol.UdpVoiceFrame, voiceBody(9))); err != nil {
		t.Fatal(err)
	}
	for {
		_, data, err := b.Read(ctx)
		if err != nil {
			t.Fatal("кадр голоса не пришёл:", err)
		}
		if int16(binary.BigEndian.Uint16(data)) == protocol.UdpVoiceFrame {
			if data[10] != 0 || binary.LittleEndian.Uint16(data[11:]) != 9 {
				t.Fatalf("кадр искажён: %x", data[:16])
			}
			return
		}
	}
}
