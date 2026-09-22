package mainserver

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

func TestReadOnlyHandlersDoNotChangeState(t *testing.T) {
	s, c, a := metaServer(t)
	var logged bytes.Buffer
	s.log = slog.New(slog.NewTextHandler(&logged, nil))

	friend := addPeer(s, "Friend")
	s.store.AddFriendReq(a.PlayerID, friend.PlayerID)
	s.store.AddFriendReq(friend.PlayerID, a.PlayerID)
	al := s.store.CreateAlliance("Readers", "desc", 0, a.PlayerID)
	al.Messages = append(al.Messages, store.AllianceMessage{PlayerID: a.PlayerID, Message: "hi", CreationTime: 1})
	s.store.CreateTeam(friend.PlayerID, 1)
	s.store.AddBattle(store.BattleRecord{BattleID: "b1", Participants: []int64{a.PlayerID},
		TrophyDeltas: map[int64]int32{a.PlayerID: 5}, Ranks: map[int64]int32{a.PlayerID: 1}})
	s.store.Meta.Bonds[store.BondKey(a.PlayerID, friend.PlayerID)] = &store.Bond{
		Low: min(a.PlayerID, friend.PlayerID), High: max(a.PlayerID, friend.PlayerID), Matches: 3}

	cases := []struct {
		name    string
		id      int16
		payload []byte
	}{
		{"keepalive", protocol.CKeepAliveRequest, nil},
		{"available-teams", protocol.CAvailableTeamsRequest, nil},
		{"alliance-search", protocol.CAllianceSearchRequest, stringPayload("Read")},
		{"alliance-data", protocol.CAllianceDataRequest, longPayload(al.ID)},
		{"alliance-messages", protocol.CAllianceMessagesRequest, nil},
		{"friends-list", protocol.CFriendsListRequest, nil},
		{"player-data-known", protocol.CPlayerDataRequest, longPayload(friend.PlayerID)},
		{"battle-logs", protocol.CBattleLogsRequest, nil},
		{"bond-state", protocol.CBondStateRequest, longPayload(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !readOnlyMessage(tc.id) {
				t.Fatalf("message %d is not listed as read-only", tc.id)
			}
			s.store.Begin()
			s.dispatchLocked(c, tc.id, tc.payload)
			dirty := s.store.DirtySet()
			s.store.Rollback()
			if !dirty.Empty() {
				t.Fatalf("read-only handler changed state: %s", describeDirty(dirty))
			}
		})
	}
	if strings.Contains(logged.String(), "read-only") {
		t.Fatalf("unexpected read-only violation logged: %s", logged.String())
	}
}

func TestPlayerDataDoesNotCreateMetaForTarget(t *testing.T) {
	s, c, _ := metaServer(t)
	stranger := s.store.CreateAccount("Stranger", []byte("token"))

	s.store.Begin()
	s.dispatchLocked(c, protocol.CPlayerDataRequest, longPayload(stranger.PlayerID))
	dirty := s.store.DirtySet()
	s.store.Rollback()

	if !dirty.Empty() {
		t.Fatalf("profile view changed state: %s", describeDirty(dirty))
	}
}

func stringPayload(v string) []byte {
	w := protocol.NewWriter()
	w.WriteString(v)
	return w.Bytes()
}

func TestIdleMatchmakingTickDoesNotCommit(t *testing.T) {
	s, _, _ := metaServer(t)
	s.mm = testMatchmaker(s)
	commits := 0
	s.store.SetCommitter(func(*store.Dump) error { commits++; return nil })

	s.mm.tick()
	if commits != 0 {
		t.Fatalf("idle tick wrote to storage %d times", commits)
	}
	if !s.mm.quiet() {
		t.Fatal("empty matchmaking is not reported as quiet")
	}

	s.store.Meta.Allocations["b1"] = &store.Allocation{StartedAt: 1}
	if s.mm.quiet() {
		t.Fatal("matchmaking with a live allocation must not be quiet")
	}
}
