package mainserver

import (
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/sim"
	"github.com/freaks/openfreaks/internal/store"
)

func TestMegaBossRotationAndQueueCapacity(t *testing.T) {
	s, _, _ := metaServer(t)
	loc := s.catData.LocByName["mega_boss_karity"]
	if loc == nil || s.teamCapacity(int32(loc.Id)) != 6 {
		t.Fatal("mega boss location must allow six teammates")
	}
	if boss := megaBossForDay(s.catData, time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)); boss == nil || boss.Name != "karity_boss_character" {
		t.Fatal("daily boss is not Karity")
	}
	entries := make([]*queueEntry, 7)
	for i := range entries {
		entries[i] = &queueEntry{playerID: int64(i + 1), queuedAt: time.Unix(int64(i), 0)}
	}
	group := planQueue(entries, 1, 6, 6)
	if len(group) != 6 {
		t.Fatalf("planned %d humans, want six", len(group))
	}
	for _, e := range group {
		if e.assignedTeam != 0 {
			t.Fatal("human placed on boss team")
		}
	}
}

func TestMegaBossWaitsFullMinuteAndCancelsUndersizedQueue(t *testing.T) {
	s, _, a := metaServer(t)
	loc := s.catData.LocByName["mega_boss_karity"]
	second := s.store.CreateAccount("QueueSecond", []byte("queue-second"))
	m := &Matchmaking{s: s, queue: map[int64]*queueEntry{
		a.PlayerID:      {playerID: a.PlayerID, location: int32(loc.Id), queuedAt: time.Now().Add(-59 * time.Second)},
		second.PlayerID: {playerID: second.PlayerID, location: int32(loc.Id), queuedAt: time.Now().Add(-59 * time.Second)},
	}}
	m.tryAllocate(a.PlayerID)
	if len(m.queue) != 2 {
		t.Fatal("mega boss queue ended before 60 seconds")
	}
	for _, e := range m.queue {
		e.queuedAt = time.Now().Add(-61 * time.Second)
	}
	m.tryAllocate(a.PlayerID)
	if len(m.queue) != 0 {
		t.Fatal("undersized mega boss queue was not cancelled after one minute")
	}
}

func TestMegaBossRewardExactlyOnceAndNoTrophyChange(t *testing.T) {
	s, _, first := metaServer(t)
	mode := s.catData.ModeByName["mega_boss"]
	loc := s.catData.LocByName["mega_boss_karity"]
	boss := s.catData.CharByName["karity_boss_character"]
	if mode == nil || loc == nil || boss == nil {
		t.Fatal("mega boss content missing")
	}
	accounts := []*store.Account{first, s.store.CreateAccount("Second", []byte("second")), s.store.CreateAccount("Third", []byte("third"))}
	a := &store.Allocation{LocationID: int32(loc.Id), ModeID: int32(mode.Id), StartedAt: time.Now().UnixMilli()}
	r := &battleserver.Result{BattleID: "mega-boss-test", ModeID: int32(mode.Id), FinalTick: 1800, WinnerTeam: 0}
	for i, account := range accounts {
		a.Players = append(a.Players, store.BattleParticipant{PlayerID: account.PlayerID, Index: i, Team: 0, CharacterModelID: first.SelectedChar, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1})
		r.Players = append(r.Players, &sim.Player{PlayerID: account.PlayerID, Index: i, Team: 0, ModelID: first.SelectedChar, HeroType: "hero", Alive: true})
	}
	a.Players = append(a.Players, store.BattleParticipant{PlayerID: -1003, Index: 3, Team: 1, IsBot: true, CharacterModelID: int32(boss.Id)})
	r.Players = append(r.Players, &sim.Player{PlayerID: -1003, Index: 3, Team: 1, IsBot: true, ModelID: int32(boss.Id)})
	s.store.Meta.Allocations[r.BattleID] = a
	m := &Matchmaking{s: s, rosters: map[string]map[int]int64{}, modes: map[string]int32{}, locations: map[string]int32{}, pending: map[string]time.Time{}}
	before := make([]store.Wallet, len(accounts))
	for i, account := range accounts {
		before[i] = account.Wallet
	}
	if !m.settleBattleResult(r) || !m.settleBattleResult(r) {
		t.Fatal("result settlement failed")
	}
	for i, account := range accounts {
		if account.Wallet.Karit-before[i].Karit != 500 || account.Wallet.UpgradeTokens-before[i].UpgradeTokens != 20 || account.Trophies != 0 {
			t.Fatalf("player %d reward/trophies incorrect: wallet=%+v trophies=%d", i, account.Wallet, account.Trophies)
		}
	}
}

func TestMegaBossSoloBotFillUsesTwoAlliesAndOneBoss(t *testing.T) {
	for _, immediate := range []bool{true, false} {
		s, _, account := metaServer(t)
		s.cfg.Testing.EnableBots = true
		backend := &captureAllocator{}
		s.battle = backend
		m := testMatchmaker(s)
		loc := s.catData.LocByName["mega_boss_karity"]
		entry := &queueEntry{playerID: account.PlayerID, location: int32(loc.Id), queuedAt: time.Now()}
		m.queue[account.PlayerID] = entry
		if immediate {
			m.BotsNow(account.PlayerID)
		} else {
			m.tryAllocate(account.PlayerID)
			if backend.request != nil {
				t.Fatal("automatic fill skipped the one-minute search")
			}
			entry.queuedAt = time.Now().Add(-61 * time.Second)
			m.tryAllocate(account.PlayerID)
		}
		if backend.request == nil || len(backend.request.Participants) != 4 {
			t.Fatalf("solo mega boss not allocated (immediate=%v)", immediate)
		}
		for i, p := range backend.request.Participants {
			if p.Index != i || p.IsBot != (i > 0) {
				t.Fatalf("invalid participant %d: %+v", i, p)
			}
			if i < 3 && (p.Team != 0 || !s.catData.CharByID[int(p.ModelID)].IsHero()) {
				t.Fatalf("ally %d placed on the boss team", i)
			}
			if i == 3 && (p.Team != 1 || s.catData.CharByID[int(p.ModelID)].Name != "karity_boss_character") {
				t.Fatal("missing boss opponent")
			}
		}
	}
}
