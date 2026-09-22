package mainserver

import (
	"github.com/freaks/openfreaks/internal/store"
	"testing"
)

func TestBattleEmotesAreScopedAndRateLimited(t *testing.T) {
	s, c, a := metaServer(t)
	c.BattleID = "room"
	other := &Conn{ID: 2, PlayerID: 222, BattleID: "other", Server: s, SendCh: make(chan []byte, 16)}
	s.byPlayer[222] = other
	s.store.Meta.Allocations["room"] = &store.Allocation{Players: []store.BattleParticipant{{PlayerID: a.PlayerID, Index: 0}, {PlayerID: 222, Index: 1}}}
	s.onBattleEmote(c, intPayload(1))
	if len(c.SendCh) != 1 || len(other.SendCh) != 0 {
		t.Fatal("wrong room broadcast")
	}
	s.onBattleEmote(c, intPayload(2))
	if len(c.SendCh) != 1 {
		t.Fatal("emote spam accepted")
	}
	delete(s.emoteLast, a.PlayerID)
	s.onBattleEmote(c, intPayload(6))
	if len(c.SendCh) != 1 {
		t.Fatal("invalid emoji broadcast")
	}
	other.BattleID = "room"
	s.onBattleEmote(c, intPayload(5))
	if len(other.SendCh) != 1 {
		t.Fatal("opponent did not receive")
	}
	delete(s.emoteLast, a.PlayerID)
	s.store.Meta.Allocations["room"].Players[0].Abandoned = true
	s.onBattleEmote(c, intPayload(0))
	if len(c.SendCh) != 2 {
		t.Fatal("departed sender accepted")
	}
}
