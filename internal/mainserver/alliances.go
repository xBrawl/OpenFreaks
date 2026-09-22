package mainserver

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func cleanSocialText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		if r == '<' {
			return '＜'
		}
		return r
	}, strings.TrimSpace(text))
}
func (s *Server) allianceProto(al *store.Alliance, viewer int64) *protocol.Alliance {
	result := &protocol.Alliance{AllianceID: al.ID, Name: al.Name, Description: al.Description, Type: al.Type, PlayerCount: int32(len(al.Members))}
	row := func(pid int64, role int32) protocol.AlliancePlayer {
		p := protocol.AlliancePlayer{PlayerID: pid, RoleModelID: role}
		if a, ok := s.store.GetAccount(pid); ok {
			p.PlayerName = a.Name
			p.IsOnline = a.Online
			p.LastOnlineAgo = int32(min(2147483647, max(0, time.Now().Unix()-unixSeconds(a.LastOnlineUnix))))
		}
		return p
	}
	for id, role := range al.Members {
		result.Members = append(result.Members, row(id, role))
	}
	sort.Slice(result.Members, func(i, j int) bool {
		a, b := result.Members[i], result.Members[j]
		if a.RoleModelID != b.RoleModelID {
			return a.RoleModelID > b.RoleModelID
		}
		return a.PlayerID < b.PlayerID
	})
	if _, member := al.Members[viewer]; member {
		for id := range al.JoinRequests {
			result.Joining = append(result.Joining, row(id, 1))
		}
		sort.Slice(result.Joining, func(i, j int) bool { return result.Joining[i].PlayerID < result.Joining[j].PlayerID })
	}
	return result
}
func (s *Server) allianceAllowed(al *store.Alliance, pid int64, permission string) bool {
	role, member := al.Members[pid]
	return member && s.catData.SpecByID("alliance_roles", int(role)).Bool(permission)
}
func (s *Server) pushAlliance(al *store.Alliance) {
	for pid := range al.Members {
		if c, ok := s.ConnByPlayer(pid); ok {
			w := protocol.NewWriter()
			s.allianceProto(al, pid).Encode(w)
			c.Send(protocol.SAllianceDataResponse, w.Bytes())
		}
	}
}
func (s *Server) membershipEvent(pid, id int64, status int32) {
	if c, ok := s.ConnByPlayer(pid); ok {
		w := protocol.NewWriter()
		w.WriteLong(id)
		w.WriteInt(status)
		c.Send(protocol.SAllianceJoinApprovedOrKicked, w.Bytes())
		s.sendHome(c)
	}
}
func (s *Server) clearAllianceRequests(pid int64) {
	s.store.ClearJoinRequests(pid)
}
func (s *Server) onAllianceSearch(c *Conn, payload []byte) bool {
	q, _, err := protocol.DecodeStringOnly(payload)
	if err != nil {
		return false
	}
	q = strings.TrimSpace(strings.TrimPrefix(q, "#"))
	list := s.store.SearchAlliance(q)
	if id, e := strconv.ParseInt(q, 10, 64); e == nil {
		if al, ok := s.store.GetAlliance(id); ok {
			list = []*store.Alliance{al}
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	w := protocol.NewWriter()
	w.WriteInt(int32(len(list)))
	for _, al := range list {
		s.allianceProto(al, c.PlayerID).Encode(w)
	}
	return c.Send(protocol.SAllianceSearchResponse, w.Bytes())
}
func (s *Server) onAllianceCreate(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	name, _, err := r.ReadString()
	if err != nil {
		return false
	}
	description, _, err := r.ReadString()
	if err != nil {
		return false
	}
	kind, err := r.ReadInt()
	if err != nil || !r.Ended() {
		return false
	}
	name, description = cleanSocialText(name), cleanSocialText(description)
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	if a.AllianceID > 0 || kind < 0 || kind > 2 || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 || utf8.RuneCountInString(description) > 1000 {
		return c.Send(protocol.SAllianceCreateResponse, encodeAllianceCreate(1, false, 0))
	}
	al := s.store.CreateAlliance(name, description, kind, c.PlayerID)
	s.clearAllianceRequests(c.PlayerID)
	c.Send(protocol.SAllianceCreateResponse, encodeAllianceCreate(0, true, al.ID))
	return s.sendHome(c)
}
func (s *Server) onAllianceEdit(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	description, _, err := r.ReadString()
	if err != nil {
		return false
	}
	kind, err := r.ReadInt()
	if err != nil || !r.Ended() {
		return false
	}
	al, ok := s.playerAlliance(c.PlayerID)
	success := ok && kind >= 0 && kind <= 2 && utf8.RuneCountInString(description) <= 1000 && s.allianceAllowed(al, c.PlayerID, "is_edit_permitted")
	if success {
		al.Description = cleanSocialText(description)
		al.Type = kind
		s.pushAlliance(al)
	}
	code := int32(1)
	if success {
		code = 0
	}
	return c.Send(protocol.SAllianceEditResponse, protocol.EncodeCodeBool(code, success))
}
func (s *Server) onAllianceData(c *Conn, payload []byte) bool {
	id, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	al, ok := s.store.GetAlliance(id)
	if !ok {
		al = &store.Alliance{ID: id}
	}
	w := protocol.NewWriter()
	s.allianceProto(al, c.PlayerID).Encode(w)
	return c.Send(protocol.SAllianceDataResponse, w.Bytes())
}
func (s *Server) onAllianceJoin(c *Conn, payload []byte) bool {
	id, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	al, ok := s.store.GetAlliance(id)
	a, exists := s.store.GetAccount(c.PlayerID)
	if !exists {
		return false
	}
	code := int32(1)
	success := false
	if ok && a.AllianceID <= 0 && len(al.Members) < 50 && al.Type != 2 {
		if al.Type == 1 {
			code = 2
			if _, exists := al.JoinRequests[c.PlayerID]; exists {
				code = 3
			} else {
				al.JoinRequests[c.PlayerID] = time.Now().UnixMilli()
			}
			success = true
		} else {
			a.AllianceID = id
			al.Members[c.PlayerID] = 1
			s.clearAllianceRequests(c.PlayerID)
			code = 0
			success = true
		}
		s.pushAlliance(al)
	}
	c.Send(protocol.SAllianceJoinResponse, protocol.EncodeCodeBool(code, success))
	if success && code == 0 {
		return s.sendHome(c)
	}
	return true
}
func (s *Server) onAllianceJoinReply(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	target, err := r.ReadLong()
	if err != nil {
		return false
	}
	accept, err := r.ReadBool()
	if err != nil || !r.Ended() {
		return false
	}
	al, ok := s.playerAlliance(c.PlayerID)
	success := false
	if ok && s.allianceAllowed(al, c.PlayerID, "is_approve_or_kick_permitted") {
		if _, pending := al.JoinRequests[target]; pending {
			a, exists := s.store.GetAccount(target)
			if !accept {
				delete(al.JoinRequests, target)
				success = true
			} else if exists && a.AllianceID <= 0 && len(al.Members) < 50 {
				al.Members[target] = 1
				a.AllianceID = al.ID
				s.clearAllianceRequests(target)
				s.membershipEvent(target, al.ID, 0)
				success = true
			}
			if success {
				s.pushAlliance(al)
			}
		}
	}
	code := int32(1)
	if success {
		code = 0
	}
	return c.Send(protocol.SAllianceJoinReplyResponse, protocol.EncodeCodeBool(code, success))
}
func (s *Server) onAllianceKick(c *Conn, payload []byte) bool {
	target, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	al, ok := s.playerAlliance(c.PlayerID)
	success := false
	if ok && target != c.PlayerID && s.allianceAllowed(al, c.PlayerID, "is_approve_or_kick_permitted") {
		if role, member := al.Members[target]; member && role < al.Members[c.PlayerID] {
			delete(al.Members, target)
			if a, ok := s.store.GetAccount(target); ok && a.AllianceID == al.ID {
				a.AllianceID = -1
			}
			s.membershipEvent(target, al.ID, 1)
			s.pushAlliance(al)
			success = true
		}
	}
	code := int32(1)
	if success {
		code = 0
	}
	return c.Send(protocol.SAllianceKickResponse, protocol.EncodeCodeBool(code, success))
}
func (s *Server) onAllianceRoleChange(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	target, err := r.ReadLong()
	if err != nil {
		return false
	}
	role, err := r.ReadInt()
	if err != nil || !r.Ended() {
		return false
	}
	al, ok := s.playerAlliance(c.PlayerID)
	success := false
	if ok && target != c.PlayerID && s.catData.SpecByID("alliance_roles", int(role)).Exists() && s.allianceAllowed(al, c.PlayerID, "is_role_change_permitted") {
		if _, member := al.Members[target]; member {
			if role == 3 {
				al.Members[c.PlayerID] = 2
			}
			al.Members[target] = role
			s.pushAlliance(al)
			success = true
		}
	}
	code := int32(1)
	if success {
		code = 0
	}
	return c.Send(protocol.SAllianceRoleChangeResponse, protocol.EncodeCodeBool(code, success))
}
func (s *Server) leaveAlliance(pid int64) {
	al, ok := s.playerAlliance(pid)
	if !ok {
		return
	}
	s.store.LeaveAlliance(pid)
	if len(al.Members) > 0 {
		leader := false
		var candidate int64
		best := int32(-1)
		for id, role := range al.Members {
			if role == 3 {
				leader = true
			}
			if role > best || role == best && id < candidate {
				candidate, best = id, role
			}
		}
		if !leader {
			al.Members[candidate] = 3
		}
		s.pushAlliance(al)
	} else {
		s.store.DeleteAlliance(al.ID)
		for key, r := range s.store.Meta.Reactors {
			if r.AllianceID == al.ID {
				delete(s.store.Meta.Reactors, key)
			}
		}
	}
}
func (s *Server) onAllianceSend(c *Conn, payload []byte) bool {
	text, _, err := protocol.DecodeStringOnly(payload)
	if err != nil {
		return false
	}
	text = cleanSocialText(text)
	al, ok := s.playerAlliance(c.PlayerID)
	if !ok || utf8.RuneCountInString(text) < 1 || utf8.RuneCountInString(text) > 500 {
		return true
	}
	a, _ := s.store.GetAccount(c.PlayerID)
	message := store.AllianceMessage{PlayerID: c.PlayerID, PlayerName: a.Name, RoleModelID: al.Members[c.PlayerID], Message: text, CreationTime: time.Now().Unix()}
	al.Messages = append(al.Messages, message)
	if len(al.Messages) > 100 {
		al.Messages = al.Messages[len(al.Messages)-100:]
	}
	w := protocol.NewWriter()
	allianceMessage(message).Encode(w)
	for pid := range al.Members {
		if c, ok := s.ConnByPlayer(pid); ok {
			c.Send(protocol.SAllianceMessage, w.Bytes())
		}
	}
	return true
}
func allianceMessage(m store.AllianceMessage) *protocol.AllianceMessage {
	return &protocol.AllianceMessage{Type: m.Type, PlayerID: m.PlayerID, PlayerName: m.PlayerName, RoleModelID: max(1, m.RoleModelID), Message: m.Message, CreationTimestamp: unixSeconds(m.CreationTime)}
}
func (s *Server) onAllianceMessages(c *Conn) bool {
	al, ok := s.playerAlliance(c.PlayerID)
	w := protocol.NewWriter()
	if !ok {
		w.WriteInt(0)
	} else {
		w.WriteInt(int32(len(al.Messages)))
		for _, m := range al.Messages {
			allianceMessage(m).Encode(w)
		}
	}
	return c.Send(protocol.SAllianceMessagesResponse, w.Bytes())
}
