package mainserver

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"strings"
	"time"
)

const battleEmoteCooldown = 3 * time.Second

func (s *Server) onBattleEmote(c *Conn, payload []byte) bool {
	emoji, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	if emoji < 0 || emoji >= 6 || c.BattleID == "" || strings.HasPrefix(c.BattleID, "replay:") {
		return true
	}
	allocation := s.store.Meta.Allocations[c.BattleID]
	if allocation == nil {
		return true
	}
	index := -1
	for _, p := range allocation.Players {
		if p.PlayerID == c.PlayerID && !p.IsBot && !p.Abandoned {
			index = p.Index
			break
		}
	}
	if index < 0 {
		return true
	}
	now := time.Now()
	if now.Sub(s.emoteLast[c.PlayerID]) < battleEmoteCooldown {
		return true
	}
	if s.emoteLast == nil {
		s.emoteLast = map[int64]time.Time{}
	}
	for id, last := range s.emoteLast {
		if now.Sub(last) > time.Minute {
			delete(s.emoteLast, id)
		}
	}
	s.emoteLast[c.PlayerID] = now
	w := protocol.NewWriter()
	w.WriteInt(int32(index))
	w.WriteInt(emoji)
	for _, p := range allocation.Players {
		if p.IsBot || p.Abandoned {
			continue
		}
		if peer, ok := s.ConnByPlayer(p.PlayerID); ok && peer.BattleID == c.BattleID {
			peer.Send(64, w.Bytes())
		}
	}
	return true
}
