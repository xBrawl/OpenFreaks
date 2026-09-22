package mainserver

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
	"sort"
	"time"
)

func (s *Server) teamMemberProto(t *store.Team, pid int64) *protocol.TeamMember {
	acc, _ := s.store.GetAccount(pid)
	m := protocol.TeamMember{PlayerID: pid, State: 1}
	if acc != nil {
		m.PlayerName = acc.Name
		m.CharacterModelID = acc.SelectedChar
		if ch, ok := acc.Characters[acc.SelectedChar]; ok {
			m.StarPowerModelID = ch.StarPowerID
			m.DashModelID = ch.DashID
			m.IsUltUpgraded = ch.IsUltUpgraded
			m.HPLevel = ch.HPLevel
			m.DamageLevel = ch.DamageLevel
			m.SpeedLevel = ch.SpeedLevel
		}
		m.Trophies = acc.Trophies
	}
	if st, ok := t.Members[pid]; ok {
		m.State = st.State
	}
	m.IsOwner = t.OwnerID == pid
	return &m
}

func (s *Server) broadcastTeamUpdate(teamID int64) {
	t, ok := s.store.GetTeam(teamID)
	if !ok {
		return
	}
	w := protocol.NewWriter()
	w.WriteLong(t.ID)
	w.WriteInt(t.LocationID)
	w.WriteInt(int32(len(t.Members)))
	for pid := range t.Members {
		s.teamMemberProto(t, pid).Encode(w)
	}
	w.WriteInt(int32(len(t.Pending)))
	for pid, typ := range t.Pending {
		name := ""
		if acc, ok := s.store.GetAccount(pid); ok {
			name = acc.Name
		}
		(&protocol.TeamPendingMember{PlayerID: pid, PlayerName: name, Type: typ}).Encode(w)
	}
	payload := w.Bytes()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for pid := range t.Members {
		if conn, ok := s.byPlayer[pid]; ok {
			conn.Send(protocol.STeamUpdate, payload)
		}
	}

}

func (s *Server) broadcastTeam(c *Conn) {
	if c.TeamID != 0 {
		s.broadcastTeamUpdate(c.TeamID)
	}
}

func (s *Server) teamCapacity(location int32) int {
	location = s.dailyLocationID(location)
	loc := s.catData.LocByID[int(location)]
	if loc == nil || loc.Hidden {
		return 0
	}
	mode := s.catData.ModeByName[loc.Mode]
	if mode == nil {
		return 0
	}
	return max(1, mode.TeamPlayerCount)
}
func (s *Server) teamBusy(t *store.Team) bool {
	for _, member := range t.Members {
		if member.State >= 4 {
			return true
		}
	}
	return false
}
func (s *Server) clearTeamPending(pid int64) {
	for _, t := range s.store.ListTeams() {
		delete(t.Pending, pid)
		delete(t.PendingAt, pid)
	}
}
func pendingTeam(t *store.Team, pid int64, kind int32) bool {
	v, ok := t.Pending[pid]
	return ok && v == kind && (t.PendingAt[pid] == 0 || time.Now().Unix()-t.PendingAt[pid] < 300)
}
func setTeamPending(t *store.Team, pid int64, kind int32) {
	if t.PendingAt == nil {
		t.PendingAt = map[int64]int64{}
	}
	t.Pending[pid] = kind
	t.PendingAt[pid] = time.Now().Unix()
}
func (s *Server) onTeamCreate(c *Conn, payload []byte) bool {
	location, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	location = s.dailyLocationID(location)
	if s.teamCapacity(location) == 0 || c.BattleID != "" {
		return true
	}
	if t := s.store.TeamForPlayer(c.PlayerID); t != nil {
		c.TeamID = t.ID
		s.broadcastTeamUpdate(t.ID)
		return true
	}
	t := s.store.CreateTeam(c.PlayerID, location)
	c.TeamID = t.ID
	s.clearTeamPending(c.PlayerID)
	s.broadcastTeamUpdate(t.ID)
	return true
}
func (s *Server) onTeamLocationChange(c *Conn, payload []byte) bool {
	location, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	location = s.dailyLocationID(location)
	t, ok := s.store.GetTeam(c.TeamID)
	if ok && t.OwnerID == c.PlayerID && !s.teamBusy(t) && s.teamCapacity(location) >= len(t.Members) {
		t.LocationID = location
		for _, member := range t.Members {
			member.State = 1
		}
		s.broadcastTeamUpdate(t.ID)
	}
	return true
}
func (s *Server) onTeamInvite(c *Conn, payload []byte) bool {
	target, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	t, ok := s.store.GetTeam(c.TeamID)
	code := int32(1)
	if ok && t.OwnerID == c.PlayerID && !s.teamBusy(t) && len(t.Members) < s.teamCapacity(t.LocationID) && len(t.Pending) < 20 && target != c.PlayerID {
		if peer, online := s.ConnByPlayer(target); online && peer.BattleID == "" && s.store.TeamForPlayer(target) == nil {
			setTeamPending(t, target, 0)
			w := protocol.NewWriter()
			w.WriteLong(t.ID)
			w.WriteInt(t.LocationID)
			a, _ := s.store.GetAccount(c.PlayerID)
			inv := protocol.PlayerCompact{PlayerID: c.PlayerID, PlayerName: a.Name, Trophies: a.Trophies}
			inv.Encode(w)
			peer.Send(protocol.STeamInvitation, w.Bytes())
			s.broadcastTeamUpdate(t.ID)
			code = 0
		} else {
			code = 2
		}
	}
	return c.Send(protocol.STeamInviteResponse, protocol.EncodeCodeBool(code, code == 0))
}
func (s *Server) onTeamInvitationReply(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	id, err := r.ReadLong()
	if err != nil {
		return false
	}
	accept, err := r.ReadBool()
	if err != nil || !r.Ended() {
		return false
	}
	t, ok := s.store.GetTeam(id)
	success := false
	if ok && pendingTeam(t, c.PlayerID, 0) {
		if !accept {
			success = true
			delete(t.Pending, c.PlayerID)
			delete(t.PendingAt, c.PlayerID)
		} else if !s.teamBusy(t) && len(t.Members) < s.teamCapacity(t.LocationID) && s.store.TeamForPlayer(c.PlayerID) == nil && c.BattleID == "" {
			t.Members[c.PlayerID] = &store.TeamMemberState{PlayerID: c.PlayerID, State: 1}
			c.TeamID = t.ID
			s.clearTeamPending(c.PlayerID)
			success = true
		}
		s.broadcastTeamUpdate(t.ID)
	}
	code := int32(1)
	if success {
		code = 0
	}
	return c.Send(protocol.STeamInvitationReplyResponse, protocol.EncodeCodeBool(code, success))
}
func (s *Server) onTeamJoin(c *Conn, payload []byte) bool {
	id, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	t, ok := s.store.GetTeam(id)
	success := ok && !s.teamBusy(t) && len(t.Members) < s.teamCapacity(t.LocationID) && len(t.Pending) < 20 && s.store.TeamForPlayer(c.PlayerID) == nil && c.BattleID == ""
	if success {
		if _, exists := t.Pending[c.PlayerID]; exists {
			success = false
		} else {
			setTeamPending(t, c.PlayerID, 1)
			s.broadcastTeamUpdate(t.ID)
		}
	}
	code := int32(1)
	if success {
		code = 0
	}
	return c.Send(protocol.STeamJoinResponse, protocol.EncodeCodeBool(code, success))
}
func (s *Server) onTeamJoinReply(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	target, err := r.ReadLong()
	if err != nil {
		return false
	}
	accept, err := r.ReadBool()
	if err != nil || !r.Ended() {
		return false
	}
	t, ok := s.store.GetTeam(c.TeamID)
	success := false
	if ok && t.OwnerID == c.PlayerID && pendingTeam(t, target, 1) {
		peer, online := s.ConnByPlayer(target)
		if !accept {
			delete(t.Pending, target)
			delete(t.PendingAt, target)
			success = true
		} else if online && peer.BattleID == "" && s.store.TeamForPlayer(target) == nil && !s.teamBusy(t) && len(t.Members) < s.teamCapacity(t.LocationID) {
			t.Members[target] = &store.TeamMemberState{PlayerID: target, State: 1}
			peer.TeamID = t.ID
			s.clearTeamPending(target)
			success = true
		}
		if success {
			s.broadcastTeamUpdate(t.ID)
		}
	}
	code := int32(1)
	if success {
		code = 0
	}
	return c.Send(protocol.STeamJoinReplyResponse, protocol.EncodeCodeBool(code, success))
}
func (s *Server) onTeamState(c *Conn, payload []byte) bool {
	state, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	if state < 1 || state > 3 {
		return true
	}
	if t, ok := s.store.GetTeam(c.TeamID); ok {
		if member := t.Members[c.PlayerID]; member != nil && member.State < 4 {
			member.State = state
			s.broadcastTeamUpdate(t.ID)
		}
	}
	return true
}
func teamDissolve(c *Conn, reason int32) {
	w := protocol.NewWriter()
	w.WriteInt(reason)
	c.Send(protocol.STeamDissolve, w.Bytes())
}
func (s *Server) onTeamKick(c *Conn, payload []byte) bool {
	target, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	t, ok := s.store.GetTeam(c.TeamID)
	if !ok || t.OwnerID != c.PlayerID || target == c.PlayerID || s.teamBusy(t) {
		return true
	}
	if _, member := t.Members[target]; member {
		delete(t.Members, target)
		if peer, ok := s.ConnByPlayer(target); ok && peer.TeamID == t.ID {
			peer.TeamID = 0
			teamDissolve(peer, 1)
		}
	}
	delete(t.Pending, target)
	delete(t.PendingAt, target)
	s.broadcastTeamUpdate(t.ID)
	return true
}
func (s *Server) onTeamLeave(c *Conn) bool {
	t, ok := s.store.GetTeam(c.TeamID)
	if !ok {
		c.TeamID = 0
		return true
	}
	if s.mm != nil {
		s.mm.Cancel(c.PlayerID)
	}
	delete(t.Members, c.PlayerID)
	c.TeamID = 0
	teamDissolve(c, 0)
	if len(t.Members) == 0 || t.OwnerID == c.PlayerID {
		for id := range t.Members {
			if peer, ok := s.ConnByPlayer(id); ok {
				peer.TeamID = 0
				teamDissolve(peer, 0)
			}
		}
		s.store.DeleteTeam(t.ID)
	} else {
		s.broadcastTeamUpdate(t.ID)
	}
	return true
}
func (s *Server) onAvailableTeams(c *Conn) bool {
	list := []*store.Team{}
	for _, t := range s.store.ListTeams() {
		if !s.teamBusy(t) && len(t.Members) < s.teamCapacity(t.LocationID) {
			list = append(list, t)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	if len(list) > 20 {
		list = list[:20]
	}
	w := protocol.NewWriter()
	w.WriteInt(int32(len(list)))
	for _, t := range list {
		entry := protocol.TeamCompact{TeamID: t.ID, State: t.State, LocationModelID: t.LocationID}
		for id := range t.Members {
			if a, ok := s.store.GetAccount(id); ok {
				entry.Members = append(entry.Members, protocol.PlayerCompact{PlayerID: id, PlayerName: a.Name, Trophies: a.Trophies})
			}
		}
		entry.Encode(w)
	}
	candidates := []protocol.PlayerCompact{}
	s.mu.RLock()
	for id, peer := range s.byPlayer {
		if id == c.PlayerID || peer.BattleID != "" || s.store.TeamForPlayer(id) != nil {
			continue
		}
		if a, ok := s.store.GetAccount(id); ok {
			candidates = append(candidates, protocol.PlayerCompact{PlayerID: id, PlayerName: a.Name, Trophies: a.Trophies})
		}
	}
	s.mu.RUnlock()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].PlayerID < candidates[j].PlayerID })
	if len(candidates) > 20 {
		candidates = candidates[:20]
	}
	w.WriteInt(int32(len(candidates)))
	for _, p := range candidates {
		p.Encode(w)
	}
	return c.Send(protocol.SAvailableTeamsResponse, w.Bytes())
}
