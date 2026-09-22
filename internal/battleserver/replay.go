package battleserver

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/freaks/openfreaks/internal/protocol"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type ReplayFrame struct {
	Tick int32
	Data []byte
}

const maxReplayFrames = 20000

type ReplayData struct {
	Version                             int
	BattleID, LocationName, Fingerprint string
	LocationID                          int
	ModeID                              int32
	Players                             []*Participant
	Frames                              []ReplayFrame
}
type ReplayGrant struct {
	SessionID int64
	Host      string
	Port      int
	Replay    *ReplayData
}
type ReplaySession struct {
	data          *ReplayData
	sessionID     int64
	mu            sync.Mutex
	peer          *Peer
	actions       chan protocol.PlayerAction
	done          chan struct{}
	once          sync.Once
	cursor, speed int
	paused        bool
}

func replayPath(dir, id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".replay.gz")
}
func saveReplay(dir string, data *ReplayData) error {
	if dir == "" || len(data.Frames) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := replayPath(dir, data.BattleID)
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	z := gzip.NewWriter(file)
	encodeErr := json.NewEncoder(z).Encode(data)
	zipErr := z.Close()
	syncErr := file.Sync()
	closeErr := file.Close()
	for _, err := range []error{encodeErr, zipErr, syncErr, closeErr} {
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	pruneReplays(dir)
	return nil
}
func pruneReplays(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type entry struct {
		name string
		size int64
		date time.Time
	}
	files := []entry{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".gz" {
			continue
		}
		info, err := e.Info()
		if err == nil {
			files = append(files, entry{e.Name(), info.Size(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].date.After(files[j].date) })
	total := int64(0)
	for _, f := range files {
		total += f.size
		if time.Since(f.date) > 7*24*time.Hour || total > 256<<20 {
			_ = os.Remove(filepath.Join(dir, f.name))
		}
	}
}
func loadReplay(dir, id, fingerprint string) (*ReplayData, error) {
	if dir == "" || id == "" || len(id) > 128 {
		return nil, errors.New("replay unavailable")
	}
	f, err := os.Open(replayPath(dir, id))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	var data ReplayData
	if err = json.NewDecoder(io.LimitReader(z, 128<<20)).Decode(&data); err != nil {
		return nil, err
	}
	if data.Version != 1 || data.BattleID != id || data.Fingerprint != fingerprint || len(data.Frames) == 0 || len(data.Frames) > maxReplayFrames {
		return nil, errors.New("replay version, content or frame range mismatch")
	}
	previous := int32(-1)
	for _, f := range data.Frames {
		if f.Tick <= previous || len(f.Data) == 0 || len(f.Data) > maxReliableMessage-16 {
			return nil, errors.New("invalid replay frame")
		}
		previous = f.Tick
	}
	return &data, nil
}
func (s *Server) OpenReplay(playerID int64, battleID string) (*ReplayGrant, error) {
	if playerID <= 0 {
		return nil, errors.New("authentication required")
	}
	data, err := loadReplay(s.cfg.ReplayDir, battleID, s.cat.Fingerprint)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return nil, errors.New("battle closed")
	default:
	}
	if len(s.replays) >= 16 {
		return nil, errors.New("replay capacity")
	}
	session := NextSessionID()
	for s.grants[session] != nil {
		session = NextSessionID()
	}
	replay := &ReplaySession{data: data, sessionID: session, actions: make(chan protocol.PlayerAction, 32), done: make(chan struct{}), speed: 1}
	s.replays[session] = replay
	s.grants[session] = &Grant{SessionID: session, Index: 0, ExpiresAt: time.Now().Add(time.Hour)}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		replay.run()
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.replays, session)
		delete(s.grants, session)
		if p := s.peers[session]; p != nil {
			p.Close()
			s.byAddr[p.Addr.IP.String()]--
			delete(s.peers, session)
		}
	}()
	return &ReplayGrant{SessionID: session, Host: s.cfg.AdvertisedHost, Port: s.cfg.AdvertisedPort, Replay: data}, nil
}
func (r *ReplaySession) attach(p *Peer) {
	r.mu.Lock()
	r.peer = p
	r.mu.Unlock()
	p.mu.Lock()
	p.onApp = func(_ *Peer, payload []byte, _ bool) {
		id, n, ok := protocol.TryReadBattleClientFrame(payload)
		if !ok || id != protocol.UdpPlayerActions || len(payload) != n+6 {
			return
		}
		actions, err := protocol.DecodePlayerActions(payload[6:])
		if err != nil {
			return
		}
		for _, a := range actions {
			select {
			case r.actions <- a:
			default:
			}
		}
	}
	p.mu.Unlock()
}
func (r *ReplaySession) stop() { r.once.Do(func() { close(r.done) }) }
func (r *ReplaySession) run() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(30 * time.Second)
	started := false
	lastSent := -1
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.mu.Lock()
			peer := r.peer
			r.mu.Unlock()
			if peer == nil || !peer.IsAuthenticated() {
				if !started && time.Now().Before(deadline) {
					continue
				}
				return
			}
			started = true
			for i := 0; i < 32; i++ {
				select {
				case a := <-r.actions:
					switch a.Type {
					case protocol.ActionWalk:
						base := r.cursor
						if lastSent >= 0 {
							base = lastSent
						}
						r.cursor = replaySeekIndex(r.data.Frames, base, a)
						lastSent = -1
					case protocol.ActionDash:
						if a.X == 1 || a.X == 2 || a.X == 4 {
							r.speed = a.X
						}
					case protocol.ActionUltOn:
						r.paused = !r.paused
					}
				default:
					i = 32
				}
			}
			if r.cursor != lastSent {
				frame := r.data.Frames[r.cursor]
				peer.SendSnapshot(protocol.EncodeBattleServerFrame(protocol.UdpBattleState, frame.Tick, frame.Data))
				lastSent = r.cursor
			}
			if !r.paused {
				r.cursor = min(len(r.data.Frames)-1, r.cursor+r.speed)
			}
		}
	}
}

func replaySeekIndex(frames []ReplayFrame, base int, action protocol.PlayerAction) int {
	target := frames[base].Tick + int32(action.X)
	if action.Y == 1 {
		target = int32(action.X)
	}
	index := sort.Search(len(frames), func(i int) bool { return frames[i].Tick >= target })
	return min(len(frames)-1, max(0, index))
}
