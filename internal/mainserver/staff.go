package mainserver

// Служебные роли и блокировки.
//
// Роли по старшинству: Сотрудник (только подпись) → Модератор → Разработчик →
// Администратор. Блокировать может модератор и выше — и только тех, кто
// младше его самого; роли назначает администратор. Подпись роли видна в
// профиле, в чатах шайки и отряда и над бойцом в бою.
//
// Совместимость — как у FeatureSocialV2: сервер объявляет FeatureStaff в
// приветствии, клиент поднимает бит в Login, и только тогда роль идёт хвостом
// записи игрока (после аватарки), роли — хвостом BattleStarted, блокировка —
// хвостом профиля, а команды 70–72 принимаются.
//
// Заблокированный игрок при входе получает SBanned (причина, срок, кто) и
// LoginFailed; если он в сети в момент блокировки — SBanned, и соединение
// закрывается. Античит боя (battleserver/anticheat.go) присылает нарушителей
// в итоге матча; блокировка тогда автоматическая: 3 дня, 30 дней, навсегда.

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

// Разовая выдача по явно настроенным неизменным ID. Совпадение ника
// с именем сотрудника не подтверждает его личность.
const staffRolesGrantKey = "staff-roles-by-id-v1"

const (
	staffOK       int32 = 0
	staffNoRights int32 = 1
	staffNotFound int32 = 2
	staffBadArgs  int32 = 3
)

const banNoteLength = 120

// Самое долгое, что можно выбрать срочной блокировкой, — год.
const maxBanHours = 24 * 365

func (s *Server) roleOf(pid int64) int32 {
	a, ok := s.store.PeekAccount(pid)
	if !ok {
		return store.RoleNone
	}
	return a.Role
}

func canBan(role int32) bool { return role >= store.RoleModerator }

func (s *Server) grantStaffRoles() {
	if len(s.cfg.Authentication.BootstrapStaffRoles) == 0 {
		return
	}
	s.runOperation(func() bool {
		changed := false
		for pid, role := range s.cfg.Authentication.BootstrapStaffRoles {
			if pid <= 0 || role < store.RoleStaff || role > store.RoleAdmin {
				continue
			}
			meta := s.store.Meta.Player(pid)
			if _, done := meta.Completed[staffRolesGrantKey]; done {
				continue
			}
			acc, ok := s.store.GetAccount(pid)
			if !ok {
				continue
			}
			acc.Role = role
			meta.Completed[staffRolesGrantKey] = time.Now().UnixMilli()
			s.log.Info("staff role granted", "player", pid, "name", acc.Name, "role", role)
			changed = true
		}
		return changed
	})
}

// banInfo — блокировка игрока для клиента; nil — не заблокирован.
func banInfo(a *store.Account, now int64) *protocol.BanInfo {
	if a == nil || !a.Ban.Active(now) {
		return nil
	}
	info := &protocol.BanInfo{Reason: a.Ban.Reason, Note: a.Ban.Note, By: a.Ban.ByName}
	if a.Ban.Until > 0 {
		info.Until = a.Ban.Until / 1000
	}
	return info
}

// rejectBanned отвечает заблокированному игроку на вход. true — вход закрыт.
func (s *Server) rejectBanned(c *Conn, pid int64) bool {
	a, ok := s.store.PeekAccount(pid)
	if !ok {
		return false
	}
	info := banInfo(a, time.Now().UnixMilli())
	if info == nil {
		return false
	}
	if c.staff.Load() {
		c.Send(protocol.SBanned, protocol.EncodeBanned(info))
	}
	s.failLogin(c, 9)
	return true
}

func (s *Server) staffReply(c *Conn, code int32, text string) bool {
	return c.Send(protocol.SStaffResponse, protocol.EncodeStaffResponse(code, text))
}

func (s *Server) onStaffBan(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	target, err1 := r.ReadLong()
	reason, err2 := r.ReadInt()
	hours, err3 := r.ReadInt()
	note, _, err4 := r.ReadString()
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || !r.Ended() {
		return false
	}
	actor, ok := s.store.GetAccount(c.PlayerID)
	if !ok || !canBan(actor.Role) {
		return s.staffReply(c, staffNoRights, "Блокировать могут только модераторы")
	}
	acc, ok := s.store.GetAccount(target)
	if !ok {
		return s.staffReply(c, staffNotFound, "Игрок не найден")
	}
	if target == c.PlayerID || acc.Role >= actor.Role {
		return s.staffReply(c, staffNoRights, "Этого игрока вы заблокировать не можете")
	}
	note = strings.TrimSpace(cleanSocialText(note))
	if reason < store.BanCheats || reason > store.BanReasonMax || hours < 0 || hours > maxBanHours ||
		utf8.RuneCountInString(note) > banNoteLength {
		return s.staffReply(c, staffBadArgs, "Проверьте причину и срок")
	}
	if reason == store.BanRules && note == "" {
		return s.staffReply(c, staffBadArgs, "Напишите, какое правило нарушено")
	}
	s.applyBan(acc, store.Ban{Reason: reason, Note: note, Hours: hours, ByID: actor.PlayerID, ByName: actor.Name})
	s.log.Warn("player banned", "player", target, "name", acc.Name, "by", actor.Name, "reason", reason, "hours", hours, "note", note)
	return s.staffReply(c, staffOK, acc.Name+" заблокирован")
}

// applyBan накладывает блокировку и выкидывает игрока, если он в сети.
func (s *Server) applyBan(acc *store.Account, ban store.Ban) {
	now := time.Now().UnixMilli()
	ban.At = now
	if ban.Hours > 0 {
		ban.Until = now + int64(ban.Hours)*3600*1000
	}
	ban.Hours = 0
	if acc.Ban != nil {
		acc.Bans = append(acc.Bans, *acc.Ban)
		if len(acc.Bans) > 20 {
			acc.Bans = acc.Bans[len(acc.Bans)-20:]
		}
	}
	acc.Ban = &ban
	conn, online := s.ConnByPlayer(acc.PlayerID)
	if !online {
		return
	}
	if conn.staff.Load() {
		conn.Send(protocol.SBanned, protocol.EncodeBanned(banInfo(acc, now)))
	} else {
		conn.Send(protocol.SDisconnected, protocol.EncodeSingleIgnoredByte())
	}
	// Даём сообщению уйти, потом рвём соединение: removeConn уберёт игрока
	// из поиска и отряда.
	time.AfterFunc(1500*time.Millisecond, func() {
		if conn.Conn != nil {
			conn.Conn.Close()
		}
	})
}

func (s *Server) onStaffUnban(c *Conn, payload []byte) bool {
	target, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	actor, ok := s.store.GetAccount(c.PlayerID)
	if !ok || !canBan(actor.Role) {
		return s.staffReply(c, staffNoRights, "Снимать блокировку могут только модераторы")
	}
	acc, ok := s.store.GetAccount(target)
	if !ok {
		return s.staffReply(c, staffNotFound, "Игрок не найден")
	}
	if !acc.Ban.Active(time.Now().UnixMilli()) {
		return s.staffReply(c, staffOK, acc.Name+" не заблокирован")
	}
	if acc.Role >= actor.Role {
		return s.staffReply(c, staffNoRights, "Этого игрока вы разблокировать не можете")
	}
	lifted := *acc.Ban
	lifted.LiftedAt = time.Now().UnixMilli()
	lifted.LiftedBy = actor.Name
	acc.Ban = &lifted
	s.log.Warn("player unbanned", "player", target, "name", acc.Name, "by", actor.Name)
	return s.staffReply(c, staffOK, acc.Name+" разблокирован")
}

func (s *Server) onStaffSetRole(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	target, err1 := r.ReadLong()
	role, err2 := r.ReadInt()
	if err1 != nil || err2 != nil || !r.Ended() {
		return false
	}
	actor, ok := s.store.GetAccount(c.PlayerID)
	if !ok || actor.Role < store.RoleAdmin {
		return s.staffReply(c, staffNoRights, "Роли назначает только администратор")
	}
	acc, ok := s.store.GetAccount(target)
	if !ok {
		return s.staffReply(c, staffNotFound, "Игрок не найден")
	}
	if role < store.RoleNone || role > store.RoleAdmin || target == c.PlayerID {
		return s.staffReply(c, staffBadArgs, "Эту роль назначить нельзя")
	}
	acc.Role = role
	s.log.Warn("staff role changed", "player", target, "name", acc.Name, "by", actor.Name, "role", role)
	// Подпись видна в шайке и у друзей: обновляем их открытые экраны.
	if al, ok := s.playerAlliance(target); ok {
		s.pushAlliance(al)
	}
	for _, id := range s.store.FriendList(target) {
		s.pushFriendUpdate(id, target)
	}
	return s.staffReply(c, staffOK, "Роль игрока "+acc.Name+" изменена")
}

// sendBattleStarted дописывает роли игроков клиентам с FeatureStaff.
func (s *Server) sendBattleStarted(c *Conn, msg *protocol.BattleStartedMsg) bool {
	msg.WithRoles = c.staff.Load()
	if msg.WithRoles {
		for i := range msg.Players {
			msg.Players[i].Role = store.RoleNone
			if id := msg.Players[i].PlayerID; id > 0 {
				msg.Players[i].Role = s.roleOf(id)
			}
		}
	}
	return c.Send(protocol.SBattleStarted, msg.Encode())
}

// applyCheatReports блокирует тех, кого античит боя поймал на подделке
// клиента. Срок растёт с каждым разом: 3 дня, 30 дней, навсегда.
func (s *Server) applyCheatReports(r *battleserver.Result) {
	for _, report := range r.Cheaters {
		acc, ok := s.store.GetAccount(report.PlayerID)
		if !ok || report.PlayerID <= 0 || acc.Role >= store.RoleDeveloper || acc.Ban.Active(time.Now().UnixMilli()) {
			continue
		}
		previous := 0
		for _, b := range acc.Bans {
			if b.Auto {
				previous++
			}
		}
		if acc.Ban != nil && acc.Ban.Auto {
			previous++
		}
		var hours int32 = 72
		switch {
		case previous == 1:
			hours = 30 * 24
		case previous >= 2:
			hours = 0
		}
		s.applyBan(acc, store.Ban{Reason: store.BanCheats, Note: "Античит: " + report.Reason, Hours: hours, ByName: "Античит", Auto: true})
		s.log.Warn("anticheat ban", "player", report.PlayerID, "name", acc.Name, "battle", r.BattleID, "reason", report.Reason, "score", report.Score, "hours", hours)
	}
}
