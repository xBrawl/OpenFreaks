package mainserver

import (
	"crypto/rand"
	"fmt"
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/protocol"
	"net/url"
	"strconv"
	"strings"
)

type replayAllocator interface {
	OpenReplay(int64, string) (*battleserver.ReplayGrant, error)
}

func newBattleID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("OS random source unavailable")
	}
	id[6] = id[6]&15 | 64
	id[8] = id[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}
func deepLinkResult(c *Conn, kind int32, target string) bool {
	w := protocol.NewWriter()
	w.WriteInt(kind)
	w.WriteString(target)
	return c.Send(protocol.SDeepLinkResponse, w.Bytes())
}
func (s *Server) onStartReplay(c *Conn, payload []byte) bool {
	id, _, err := protocol.DecodeStringOnly(payload)
	if err != nil {
		return false
	}
	if len(id) > 128 || c.BattleID != "" {
		return deepLinkResult(c, 0, "")
	}
	allocator, ok := s.battle.(replayAllocator)
	if !ok {
		return deepLinkResult(c, 0, "")
	}
	grant, err := allocator.OpenReplay(c.PlayerID, id)
	if err != nil {
		return deepLinkResult(c, 0, "")
	}
	data := grant.Replay
	message := &protocol.BattleStartedMsg{ParticipantType: protocol.ParticipantReplayWatcher, ReplayTickLength: data.Frames[len(data.Frames)-1].Tick, LocationName: data.LocationName, OwnIndex: 0, OwnTeam: 0, Host: grant.Host, Port: int32(grant.Port), SessionID: grant.SessionID}
	for _, p := range data.Players {
		message.Players = append(message.Players, protocol.BattleStartedPlayer{Name: p.Name, ModelID: p.ModelID, Index: int32(p.Index), Team: int32(p.Team), HPLevel: p.HPLevel, DamageLevel: p.DamageLevel, SpeedLevel: p.SpeedLevel, UltimateUpgradeID: p.UltID, DashID: p.DashID, AugmentIDs: p.AugmentIDs})
	}
	if message.Host == "" {
		message.Host = s.advHost
	}
	if message.Port <= 0 {
		message.Port = int32(s.advPort)
	}
	c.BattleID = "replay:" + id
	return c.Send(protocol.SBattleStarted, message.Encode())
}
func (s *Server) onDeepLink(c *Conn, payload []byte) bool {
	text, _, err := protocol.DecodeStringOnly(payload)
	if err != nil {
		return false
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme != "https" || (parsed.Hostname() != "playfreaks.ru" && parsed.Hostname() != "denis.kornilov12.fvds.ru") {
		return deepLinkResult(c, 0, "")
	}
	path := strings.Trim(parsed.Path, "/")
	path = strings.TrimPrefix(path, "freaks/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return deepLinkResult(c, 0, "")
	}
	switch parts[0] {
	case "player", "profile":
		id, e := strconv.ParseInt(parts[1], 10, 64)
		if _, ok := s.store.GetAccount(id); e == nil && ok {
			return deepLinkResult(c, 1, parts[1])
		}
	case "team":
		id, e := strconv.ParseInt(parts[1], 10, 64)
		if _, ok := s.store.GetTeam(id); e == nil && ok {
			return deepLinkResult(c, 3, parts[1])
		}
	case "replay":
		if len(parts[1]) == 36 {
			return deepLinkResult(c, 2, parts[1])
		}
	}
	return deepLinkResult(c, 0, "")
}
