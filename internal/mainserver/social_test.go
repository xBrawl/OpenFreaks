package mainserver

import (
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/protocol"
	"testing"
	"time"
)

func addPeer(s *Server, name string) *Conn {
	a := s.store.CreateAccount(name, []byte("token"))
	c := &Conn{ID: a.PlayerID, Server: s, PlayerID: a.PlayerID, SendCh: make(chan []byte, 4096)}
	c.Authed.Store(true)
	s.conns[c.ID] = c
	s.byPlayer[c.PlayerID] = c
	return c
}
func payloadReply(id int64, accept bool) []byte {
	w := protocol.NewWriter()
	w.WriteLong(id)
	w.WriteBool(accept)
	return w.Bytes()
}
func testLocation(s *Server, name string) int32 {
	for _, loc := range s.catData.Locations {
		if !loc.Hidden && loc.Mode == name {
			return int32(loc.Id)
		}
	}
	return 0
}

type captureAllocator struct{ request *battleserver.AllocateRequest }

func (a *captureAllocator) Allocate(req *battleserver.AllocateRequest) (map[int]int64, string, int, string) {
	a.request = req
	sessions := map[int]int64{}
	for _, p := range req.Participants {
		sessions[p.Index] = int64(p.Index + 100)
	}
	return sessions, "127.0.0.1", 29610, ""
}
func testMatchmaker(s *Server) *Matchmaking {
	return &Matchmaking{s: s, queue: map[int64]*queueEntry{}, rosters: map[string]map[int]int64{}, modes: map[string]int32{}, locations: map[string]int32{}, applied: map[string]bool{}, pending: map[string]time.Time{}}
}

func TestInvitationCannotBeForgedAndPartyIsKeptTogether(t *testing.T) {
	s, c, _ := metaServer(t)
	friend := addPeer(s, "Friend")
	stranger := addPeer(s, "Stranger")
	loc := testLocation(s, "bounty")
	if loc == 0 {
		t.Fatal("missing map")
	}
	s.mm = testMatchmaker(s)
	s.cfg.Testing.EnableBots = true
	s.cfg.Matchmaking.BotFillSeconds = 1
	s.battle = &captureAllocator{}
	s.dispatch(c, protocol.CTeamCreate, intPayload(loc))
	team, _ := s.store.GetTeam(c.TeamID)
	s.dispatch(stranger, protocol.CTeamInvitationReplyRequest, payloadReply(team.ID, true))
	if stranger.TeamID != 0 || len(team.Members) != 1 {
		t.Fatal("uninvited player joined")
	}
	s.dispatch(c, protocol.CTeamInviteRequest, longPayload(friend.PlayerID))
	s.dispatch(friend, protocol.CTeamInvitationReplyRequest, payloadReply(team.ID, true))
	s.dispatch(friend, protocol.CTeamMemberStateChange, intPayload(3))
	if !s.runOperation(func() bool { return s.mm.Enqueue(c.PlayerID, loc) && s.mm.Enqueue(stranger.PlayerID, loc) }) {
		t.Fatal("queue rejected")
	}
	for _, e := range s.mm.queue {
		e.queuedAt = time.Now().Add(-3 * time.Second)
	}
	s.runOperation(func() bool { s.mm.tryAllocate(c.PlayerID); return true })
	req := s.battle.(*captureAllocator).request
	if req == nil {
		t.Fatal("no match allocated")
	}
	teams := map[int64]int{}
	for _, p := range req.Participants {
		teams[p.PlayerID] = p.Team
	}
	if teams[c.PlayerID] != teams[friend.PlayerID] || teams[c.PlayerID] == teams[stranger.PlayerID] {
		t.Fatalf("party split or opponents missing: %+v", teams)
	}
	if s.store.Meta.Allocations[req.BattleID] == nil {
		t.Fatal("match roster not durable")
	}
}
func TestClanMembershipAndRoleBoundaries(t *testing.T) {
	s, c, a := metaServer(t)
	member := addPeer(s, "Member")
	outsider := addPeer(s, "Outsider")
	al := s.store.CreateAlliance("One", "", 0, a.PlayerID)
	second := s.store.CreateAlliance("Two", "", 0, outsider.PlayerID)
	al.Members[member.PlayerID] = 1
	acc, _ := s.store.GetAccount(member.PlayerID)
	acc.AllianceID = al.ID
	s.dispatch(member, protocol.CAllianceKickRequest, longPayload(a.PlayerID))
	if al.Members[a.PlayerID] != 3 {
		t.Fatal("rookie kicked leader")
	}
	s.dispatch(c, protocol.CAllianceKickRequest, longPayload(outsider.PlayerID))
	outside, _ := s.store.GetAccount(outsider.PlayerID)
	if outside.AllianceID != second.ID {
		t.Fatal("cross-clan kick")
	}
	w := protocol.NewWriter()
	w.WriteLong(member.PlayerID)
	w.WriteInt(3)
	s.dispatch(member, protocol.CAllianceRoleChangeRequest, w.Bytes())
	if al.Members[member.PlayerID] != 1 {
		t.Fatal("self promotion")
	}
	s.dispatch(c, protocol.CAllianceRoleChangeRequest, w.Bytes())
	if al.Members[c.PlayerID] != 2 || al.Members[member.PlayerID] != 3 {
		t.Fatal("leader transfer failed")
	}
	s.dispatch(member, protocol.CAllianceLeaveRequest, []byte{0})
	if al.Members[c.PlayerID] != 3 {
		t.Fatal("clan left without leader")
	}
}

func TestQueueNeverSplitsPartyAtCapacity(t *testing.T) {
	entries := []*queueEntry{}
	for i := 0; i < 8; i++ {
		party := int64(0)
		if i >= 5 {
			party = 100
		}
		entries = append(entries, &queueEntry{playerID: int64(i + 1), partyID: party, queuedAt: time.Unix(int64(i), 0)})
	}
	group := planQueue(entries, 2, 3, 6)
	members := 0
	for _, p := range group {
		if p.partyID == 100 {
			members++
		}
	}
	if members != 0 && members != 3 {
		t.Fatal("partially allocated a party")
	}
}
