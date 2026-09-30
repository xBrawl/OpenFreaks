package battleserver

import (
	"testing"

	"github.com/freaks/openfreaks/internal/content"
)

func TestMegaBossRejectsBotFilledAndUndersizedRooms(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	loc := cat.LocByName["mega_boss_karity"]
	mode := cat.ModeByName["mega_boss"]
	if loc == nil || mode == nil {
		t.Fatal("mega boss content missing")
	}
	s := NewServerWithCatalog(Config{}, nil, nil, cat)
	hero := int32(cat.Heroes()[0].Id)
	boss := int32(cat.CharByName["karity_boss_character"].Id)
	parts := []*Participant{
		{PlayerID: 1, Index: 0, Team: 0, ModelID: hero},
		{PlayerID: 2, Index: 1, Team: 0, ModelID: hero},
		{PlayerID: -1002, Index: 2, Team: 1, IsBot: true, ModelID: boss},
	}
	request := &AllocateRequest{BattleID: "undersized", LocationID: loc.Id, ModeID: int32(mode.Id), Participants: parts}
	if _, _, _, got := s.Allocate(request); got != "mega_boss_requires_three_to_six_humans" {
		t.Fatalf("two humans accepted: %s", got)
	}
	parts[2] = &Participant{PlayerID: 3, Index: 2, Team: 0, ModelID: hero}
	parts = append(parts, &Participant{PlayerID: -1003, Index: 3, Team: 0, IsBot: true, ModelID: hero})
	request.BattleID = "bot-fill"
	request.Participants = parts
	if _, _, _, got := s.Allocate(request); got != "invalid_mega_boss_roster" {
		t.Fatalf("player bot accepted: %s", got)
	}
}

func TestMegaBossAllowsSoloWithTwoBotsWhenEnabled(t *testing.T) {
	cat, err := content.Parse("../../deploy/content-real")
	if err != nil {
		t.Fatal(err)
	}
	loc := cat.LocByName["mega_boss_karity"]
	hero := int32(cat.Heroes()[0].Id)
	boss := int32(cat.CharByName[practiceBossModel].Id)
	request := &AllocateRequest{BattleID: "solo-mega-boss", LocationID: loc.Id, Participants: []*Participant{
		{PlayerID: 1, Index: 0, Team: 0, ModelID: hero, DashID: -1, UltID: -1, StarPowerID: -1},
		{PlayerID: -1001, Index: 1, Team: 0, IsBot: true, ModelID: hero, DashID: -1, UltID: -1, StarPowerID: -1},
		{PlayerID: -1002, Index: 2, Team: 0, IsBot: true, ModelID: hero, DashID: -1, UltID: -1, StarPowerID: -1},
		{PlayerID: -1003, Index: 3, Team: 1, IsBot: true, ModelID: boss, DashID: -1, UltID: -1, StarPowerID: -1},
	}}
	s := NewServerWithCatalog(Config{AllowMegaBossBots: true}, nil, nil, cat)
	defer s.Close()
	if _, _, _, got := s.Allocate(request); got != "" {
		t.Fatalf("solo with bot allies rejected: %s", got)
	}
	allBots := *request
	allBots.BattleID = "all-bots"
	allBots.Participants = make([]*Participant, len(request.Participants))
	for i, p := range request.Participants {
		copy := *p
		allBots.Participants[i] = &copy
	}
	allBots.Participants[0].IsBot = true
	if _, _, _, got := s.Allocate(&allBots); got == "" {
		t.Fatal("accepted a mega boss room without any human")
	}
}
