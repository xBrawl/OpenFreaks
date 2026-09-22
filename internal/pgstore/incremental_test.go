package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("PGTEST_URL")
	if url == "" {
		t.Skip("PGTEST_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	schema := fmt.Sprintf("of_inc_%d", time.Now().UnixNano())
	pool, err := PoolWithSchema(ctx, url, schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
		pool.Close()
	})
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return ctx, pool
}

func seedStore(t *testing.T) (*store.Store, *store.Account, *store.Account) {
	t.Helper()
	st := store.New("")
	st.ConfigureHeroes([]int32{1, 2, 3})
	a := st.CreateAccount("alpha", []byte("0123456789abcdef0123456789abcdef"))
	st.PutCredential(&store.Credential{LoginKey: "ALPHA", Nickname: "alpha", PlayerID: a.PlayerID,
		Salt: []byte("0123456789abcdef"), Hash: make([]byte, 32), Iterations: 210000})
	b := st.CreateAccount("bravo", []byte("fedcba9876543210fedcba9876543210"))
	st.PutCredential(&store.Credential{LoginKey: "BRAVO", Nickname: "bravo", PlayerID: b.PlayerID,
		Salt: []byte("fedcba9876543210"), Hash: make([]byte, 32), Iterations: 210000})
	a.Wallet.Karit = 500
	b.Wallet.Karit = 700
	st.Meta.Player(a.PlayerID).Streak = 4
	st.Meta.Player(b.PlayerID).Streak = 9
	return st, a, b
}

func TestIncrementalWriteKeepsUntouchedData(t *testing.T) {
	ctx, pool := testPool(t)
	st, a, b := seedStore(t)
	if err := Persist(ctx, pool, st.DumpState()); err != nil {
		t.Fatal(err)
	}

	st.Begin()
	live, _ := st.GetAccount(a.PlayerID)
	live.Wallet.Karit = 12345
	st.Meta.Player(a.PlayerID).Streak = 77
	dirty := st.DirtySet()
	if len(dirty.Accounts) != 1 || !dirty.Accounts[a.PlayerID] {
		t.Fatalf("expected exactly one dirty account, got %+v", dirty.Accounts)
	}
	if err := Persist(ctx, pool, st.LiveDump(dirty)); err != nil {
		t.Fatal(err)
	}
	st.End()

	got, err := Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if got.Accounts[a.PlayerID].Wallet.Karit != 12345 {
		t.Fatalf("touched account not written: %d", got.Accounts[a.PlayerID].Wallet.Karit)
	}
	if got.Meta.Players[a.PlayerID].Streak != 77 {
		t.Fatalf("touched meta not written: %d", got.Meta.Players[a.PlayerID].Streak)
	}
	if got.Accounts[b.PlayerID].Wallet.Karit != 700 {
		t.Fatalf("untouched account lost: %d", got.Accounts[b.PlayerID].Wallet.Karit)
	}
	if got.Meta.Players[b.PlayerID].Streak != 9 {
		t.Fatalf("untouched meta lost: %d", got.Meta.Players[b.PlayerID].Streak)
	}
	if _, ok := got.Creds["BRAVO"]; !ok {
		t.Fatal("untouched credential lost")
	}
}

func TestIncrementalAllianceMove(t *testing.T) {
	ctx, pool := testPool(t)
	st, a, b := seedStore(t)
	first := st.CreateAlliance("first", "d", 0, a.PlayerID)
	second := st.CreateAlliance("second", "d", 0, b.PlayerID)
	if err := Persist(ctx, pool, st.DumpState()); err != nil {
		t.Fatal(err)
	}

	st.Begin()
	st.LeaveAlliance(b.PlayerID)
	target, _ := st.GetAlliance(first.ID)
	target.Members[b.PlayerID] = 1
	moved, _ := st.GetAccount(b.PlayerID)
	moved.AllianceID = first.ID
	if err := Persist(ctx, pool, st.LiveDump(st.DirtySet())); err != nil {
		t.Fatal(err)
	}
	st.End()

	var allianceID int64
	if err := pool.QueryRow(ctx, `SELECT alliance_id FROM alliance_members WHERE player_id=$1`, b.PlayerID).Scan(&allianceID); err != nil {
		t.Fatal(err)
	}
	if allianceID != first.ID {
		t.Fatalf("member row points at %d, want %d", allianceID, first.ID)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alliance_members WHERE alliance_id=$1`, second.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("old clan still has %d members", rows)
	}
}

func TestIncrementalAllianceDelete(t *testing.T) {
	ctx, pool := testPool(t)
	st, a, _ := seedStore(t)
	al := st.CreateAlliance("doomed", "d", 0, a.PlayerID)
	if err := Persist(ctx, pool, st.DumpState()); err != nil {
		t.Fatal(err)
	}

	st.Begin()
	st.LeaveAlliance(a.PlayerID)
	st.DeleteAlliance(al.ID)
	if err := Persist(ctx, pool, st.LiveDump(st.DirtySet())); err != nil {
		t.Fatal(err)
	}
	st.End()

	got, err := Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Alliances[al.ID]; ok {
		t.Fatal("deleted alliance survived the checkpoint")
	}
	var shardRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM openfreaks_state_shards WHERE kind='alliance'`).Scan(&shardRows); err != nil {
		t.Fatal(err)
	}
	if shardRows != 0 {
		t.Fatalf("alliance shard not removed: %d rows", shardRows)
	}
}

func TestIncrementalFriendship(t *testing.T) {
	ctx, pool := testPool(t)
	st, a, b := seedStore(t)
	if err := Persist(ctx, pool, st.DumpState()); err != nil {
		t.Fatal(err)
	}

	st.Begin()
	st.AddFriendReq(a.PlayerID, b.PlayerID)
	st.AddFriendReq(b.PlayerID, a.PlayerID)
	if err := Persist(ctx, pool, st.LiveDump(st.DirtySet())); err != nil {
		t.Fatal(err)
	}
	st.End()

	var pairs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM friendships`).Scan(&pairs); err != nil {
		t.Fatal(err)
	}
	if pairs != 1 {
		t.Fatalf("expected one friendship row, got %d", pairs)
	}

	st.Begin()
	st.RemoveFriend(a.PlayerID, b.PlayerID)
	if err := Persist(ctx, pool, st.LiveDump(st.DirtySet())); err != nil {
		t.Fatal(err)
	}
	st.End()

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM friendships`).Scan(&pairs); err != nil {
		t.Fatal(err)
	}
	if pairs != 0 {
		t.Fatalf("friendship not removed, %d rows left", pairs)
	}
}

func TestLegacyCheckpointMigratesToShards(t *testing.T) {
	ctx, pool := testPool(t)
	st, a, _ := seedStore(t)
	st.CreateAlliance("legacy", "d", 0, a.PlayerID)
	st.Meta.Bonds["1:2"] = &store.Bond{Low: 1, High: 2, Matches: 5}
	st.Meta.Receipts["battle:x"] = 42
	st.AddBattle(store.BattleRecord{BattleID: "legacy-b", Participants: []int64{a.PlayerID}})

	payload, err := json.Marshal(st.DumpState())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO openfreaks_state(singleton,payload) VALUES(TRUE,$1)`, payload); err != nil {
		t.Fatal(err)
	}

	got, err := Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if got.Accounts[a.PlayerID].Wallet.Karit != 500 {
		t.Fatalf("legacy wallet lost: %d", got.Accounts[a.PlayerID].Wallet.Karit)
	}
	var legacyRows, shardRows int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM openfreaks_state`).Scan(&legacyRows); err != nil {
		t.Fatal(err)
	}
	if legacyRows != 0 {
		t.Fatal("legacy checkpoint row still present after migration")
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM openfreaks_state_shards`).Scan(&shardRows); err != nil {
		t.Fatal(err)
	}
	if shardRows == 0 {
		t.Fatal("no shards written during migration")
	}

	again, err := Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Accounts) != len(got.Accounts) {
		t.Fatalf("shard reload lost accounts: %d vs %d", len(again.Accounts), len(got.Accounts))
	}
	if len(again.Alliances) != 1 {
		t.Fatalf("shard reload lost alliances: %d", len(again.Alliances))
	}
	if again.Meta.Bonds["1:2"] == nil || again.Meta.Bonds["1:2"].Matches != 5 {
		t.Fatal("shard reload lost bonds")
	}
	if again.Meta.Receipts["battle:x"] != 42 {
		t.Fatal("shard reload lost receipts")
	}
	if len(again.Battles) != 1 || again.Battles[0].BattleID != "legacy-b" {
		t.Fatalf("shard reload lost battles: %+v", again.Battles)
	}
	if again.Meta.Players[a.PlayerID].Streak != 4 {
		t.Fatal("shard reload lost player meta")
	}
}
