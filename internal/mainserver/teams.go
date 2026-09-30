package mainserver

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
	"sort"
	"time"
)

func (s *Server) teamMemberProto(t *store.Team, pid int64, social, staff bool) *protocol.TeamMember {
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
	if social {
		m.Social = protocol.Social{Enabled: true, Avatar: avatarOf(acc), Staff: staff}
		if staff && acc != nil {
			m.Social.Role = acc.Role
		}
	}
	return &m
}

func (s *Server) broadcastTeamUpdate(teamID int64) {
	t, ok := s.store.GetTeam(teamID)
	if !ok {
		return
	}
	// Три варианта: прежний формат, с аватарками (FeatureSocialV2) и ещё с
	// ролью после аватарки (FeatureStaff) — клиент со staff её читает всегда.
	encode := func(social, staff bool) []byte {
		w := protocol.NewWriter()
		w.WriteLong(t.ID)
		w.WriteInt(t.LocationID)
		w.WriteInt(int32(len(t.Members)))
		for pid := range t.Members {
			s.teamMemberProto(t, pid, social, staff).Encode(w)
		}
		w.WriteInt(int32(len(t.Pending)))
		for pid, typ := range t.Pending {
			pending := protocol.TeamPendingMember{PlayerID: pid, Type: typ}
			if acc, ok := s.store.GetAccount(pid); ok {
				pending.PlayerName = acc.Name
				if social {
					pending.Social = protocol.Social{Enabled: true, Avatar: avatarOf(acc), Staff: staff, Role: acc.Role}
				}
			}
			pending.Encode(w)
		}
		return w.Bytes()
	}
	legacy, social, staff := encode(false, false), encode(true, false), encode(true, true)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for pid := range t.Members {
		if conn, ok := s.byPlayer[pid]; ok {
			if conn.social.Load() && conn.staff.Load() {
				conn.Send(protocol.STeamUpdate, staff)
			} else if conn.social.Load() {
				conn.Send(protocol.STeamUpdate, social)
			} else {
				conn.Send(protocol.STeamUpdate, legacy)
			}
		}
	}

}

func (s *Server) broadcastTeam(c *Conn) {
	if c.TeamID != 0 {
		s.broadcastTeamUpdate(c.TeamID)
	}
}

// maxPartySize — сколько человек можно собрать в команду в любом режиме.
// Раньше предел задавал выбранный режим (обычно 3), и шестерых на мегабосса
// можно было позвать, только если заранее переключиться на него. Играть
// командой больше, чем берёт режим, по-прежнему нельзя: это проверяет очередь
// (Enqueue), а клиент говорит об этом на кнопке «Играть».
const maxPartySize = 6

// teamCapacity — сколько игроков одной команды берёт режим локации.
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
// teamReady — «Играть» в команде. Клиент никогда не слал отдельное «готов»
// (TeamMemberStateChange с 3), а очередь принимала команду, только когда все,
// кроме хозяина, готовы: нажатие «Играть» в команде из двух и больше просто
// отменялось. Теперь «Играть» и есть «готов», а когда готовы все, команда сама
// встаёт в поиск от имени хозяина.
func (s *Server) teamReady(c *Conn, t *store.Team) bool {
	member := t.Members[c.PlayerID]
	if member == nil || member.State >= 4 || s.teamBusy(t) {
		return true
	}
	member.State = 3
	for _, m := range t.Members {
		if m.State != 3 {
			s.broadcastTeamUpdate(t.ID)
			return true
		}
	}
	if s.mm != nil && s.mm.Enqueue(t.OwnerID, t.LocationID) {
		return true
	}
	// Очередь не приняла (кто-то офлайн или ещё в бою): готовность снимается у
	// всех, иначе команда застрянет в «готовы» без поиска.
	for id, m := range t.Members {
		if m.State == 3 {
			m.State = 1
		}
		if peer, ok := s.ConnByPlayer(id); ok {
			peer.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
		}
	}
	s.broadcastTeamUpdate(t.ID)
	return true
}

// teamUnready — «Отмена» до начала поиска снимает только свою готовность.
func (s *Server) teamUnready(pid int64) {
	t := s.store.TeamForPlayer(pid)
	if t == nil {
		return
	}
	if member := t.Members[pid]; member != nil && member.State == 3 {
		member.State = 1
		s.broadcastTeamUpdate(t.ID)
	}
}

// releaseTeamMember возвращает участника команды из боя в лобби. Раньше статус
// «в бою» снимал только тот, кто досидел матч до конца: вышедший раньше (умер в
// выживании и нажал «Выйти») навсегда держал команду занятой, и сервер отвечал
// «команда заполнена» на любое приглашение.
func (s *Server) releaseTeamMember(pid int64) {
	t := s.store.TeamForPlayer(pid)
	if t == nil {
		return
	}
	member := t.Members[pid]
	if member == nil || member.State < 5 {
		return
	}
	member.State = 1
	s.broadcastTeamUpdate(t.ID)
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
	if ok && t.OwnerID == c.PlayerID && !s.teamBusy(t) && s.teamCapacity(location) > 0 {
		t.LocationID = location
		for _, member := range t.Members {
			member.State = 1
		}
		s.broadcastTeamUpdate(t.ID)
	}
	return true
}

// Команды живут на сервере и после выхода из игры: кто однажды создал команду
// и остался в ней один, навсегда считался «уже в команде», и позвать его было
// нельзя. Одиночная незанятая команда больше не мешает — при согласии она
// распускается.
func (s *Server) soloTeam(pid int64) *store.Team {
	t := s.store.TeamForPlayer(pid)
	if t == nil || len(t.Members) != 1 || s.teamBusy(t) {
		return nil
	}
	if _, ok := t.Members[pid]; !ok {
		return nil
	}
	return t
}

func (s *Server) freeForTeam(pid int64) bool {
	return s.store.TeamForPlayer(pid) == nil || s.soloTeam(pid) != nil
}

// leaveSoloTeam распускает одиночную команду игрока перед переходом в другую.
func (s *Server) leaveSoloTeam(pid int64, into int64) {
	t := s.soloTeam(pid)
	if t == nil || t.ID == into {
		return
	}
	s.store.DeleteTeam(t.ID)
	if peer, ok := s.ConnByPlayer(pid); ok && peer.TeamID == t.ID {
		peer.TeamID = 0
		teamDissolve(peer, 0)
	}
}

// Коды ответа на приглашение — как их читает клиент (TeamInviteResponse).
const (
	inviteTeamFull      = 1
	inviteTargetOffline = 2
	inviteTargetInTeam  = 3
)

func (s *Server) onTeamInvite(c *Conn, payload []byte) bool {
	target, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	t, ok := s.store.GetTeam(c.TeamID)
	code := int32(inviteTeamFull)
	if ok && t.OwnerID == c.PlayerID && !s.teamBusy(t) && len(t.Members) < maxPartySize && len(t.Pending) < 20 && target != c.PlayerID {
		peer, online := s.ConnByPlayer(target)
		if !online || peer.BattleID != "" {
			code = inviteTargetOffline
		} else if !s.freeForTeam(target) {
			code = inviteTargetInTeam
		} else {
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
		} else if !s.teamBusy(t) && len(t.Members) < maxPartySize && s.freeForTeam(c.PlayerID) && c.BattleID == "" {
			s.leaveSoloTeam(c.PlayerID, t.ID)
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
	success := ok && !s.teamBusy(t) && len(t.Members) < maxPartySize && len(t.Pending) < 20 && s.freeForTeam(c.PlayerID) && c.BattleID == "" && t.ID != c.TeamID
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
		} else if online && peer.BattleID == "" && s.freeForTeam(target) && !s.teamBusy(t) && len(t.Members) < maxPartySize {
			s.leaveSoloTeam(target, t.ID)
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
		member := t.Members[c.PlayerID]
		// Застрявший «в бою» / «в поиске» статус снимается, если игрок уже
		// ни в бою, ни в очереди: клиент шлёт «не готов» после каждого матча.
		if member != nil && member.State >= 4 && c.BattleID == "" && (s.mm == nil || !s.mm.Queued(c.PlayerID)) {
			member.State = 1
		}
		if member != nil && member.State < 4 {
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
		if !s.teamBusy(t) && len(t.Members) < maxPartySize {
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
		// Одиночная команда не мешает: её распускают при согласии. Раньше
		// каждый, кто однажды звал друга, навсегда пропадал из этого списка.
		if id == c.PlayerID || peer.BattleID != "" || !s.freeForTeam(id) {
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
