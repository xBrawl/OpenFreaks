package mainserver

import (
	"encoding/binary"
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

// frames вынимает из очереди соединения все пакеты с данным id.
func frames(c *Conn, id int16) [][]byte {
	var out [][]byte
	for len(c.SendCh) > 0 {
		frame := <-c.SendCh
		if int16(binary.BigEndian.Uint16(frame[:2])) == id {
			out = append(out, frame[6:])
		}
	}
	return out
}

func createAlliancePayload(name string) []byte {
	w := protocol.NewWriter()
	w.WriteString(name)
	w.WriteString("")
	w.WriteInt(0)
	return w.Bytes()
}

type decodedMessage struct {
	kind, event int32
	player      int64
	text        string
	avatar      int32
	hasAvatar   bool
}

func decodeAllianceMessage(t *testing.T, r *protocol.Reader, social bool) decodedMessage {
	t.Helper()
	var m decodedMessage
	var err error
	must := func(e error) {
		if e != nil {
			t.Fatal(e)
		}
	}
	m.kind, err = r.ReadInt()
	must(err)
	m.player, err = r.ReadLong()
	must(err)
	_, _, err = r.ReadString()
	must(err)
	_, err = r.ReadInt()
	must(err)
	if m.kind == 0 {
		m.text, _, err = r.ReadString()
		must(err)
	} else {
		m.event, err = r.ReadInt()
		must(err)
		_, err = r.ReadLong()
		must(err)
		_, _, err = r.ReadString()
		must(err)
		_, err = r.ReadInt()
		must(err)
	}
	_, err = r.ReadLong()
	must(err)
	if social {
		m.avatar, err = r.ReadInt()
		must(err)
		m.hasAvatar = true
	}
	return m
}

func TestChatEmojiTokensAreValidated(t *testing.T) {
	text, length := normalizeChatText("привет :e3: :e99: :e11:")
	if text != "привет :e3:  :e11:" {
		t.Fatalf("text=%q", text)
	}
	// «привет» + три пробела + две рожицы по одному знаку
	if length != 11 {
		t.Fatalf("length=%d", length)
	}
	if legacyChatText(text) != "привет *  *" {
		t.Fatalf("legacy=%q", legacyChatText(text))
	}
}

// Старый клиент получает сообщение чата байт в байт в прежнем формате,
// новый — с аватаркой в хвосте и с эмодзи как есть.
func TestChatKeepsLegacyFormatAndAddsAvatarForSocialClients(t *testing.T) {
	s, legacy, owner := metaServer(t)
	modern := addPeer(s, "Modern")
	modern.social.Store(true)
	if !s.dispatch(legacy, protocol.CAllianceCreateRequest, createAlliancePayload("Шайка")) {
		t.Fatal("create failed")
	}
	if !s.dispatch(modern, protocol.CAllianceJoinRequest, longPayload(owner.AllianceID)) {
		t.Fatal("join failed")
	}
	frames(legacy, 0)
	frames(modern, 0)
	if !s.dispatch(legacy, protocol.CSendAllianceMessage, stringPayload(":e2: го")) {
		t.Fatal("send failed")
	}

	old := frames(legacy, protocol.SAllianceMessage)
	fresh := frames(modern, protocol.SAllianceMessage)
	if len(old) != 1 || len(fresh) != 1 {
		t.Fatalf("legacy=%d modern=%d", len(old), len(fresh))
	}
	r := protocol.NewReader(old[0])
	m := decodeAllianceMessage(t, r, false)
	if !r.Ended() || m.text != "* го" {
		t.Fatalf("legacy frame: %+v ended=%v", m, r.Ended())
	}
	r = protocol.NewReader(fresh[0])
	m = decodeAllianceMessage(t, r, true)
	if !r.Ended() || m.text != ":e2: го" || m.avatar != owner.SelectedChar {
		t.Fatalf("modern frame: %+v ended=%v selected=%d", m, r.Ended(), owner.SelectedChar)
	}
}

func TestAllianceEventsLandInChatHistory(t *testing.T) {
	s, c, owner := metaServer(t)
	c.social.Store(true)
	peer := addPeer(s, "Peer")
	s.dispatch(c, protocol.CAllianceCreateRequest, createAlliancePayload("Шайка"))
	s.dispatch(peer, protocol.CAllianceJoinRequest, longPayload(owner.AllianceID))
	role := protocol.NewWriter()
	role.WriteLong(peer.PlayerID)
	role.WriteInt(2)
	if !s.dispatch(c, protocol.CAllianceRoleChangeRequest, role.Bytes()) {
		t.Fatal("promote failed")
	}
	if !s.dispatch(c, protocol.CAllianceKickRequest, longPayload(peer.PlayerID)) {
		t.Fatal("kick failed")
	}
	frames(c, 0)
	s.dispatch(c, protocol.CAllianceMessagesRequest, []byte{0})
	list := frames(c, protocol.SAllianceMessagesResponse)
	if len(list) != 1 {
		t.Fatal("no history")
	}
	r := protocol.NewReader(list[0])
	count, _ := r.ReadInt()
	var events []int32
	for i := int32(0); i < count; i++ {
		m := decodeAllianceMessage(t, r, true)
		if m.kind != 1 {
			t.Fatalf("unexpected chat message %+v", m)
		}
		events = append(events, m.event)
	}
	want := []int32{allianceEventCreated, allianceEventJoin, allianceEventPromote, allianceEventKick}
	if len(events) != len(want) || !r.Ended() {
		t.Fatalf("events=%v ended=%v", events, r.Ended())
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events=%v want %v", events, want)
		}
	}
}

func TestAvatarMustBeAnOwnedHero(t *testing.T) {
	s, c, a := metaServer(t)
	c.social.Store(true)
	var unowned int32
	for _, hero := range s.catData.Heroes() {
		if _, owned := a.Characters[int32(hero.Id)]; !owned {
			unowned = int32(hero.Id)
			break
		}
	}
	if unowned != 0 {
		s.dispatch(c, protocol.CSetAvatarRequest, intPayload(unowned))
		if a.AvatarID != 0 {
			t.Fatal("unowned hero accepted as avatar")
		}
	}
	var owned int32
	for id := range a.Characters {
		owned = id
	}
	if !s.dispatch(c, protocol.CSetAvatarRequest, intPayload(owned)) || a.AvatarID != owned || avatarOf(a) != owned {
		t.Fatalf("owned avatar rejected: %d", a.AvatarID)
	}
	replies := frames(c, protocol.SSetAvatarResponse)
	if len(replies) == 0 || replies[len(replies)-1][0] != 1 {
		t.Fatal("no successful reply")
	}
}

func TestLoginFeaturesAreOptional(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteLong(7)
	w.WriteString("token")
	w.WriteInt(1)
	legacy, err := protocol.DecodeLogin(w.Bytes())
	if err != nil || legacy.Features != 0 {
		t.Fatalf("legacy login: %v %+v", err, legacy)
	}
	w.WriteInt(protocol.FeatureSocialV2)
	modern, err := protocol.DecodeLogin(w.Bytes())
	if err != nil || modern.Features != protocol.FeatureSocialV2 {
		t.Fatalf("modern login: %v %+v", err, modern)
	}
	w.WriteInt(5)
	if _, err := protocol.DecodeLogin(w.Bytes()); err == nil {
		t.Fatal("extra bytes accepted")
	}
}

// Чат отряда доходит только до новых клиентов, а обновление команды у старых
// остаётся прежним по формату.
func TestTeamChatAndAvatarsInTeamUpdates(t *testing.T) {
	s, legacy, _ := metaServer(t)
	modern := addPeer(s, "Modern")
	modern.social.Store(true)
	loc := testLocation(s, "bounty")
	if loc == 0 {
		t.Fatal("missing map")
	}
	if !s.dispatch(modern, protocol.CTeamCreate, intPayload(loc)) {
		t.Fatal("team create failed")
	}
	team := s.store.TeamForPlayer(modern.PlayerID)
	s.dispatch(modern, protocol.CTeamInviteRequest, longPayload(legacy.PlayerID))
	s.dispatch(legacy, protocol.CTeamInvitationReplyRequest, payloadReply(team.ID, true))
	if len(team.Members) != 2 {
		t.Fatalf("members=%d", len(team.Members))
	}
	frames(legacy, 0)
	frames(modern, 0)
	s.dispatch(legacy, protocol.CSendTeamMessage, stringPayload("го :e1:"))
	if len(frames(legacy, protocol.STeamMessage)) != 0 {
		t.Fatal("legacy client got a team chat message")
	}
	got := frames(modern, protocol.STeamMessage)
	if len(got) != 1 {
		t.Fatal("modern client got no team chat message")
	}
	r := protocol.NewReader(got[0])
	pid, _ := r.ReadLong()
	_, _, _ = r.ReadString()
	_, _ = r.ReadInt()
	text, _, _ := r.ReadString()
	_, _ = r.ReadLong()
	if pid != legacy.PlayerID || text != "го :e1:" || !r.Ended() {
		t.Fatalf("pid=%d text=%q ended=%v", pid, text, r.Ended())
	}
	s.dispatch(modern, protocol.CTeamMessagesRequest, []byte{0})
	history := frames(modern, protocol.STeamMessagesResponse)
	if len(history) != 1 || history[0][3] != 1 {
		t.Fatal("history does not hold the message")
	}

	s.broadcastTeamUpdate(team.ID)
	old := frames(legacy, protocol.STeamUpdate)
	fresh := frames(modern, protocol.STeamUpdate)
	if len(old) != 1 || len(fresh) != 1 {
		t.Fatal("no team updates")
	}
	// у нового клиента по одной аватарке (4 байта) на каждого из двух участников
	if len(fresh[0])-len(old[0]) != 8 {
		t.Fatalf("legacy=%d modern=%d", len(old[0]), len(fresh[0]))
	}
}
