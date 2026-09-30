package mainserver

import (
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

func banPayload(target int64, reason, hours int32, note string) []byte {
	w := protocol.NewWriter()
	w.WriteLong(target)
	w.WriteInt(reason)
	w.WriteInt(hours)
	w.WriteString(note)
	return w.Bytes()
}

func staffCode(t *testing.T, c *Conn) (int32, string) {
	t.Helper()
	replies := frames(c, protocol.SStaffResponse)
	if len(replies) != 1 {
		t.Fatalf("ответов модерации: %d", len(replies))
	}
	r := protocol.NewReader(replies[0])
	code, _ := r.ReadInt()
	text, _, _ := r.ReadString()
	return code, text
}

func decodeBan(t *testing.T, payload []byte) protocol.BanInfo {
	t.Helper()
	r := protocol.NewReader(payload)
	var b protocol.BanInfo
	var err error
	if b.Reason, err = r.ReadInt(); err != nil {
		t.Fatal(err)
	}
	b.Until, _ = r.ReadLong()
	b.Note, _, _ = r.ReadString()
	b.By, _, err = r.ReadString()
	if err != nil || !r.Ended() {
		t.Fatalf("блокировка не разобралась: %v", err)
	}
	return b
}

func TestModeratorBansForRulesWithNoteAndPlayerIsKicked(t *testing.T) {
	s, mod, acc := metaServer(t)
	acc.Role = store.RoleModerator
	mod.staff.Store(true)
	target := addPeer(s, "Seller")
	target.staff.Store(true)

	// Нарушение правил без конкретной причины не принимается.
	s.dispatch(mod, protocol.CStaffBanRequest, banPayload(target.PlayerID, store.BanRules, 24, "  "))
	if code, _ := staffCode(t, mod); code != staffBadArgs {
		t.Fatalf("без причины: код %d", code)
	}

	if !s.dispatch(mod, protocol.CStaffBanRequest, banPayload(target.PlayerID, store.BanRules, 24, "Продажа готовых аккаунтов")) {
		t.Fatal("dispatch")
	}
	if code, text := staffCode(t, mod); code != staffOK {
		t.Fatalf("бан: %d %s", code, text)
	}
	banned := frames(target, protocol.SBanned)
	if len(banned) != 1 {
		t.Fatalf("игрок в сети не получил SBanned: %d", len(banned))
	}
	info := decodeBan(t, banned[0])
	if info.Reason != store.BanRules || info.Note != "Продажа готовых аккаунтов" || info.By != "MetaAudit" {
		t.Fatalf("блокировка: %+v", info)
	}
	if left := info.Until - time.Now().Unix(); left < 23*3600 || left > 24*3600+5 {
		t.Fatalf("срок: осталось %d с", left)
	}

	// Повторный вход закрыт — с причиной.
	again := &Conn{ID: 99, Server: s, SendCh: make(chan []byte, 64)}
	again.staff.Store(true)
	if !s.rejectBanned(again, target.PlayerID) {
		t.Fatal("заблокированный вошёл")
	}
	if len(frames(again, protocol.SBanned)) != 1 {
		t.Fatal("нет SBanned при входе")
	}
}

func TestBanRightsFollowSeniority(t *testing.T) {
	s, mod, acc := metaServer(t)
	mod.staff.Store(true)
	admin := addPeer(s, "Wifi1")
	adminAcc, _ := s.store.PeekAccount(admin.PlayerID)
	adminAcc.Role = store.RoleAdmin
	admin.staff.Store(true)
	player := addPeer(s, "Player")

	// Обычный игрок банить не может.
	s.dispatch(mod, protocol.CStaffBanRequest, banPayload(player.PlayerID, store.BanAbuse, 24, ""))
	if code, _ := staffCode(t, mod); code != staffNoRights {
		t.Fatalf("игрок забанил: %d", code)
	}
	// Модератор не банит старшего.
	acc.Role = store.RoleModerator
	s.dispatch(mod, protocol.CStaffBanRequest, banPayload(admin.PlayerID, store.BanAbuse, 24, ""))
	if code, _ := staffCode(t, mod); code != staffNoRights {
		t.Fatalf("модератор забанил администратора: %d", code)
	}
	// Роли назначает только администратор.
	w := protocol.NewWriter()
	w.WriteLong(player.PlayerID)
	w.WriteInt(store.RoleStaff)
	s.dispatch(mod, protocol.CStaffSetRoleRequest, w.Bytes())
	if code, _ := staffCode(t, mod); code != staffNoRights {
		t.Fatalf("модератор назначил роль: %d", code)
	}
	s.dispatch(admin, protocol.CStaffSetRoleRequest, w.Bytes())
	if code, _ := staffCode(t, admin); code != staffOK {
		t.Fatalf("администратор не назначил роль: %d", code)
	}
	if got := s.roleOf(player.PlayerID); got != store.RoleStaff {
		t.Fatalf("роль %d", got)
	}
	// Сотрудник банить не может.
	player.staff.Store(true)
	s.dispatch(player, protocol.CStaffBanRequest, banPayload(mod.PlayerID, store.BanAbuse, 24, ""))
	if code, _ := staffCode(t, player); code != staffNoRights {
		t.Fatalf("сотрудник забанил: %d", code)
	}

	// Бан навсегда и снятие.
	s.dispatch(admin, protocol.CStaffBanRequest, banPayload(player.PlayerID, store.BanAbuse, 0, ""))
	if code, _ := staffCode(t, admin); code != staffOK {
		t.Fatal("бан навсегда")
	}
	pacc, _ := s.store.PeekAccount(player.PlayerID)
	if !pacc.Ban.Active(time.Now().Add(1000*24*time.Hour).UnixMilli()) {
		t.Fatal("вечный бан истёк")
	}
	s.dispatch(admin, protocol.CStaffUnbanRequest, longPayload(player.PlayerID))
	if code, _ := staffCode(t, admin); code != staffOK || pacc.Ban.Active(time.Now().UnixMilli()) {
		t.Fatal("бан не снят")
	}
}

func TestStaffCommandsNeedTheFeatureBit(t *testing.T) {
	s, c, acc := metaServer(t)
	acc.Role = store.RoleAdmin
	if s.dispatch(c, protocol.CStaffBanRequest, banPayload(1, 1, 1, "")) {
		t.Fatal("старый клиент прислал команду модерации — соединение должно закрыться")
	}
}

func TestRoleRidesAfterAvatar(t *testing.T) {
	s, c, acc := metaServer(t)
	acc.Role = store.RoleDeveloper
	c.social.Store(true)
	c.staff.Store(true)
	w := protocol.NewWriter()
	s.socialFor(c, acc.PlayerID).Encode(w)
	r := protocol.NewReader(w.Bytes())
	avatar, _ := r.ReadInt()
	role, _ := r.ReadInt()
	if avatar != acc.SelectedChar || role != store.RoleDeveloper || !r.Ended() {
		t.Fatalf("хвост: аватарка %d роль %d", avatar, role)
	}
	c.staff.Store(false)
	w = protocol.NewWriter()
	s.socialFor(c, acc.PlayerID).Encode(w)
	if len(w.Bytes()) != 4 {
		t.Fatal("клиенту без FeatureStaff роль не пишется")
	}
}

func TestAnticheatBansEscalate(t *testing.T) {
	s, _, _ := metaServer(t)
	cheater := addPeer(s, "Cheater")
	acc, _ := s.store.PeekAccount(cheater.PlayerID)
	report := &battleserver.Result{BattleID: "b", Cheaters: []battleserver.CheatReport{{PlayerID: cheater.PlayerID, Reason: "поток действий", Score: 120}}}
	want := []int64{72, 30 * 24, 0}
	for i, hours := range want {
		s.runOperation(func() bool { s.applyCheatReports(report); return true })
		if acc.Ban == nil || !acc.Ban.Auto || acc.Ban.Reason != store.BanCheats {
			t.Fatalf("раз %d: нет автоблокировки", i)
		}
		got := int64(0)
		if acc.Ban.Until > 0 {
			got = (acc.Ban.Until - acc.Ban.At) / 3600000
		}
		if got != hours {
			t.Fatalf("раз %d: срок %d ч, ждали %d", i, got, hours)
		}
		// Модератор снял — следующий раз строже.
		acc.Ban.LiftedAt = time.Now().UnixMilli()
	}
}

// Клиент со staff читает роль после аватарки в каждом участнике отряда:
// без неё поток съезжал на 4 байта и новый клиент рвал соединение при входе.
func TestTeamUpdateCarriesRoleForStaffClients(t *testing.T) {
	s, owner, _ := metaServer(t)
	owner.social.Store(true)
	owner.staff.Store(true)
	social := addPeer(s, "SocialOnly")
	social.social.Store(true)
	loc := testLocation(s, "bounty")
	if !s.dispatch(owner, protocol.CTeamCreate, intPayload(loc)) {
		t.Fatal("team create failed")
	}
	team := s.store.TeamForPlayer(owner.PlayerID)
	s.dispatch(owner, protocol.CTeamInviteRequest, longPayload(social.PlayerID))
	s.dispatch(social, protocol.CTeamInvitationReplyRequest, payloadReply(team.ID, true))
	if len(team.Members) != 2 {
		t.Fatalf("members=%d", len(team.Members))
	}
	frames(owner, 0)
	frames(social, 0)
	s.broadcastTeamUpdate(team.ID)
	withRole := frames(owner, protocol.STeamUpdate)
	avatarOnly := frames(social, protocol.STeamUpdate)
	if len(withRole) != 1 || len(avatarOnly) != 1 {
		t.Fatal("no team updates")
	}
	if len(withRole[0])-len(avatarOnly[0]) != 8 {
		t.Fatalf("staff=%d social=%d: роль не дописана", len(withRole[0]), len(avatarOnly[0]))
	}
}
