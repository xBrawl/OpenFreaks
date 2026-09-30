package store

import (
	"fmt"
	"testing"
)

func TestBattleHistorySurvivesOtherPlayersMatches(t *testing.T) {
	s := New("")
	s.AddBattle(BattleRecord{BattleID: "quiet-player", Participants: []int64{1}})
	for i := 0; i < 1024; i++ {
		s.AddBattle(BattleRecord{BattleID: fmt.Sprint(i), Participants: []int64{2}})
	}
	own := s.BattlesFor(1, 20)
	if len(own) != 1 || own[0].BattleID != "quiet-player" {
		t.Fatal("other players evicted own history")
	}
	busy := s.BattlesFor(2, 20)
	if len(busy) != 20 || busy[0].BattleID != "1023" || busy[19].BattleID != "1004" {
		t.Fatal("latest twenty not retained in order")
	}
	if len(s.battles) > 320 {
		t.Fatalf("history was not pruned: %d", len(s.battles))
	}
}

func TestBattleHistoryPruningRollsBack(t *testing.T) {
	s := New("")
	for i := 0; i < 319; i++ {
		s.AddBattle(BattleRecord{BattleID: fmt.Sprint(i), Participants: []int64{1}})
	}
	s.Begin()
	s.AddBattle(BattleRecord{BattleID: "rolled-back", Participants: []int64{2}})
	s.Rollback()
	if len(s.BattlesFor(2, 20)) != 0 || s.BattlesFor(1, 1)[0].BattleID != "318" {
		t.Fatal("pruning broke transaction rollback")
	}
}
