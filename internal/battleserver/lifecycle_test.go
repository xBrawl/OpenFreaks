package battleserver

import (
	"github.com/freaks/openfreaks/internal/sim"
	"testing"
)

func TestReconnectUsesExistingRoomAndLeaveCannotRejoin(t *testing.T) {
	p := &sim.Player{Index: 0, PlayerID: 42, Team: 0, HP: 100, MaxHP: 100, Alive: true, HeroType: "hero"}
	w := &sim.World{Players: []*sim.Player{p}, DurationTicks: 10000, TeamCount: 2, TeamPoints: make([]int, 2)}
	m := NewMatch("battle", w, []*Participant{{Index: 0, PlayerID: 42, Team: 0}}, map[int]int64{0: 123}, nil)
	s := NewServer(Config{}, nil, nil)
	s.matches[m.ID] = m
	s.grants[123] = &Grant{SessionID: 123, Index: 0}
	grant, err := s.Resume(m.ID, 42)
	if err != nil || grant.SessionID != 123 {
		t.Fatal("resume allocated a new room")
	}
	if _, err := s.Resume(m.ID, 43); err == nil {
		t.Fatal("another player resumed session")
	}
	for i := 0; i < 41; i++ {
		m.doTick()
	}
	if !p.BotControlled || p.IsBot || p.Abandoned {
		t.Fatal("temporary disconnect changed participant identity")
	}
	if !s.Leave(m.ID, 42) {
		t.Fatal("leave failed")
	}
	m.doTick()
	if !p.Abandoned || p.Alive || p.BotControlled {
		t.Fatal("explicit leave continued playing")
	}
	if _, err := s.Resume(m.ID, 42); err == nil {
		t.Fatal("departed participant resumed")
	}
	if s.grants[123] != nil {
		t.Fatal("departed session still authorized")
	}
}
