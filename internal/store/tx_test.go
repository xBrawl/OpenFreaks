package store

import (
	"strconv"
	"testing"
)

func newSeeded(t *testing.T) (*Store, *Account) {
	t.Helper()
	s := New(t.TempDir())
	s.ConfigureHeroes([]int32{1, 2, 3})
	a := s.CreateAccount("alpha", []byte("token-hash-alpha"))
	s.PutCredential(&Credential{LoginKey: "alpha", Nickname: "alpha", PlayerID: a.PlayerID,
		Salt: []byte("salt"), Hash: []byte("hash"), Iterations: 10})
	return s, a
}

func TestTxRollbackRestoresWalletAndMeta(t *testing.T) {
	s, a := newSeeded(t)
	pid := a.PlayerID
	a.Wallet.Karit = 500
	s.Meta.Player(pid).Streak = 3

	s.Begin()
	live, _ := s.GetAccount(pid)
	live.Wallet.Karit = 999
	live.Characters[1].HPLevel = 9
	live.OwnedSkins[77] = true
	s.Meta.Player(pid).Streak = 42
	s.Meta.Player(pid).Completed["bought"] = 1
	s.Rollback()

	got, _ := s.GetAccount(pid)
	if got.Wallet.Karit != 500 {
		t.Fatalf("wallet not rolled back: %d", got.Wallet.Karit)
	}
	if got.Characters[1].HPLevel != 1 {
		t.Fatalf("character not rolled back: %d", got.Characters[1].HPLevel)
	}
	if got.OwnedSkins[77] {
		t.Fatal("skin not rolled back")
	}
	if s.Meta.Player(pid).Streak != 3 {
		t.Fatalf("meta not rolled back: %d", s.Meta.Player(pid).Streak)
	}
	if _, ok := s.Meta.Player(pid).Completed["bought"]; ok {
		t.Fatal("meta map entry not rolled back")
	}
}

func TestTxRollbackKeepsPointerIdentity(t *testing.T) {
	s, a := newSeeded(t)
	a.Wallet.Sparks = 10
	held, _ := s.GetAccount(a.PlayerID)

	s.Begin()
	live, _ := s.GetAccount(a.PlayerID)
	live.Wallet.Sparks = 777
	s.Rollback()

	if held.Wallet.Sparks != 10 {
		t.Fatalf("held pointer sees %d, want 10", held.Wallet.Sparks)
	}
	current, _ := s.GetAccount(a.PlayerID)
	if current != held {
		t.Fatal("rollback replaced the account pointer")
	}
}

func TestTxRollbackDropsCreatedAccount(t *testing.T) {
	s, _ := newSeeded(t)
	s.Begin()
	fresh := s.CreateAccount("beta", []byte("t"))
	pid := fresh.PlayerID
	s.Meta.Player(pid).Streak = 5
	s.Rollback()

	if _, ok := s.PeekAccount(pid); ok {
		t.Fatal("account created inside the rolled back transaction survived")
	}
	if _, ok := s.Meta.Players[pid]; ok {
		t.Fatal("meta created inside the rolled back transaction survived")
	}
}

func TestTxRollbackRestoresFriendsAllianceTeamsBattles(t *testing.T) {
	s, a := newSeeded(t)
	b := s.CreateAccount("bravo", []byte("t"))
	s.AddFriendReq(a.PlayerID, b.PlayerID)
	al := s.CreateAlliance("clan", "d", 0, a.PlayerID)
	team := s.CreateTeam(a.PlayerID, 7)
	s.AddBattle(BattleRecord{BattleID: "before"})

	s.Begin()
	s.RemoveFriend(a.PlayerID, b.PlayerID)
	liveAl, _ := s.GetAlliance(al.ID)
	liveAl.Name = "renamed"
	liveAl.Members[b.PlayerID] = 1
	liveTeam, _ := s.GetTeam(team.ID)
	liveTeam.LocationID = 99
	s.AddBattle(BattleRecord{BattleID: "during"})
	s.Rollback()

	if s.FriendStatus(a.PlayerID, b.PlayerID) == 0 {
		t.Fatal("friend link not restored")
	}
	restored, _ := s.GetAlliance(al.ID)
	if restored.Name != "clan" {
		t.Fatalf("alliance name not restored: %s", restored.Name)
	}
	if _, ok := restored.Members[b.PlayerID]; ok {
		t.Fatal("alliance member not rolled back")
	}
	restoredTeam, _ := s.GetTeam(team.ID)
	if restoredTeam.LocationID != 7 {
		t.Fatalf("team not restored: %d", restoredTeam.LocationID)
	}
	if got := len(s.BattlesFor(0, 10)); got != 0 {
		t.Fatalf("unexpected battles for unknown player: %d", got)
	}
	ids := map[string]bool{}
	for _, rec := range s.battles {
		ids[rec.BattleID] = true
	}
	if ids["during"] || !ids["before"] {
		t.Fatalf("battle slice not restored: %v", ids)
	}
}

func TestDirtySetIgnoresPureReads(t *testing.T) {
	s, a := newSeeded(t)
	s.Meta.Player(a.PlayerID).Streak = 2
	al := s.CreateAlliance("clan", "d", 0, a.PlayerID)
	s.AddBattle(BattleRecord{BattleID: "b1"})

	s.Begin()
	_, _ = s.GetAccount(a.PlayerID)
	_, _ = s.GetAlliance(al.ID)
	_ = s.Meta.Player(a.PlayerID)
	_, _ = s.FindByNickname("alpha")
	_ = s.FriendList(a.PlayerID)
	_ = s.BattlesFor(a.PlayerID, 5)
	d := s.DirtySet()
	s.End()

	if !d.Empty() {
		t.Fatalf("pure reads marked dirty: %+v", d)
	}
}

func TestDirtySetTracksEachKind(t *testing.T) {
	s, a := newSeeded(t)
	other := s.CreateAccount("bravo", []byte("t"))
	al := s.CreateAlliance("clan", "d", 0, a.PlayerID)

	cases := []struct {
		name  string
		apply func()
		check func(*Dirty) bool
	}{
		{"wallet", func() { live, _ := s.GetAccount(a.PlayerID); live.Wallet.Karit++ },
			func(d *Dirty) bool { return d.Accounts[a.PlayerID] }},
		{"meta", func() { s.Meta.Player(a.PlayerID).Streak++ },
			func(d *Dirty) bool { return d.Accounts[a.PlayerID] }},
		{"friends", func() { s.AddFriendReq(a.PlayerID, other.PlayerID) },
			func(d *Dirty) bool { return d.Friends[a.PlayerID] && d.Friends[other.PlayerID] }},
		{"alliance", func() { live, _ := s.GetAlliance(al.ID); live.Description = "x" },
			func(d *Dirty) bool { return d.Alliances[al.ID] }},
		{"battle", func() { s.AddBattle(BattleRecord{BattleID: "fresh"}) },
			func(d *Dirty) bool { return d.Battles["fresh"] }},
		{"bond", func() { s.Meta.Bonds["1:2"] = &Bond{Low: 1, High: 2} },
			func(d *Dirty) bool { return d.Bonds["1:2"] }},
		{"receipt", func() { s.Meta.Receipts["r1"] = 5 },
			func(d *Dirty) bool { return d.Receipts["r1"] }},
		{"reactor", func() { s.Meta.Reactor(al.ID, 1).Points = 4 },
			func(d *Dirty) bool { return d.Reactors["1:1"] || len(d.Reactors) == 1 }},
		{"team", func() { s.CreateTeam(a.PlayerID, 3) },
			func(d *Dirty) bool { return d.Core }},
		{"allocation", func() { s.Meta.Allocations["b"] = &Allocation{ArenaID: "a"} },
			func(d *Dirty) bool { return d.Core }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s.Begin()
			tc.apply()
			d := s.DirtySet()
			s.Rollback()
			if !tc.check(d) {
				t.Fatalf("%s change not reported: %+v", tc.name, d)
			}
		})
	}
}

func TestDirtySetOutsideTransactionIsFull(t *testing.T) {
	s, _ := newSeeded(t)
	if d := s.DirtySet(); !d.All {
		t.Fatal("expected a full dirty set outside a transaction")
	}
}

func TestTxLedgerTruncatedOnRollback(t *testing.T) {
	s, a := newSeeded(t)
	s.Meta.Ledger = append(s.Meta.Ledger, LedgerEntry{ID: "keep", PlayerID: a.PlayerID})

	s.Begin()
	s.Meta.Ledger = append(s.Meta.Ledger, LedgerEntry{ID: "drop", PlayerID: a.PlayerID})
	s.Rollback()

	if len(s.Meta.Ledger) != 1 || s.Meta.Ledger[0].ID != "keep" {
		t.Fatalf("ledger not truncated: %+v", s.Meta.Ledger)
	}
}

func TestTxPreimageTakenOnce(t *testing.T) {
	s, a := newSeeded(t)
	a.Wallet.Karit = 100

	s.Begin()
	first, _ := s.GetAccount(a.PlayerID)
	first.Wallet.Karit = 200
	second, _ := s.GetAccount(a.PlayerID)
	second.Wallet.Karit = 300
	s.Rollback()

	got, _ := s.GetAccount(a.PlayerID)
	if got.Wallet.Karit != 100 {
		t.Fatalf("preimage overwritten: %d", got.Wallet.Karit)
	}
}

func BenchmarkTxCostIsIndependentOfSize(b *testing.B) {
	for _, n := range []int{200, 5000} {
		b.Run(fmtInt(n), func(b *testing.B) {
			s := New(b.TempDir())
			s.ConfigureHeroes([]int32{1, 2, 3})
			for i := 0; i < n; i++ {
				s.CreateAccount(fmtInt(i), make([]byte, 32))
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s.Begin()
				a, _ := s.GetAccount(1001)
				a.Wallet.Karit++
				_ = s.LiveDump(s.DirtySet())
				s.End()
			}
		})
	}
}

func fmtInt(v int) string { return strconv.Itoa(v) }
