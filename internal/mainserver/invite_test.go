package mainserver

import (
	"bytes"
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

// Кто однажды создал команду и остался в ней один, раньше навсегда считался
// «уже в команде»: позвать его было нельзя.
func TestInviteReachesPlayerLeftAloneInOldTeam(t *testing.T) {
	s, c, _ := metaServer(t)
	friend := addPeer(s, "Friend")
	busy := addPeer(s, "Busy")
	buddy := addPeer(s, "Buddy")
	loc := testLocation(s, "bounty")
	if loc == 0 {
		t.Fatal("missing map")
	}

	stale := s.store.CreateTeam(friend.PlayerID, loc)
	friend.TeamID = stale.ID

	real := s.store.CreateTeam(busy.PlayerID, loc)
	busy.TeamID = real.ID
	s.dispatch(busy, protocol.CTeamInviteRequest, longPayload(buddy.PlayerID))
	s.dispatch(buddy, protocol.CTeamInvitationReplyRequest, payloadReply(real.ID, true))
	if len(real.Members) != 2 {
		t.Fatalf("second team not formed: %d", len(real.Members))
	}

	s.dispatch(c, protocol.CTeamCreate, intPayload(loc))
	team, _ := s.store.GetTeam(c.TeamID)

	s.dispatch(c, protocol.CTeamInviteRequest, longPayload(busy.PlayerID))
	if _, pending := team.Pending[busy.PlayerID]; pending {
		t.Fatal("player of a real team got invited")
	}

	s.dispatch(c, protocol.CTeamInviteRequest, longPayload(friend.PlayerID))
	if _, pending := team.Pending[friend.PlayerID]; !pending {
		t.Fatal("alone-in-old-team player was not invited")
	}
	s.dispatch(friend, protocol.CTeamInvitationReplyRequest, payloadReply(team.ID, true))
	if _, member := team.Members[friend.PlayerID]; !member || friend.TeamID != team.ID {
		t.Fatal("invite accept did not move player into the team")
	}
	if _, ok := s.store.GetTeam(stale.ID); ok {
		t.Fatal("old solo team was not dissolved")
	}
}

// Кто вышел из боя раньше конца (умер в выживании и нажал «Выйти»), раньше
// навсегда оставался «в бою»: команда считалась занятой, и на любое
// приглашение сервер отвечал «команда заполнена».
func TestLeavingBattleEarlyFreesTheTeam(t *testing.T) {
	s, c, _ := metaServer(t)
	mate := addPeer(s, "Mate")
	guest := addPeer(s, "Guest")
	loc := testLocation(s, "bounty")
	if loc == 0 {
		t.Fatal("missing map")
	}
	s.dispatch(c, protocol.CTeamCreate, intPayload(loc))
	team, _ := s.store.GetTeam(c.TeamID)
	s.dispatch(c, protocol.CTeamInviteRequest, longPayload(mate.PlayerID))
	s.dispatch(mate, protocol.CTeamInvitationReplyRequest, payloadReply(team.ID, true))
	if len(team.Members) != 2 {
		t.Fatalf("team not formed: %d", len(team.Members))
	}

	for _, member := range team.Members {
		member.State = 5
	}
	c.BattleID, mate.BattleID = "b1", "b1"
	s.dispatch(c, protocol.CExitBattle, emptyPayload())
	if c.BattleID != "" || team.Members[c.PlayerID].State != 1 {
		t.Fatalf("leaver still battling: battle=%q state=%d", c.BattleID, team.Members[c.PlayerID].State)
	}

	// Напарник вышел, когда бой уже кончился без него: статус снимает «не готов».
	mate.BattleID = ""
	s.dispatch(mate, protocol.CTeamMemberStateChange, intPayload(1))
	if team.Members[mate.PlayerID].State != 1 {
		t.Fatalf("mate stuck in battle state %d", team.Members[mate.PlayerID].State)
	}

	s.dispatch(c, protocol.CTeamInviteRequest, longPayload(guest.PlayerID))
	if _, pending := team.Pending[guest.PlayerID]; !pending {
		t.Fatal("invite refused after the battle")
	}
}

// Одиночная команда не прячет игрока из списка «позвать».
func TestSoloTeamPlayerStaysInInviteList(t *testing.T) {
	s, c, _ := metaServer(t)
	loner := addPeer(s, "Loner")
	loc := testLocation(s, "bounty")
	lonely := s.store.CreateTeam(loner.PlayerID, loc)
	loner.TeamID = lonely.ID
	if !s.freeForTeam(loner.PlayerID) {
		t.Fatal("solo team should not block invites")
	}
	for len(c.SendCh) > 0 {
		<-c.SendCh
	}
	s.dispatch(c, protocol.CAvailableTeamsRequest, emptyPayload())
	id := protocol.NewWriter()
	id.WriteLong(loner.PlayerID)
	found := false
	for len(c.SendCh) > 0 {
		if bytes.Contains(<-c.SendCh, id.Bytes()) {
			found = true
		}
	}
	if !found {
		t.Fatal("player alone in an old team vanished from the invite list")
	}
}

// «Играть с ботами» в поиске: матч собирается сразу, не дожидаясь минуты.
func TestBotsNowStartsTheMatchWithoutWaiting(t *testing.T) {
	s, c, _ := metaServer(t)
	loc := testLocation(s, "bounty")
	s.mm = testMatchmaker(s)
	s.cfg.Testing.EnableBots = true
	s.cfg.Matchmaking.BotFillSeconds = 60
	s.battle = &captureAllocator{}
	if !s.runOperation(func() bool { return s.mm.Enqueue(c.PlayerID, loc) }) {
		t.Fatal("queue rejected")
	}
	s.runOperation(func() bool { s.mm.tryAllocate(c.PlayerID); return true })
	if s.battle.(*captureAllocator).request != nil {
		t.Fatal("match started before the bot delay")
	}
	s.dispatch(c, protocol.CMatchmakingBotsNow, emptyPayload())
	if s.battle.(*captureAllocator).request == nil {
		t.Fatal("bots-now did not start the match")
	}
}

func startBattlePayload(loc int32) []byte {
	w := protocol.NewWriter()
	w.WriteInt(loc)
	w.WriteBool(false)
	w.WriteBool(false)
	return w.Bytes()
}

// «Играть» в команде: каждый становится готовым, и когда готовы все, команда
// встаёт в поиск. Раньше клиент не слал «готов», и поиск команды всегда
// отменялся.
func TestTeamPlayStartsWhenEveryoneIsReady(t *testing.T) {
	s, c, _ := metaServer(t)
	friend := addPeer(s, "Friend")
	loc := testLocation(s, "bounty")
	s.mm = testMatchmaker(s)
	s.dispatch(c, protocol.CTeamCreate, intPayload(loc))
	team, _ := s.store.GetTeam(c.TeamID)
	s.dispatch(c, protocol.CTeamInviteRequest, longPayload(friend.PlayerID))
	s.dispatch(friend, protocol.CTeamInvitationReplyRequest, payloadReply(team.ID, true))

	s.dispatch(friend, protocol.CStartBattle, startBattlePayload(loc))
	if team.Members[friend.PlayerID].State != 3 || s.mm.Queued(friend.PlayerID) {
		t.Fatalf("friend not ready: state %d", team.Members[friend.PlayerID].State)
	}
	// Передумал и снова нажал.
	s.dispatch(friend, protocol.CMatchmakingCancelRequest, emptyPayload())
	if team.Members[friend.PlayerID].State != 1 {
		t.Fatal("cancel did not clear readiness")
	}
	s.dispatch(friend, protocol.CStartBattle, startBattlePayload(loc))

	s.dispatch(c, protocol.CStartBattle, startBattlePayload(loc))
	if !s.mm.Queued(c.PlayerID) || !s.mm.Queued(friend.PlayerID) {
		t.Fatal("ready team was not queued")
	}
	for id, m := range team.Members {
		if m.State != 4 {
			t.Fatalf("member %d state %d, want matchmaking", id, m.State)
		}
	}
}
