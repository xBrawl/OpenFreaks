package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/store"
)

func TestMigratePersistLoad(t *testing.T) {
	url := os.Getenv("PGTEST_URL")
	if url == "" {
		t.Skip("PGTEST_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema := fmt.Sprintf("of_test_%d", time.Now().UnixNano())
	pool, err := PoolWithSchema(ctx, url, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	defer func() { _, _ = pool.Exec(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`) }()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("re-migrate not idempotent: %v", err)
	}
	st := store.New("")
	acc := st.CreateAccount("pgtest", []byte("0123456789abcdef0123456789abcdef"))
	st.PutCredential(&store.Credential{LoginKey: "PGTEST", Nickname: "pgtest", PlayerID: acc.PlayerID, Salt: []byte("0123456789abcdef"), Hash: make([]byte, 32), Iterations: 210000})
	acc2 := st.CreateAccount("pgtest2", []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")[:32])
	st.AddFriendReq(acc.PlayerID, acc2.PlayerID)
	al := st.CreateAlliance("pgall", "desc", 0, acc.PlayerID)
	al.JoinRequests[acc2.PlayerID] = 123
	al.Messages = []store.AllianceMessage{{PlayerID: acc.PlayerID, Message: "hello", CreationTime: 456}}
	meta := st.Meta.Player(acc.PlayerID)
	meta.ResultRanks = map[int]map[int]int{1: {1: 1}}
	st.Meta.Player(acc2.PlayerID).ResultRanks = map[int]map[int]int{}
	meta.Mastery[2] = &store.Mastery{Points: 888, Matches: 8, Wins: 3, Damage: 17000}
	meta.Completed["wolf.intro"] = 123
	meta.OwnedStars[1] = true
	meta.Pass = store.PassProgress{Season: 1, Tier: 2, ClaimedFree: 7}
	st.Meta.Receipts["battle:b1"] = 456
	st.Meta.Bonds[store.BondKey(acc.PlayerID, acc2.PlayerID)] = &store.Bond{Low: acc.PlayerID, High: acc2.PlayerID, CreatedAt: 123, Matches: 12, Wins: 7}
	team := st.CreateTeam(acc.PlayerID, 1)
	_ = team
	st.AddBattle(store.BattleRecord{Players: []store.BattleParticipant{{PlayerID: acc.PlayerID, Name: "old name", CharacterModelID: 2, HPLevel: 3, Team: 0, Rank: 1}, {PlayerID: -1001, Name: "Bot", CharacterModelID: 4, HPLevel: 7, Team: 1, Rank: 2, IsBot: true}}, BattleID: "b1", LocationID: 1, ModeID: 1, StartedAt: 1, TotalTicks: 10,
		Participants: []int64{acc.PlayerID}, TrophyDeltas: map[int64]int32{acc.PlayerID: 8, -1001: 0}, Ranks: map[int64]int32{acc.PlayerID: 1}})
	if err := Persist(ctx, pool, st.DumpState()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = pool.Exec(cleanup, `DELETE FROM accounts WHERE name LIKE 'pgtest%'`)
		_, _ = pool.Exec(cleanup, `DELETE FROM alliances WHERE name='pgall'`)
		_, _ = pool.Exec(cleanup, `DELETE FROM battle_history WHERE battle_id='b1'`)
	}()
	d, err := Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Accounts) == 0 {
		t.Fatal("no accounts loaded")
	}
	found := false
	for _, a := range d.Accounts {
		if a.Name == "pgtest" {
			found = true
			if a.Wallet.Karit != acc.Wallet.Karit {
				t.Fatalf("wallet mismatch")
			}
		}
	}
	if !found {
		t.Fatal("pgtest account missing")
	}
	if d.Alliances[al.ID].JoinRequests[acc2.PlayerID] != 123 || len(d.Alliances[al.ID].Messages) != 1 {
		t.Fatal("clan state lost after restart")
	}
	if len(d.Battles) != 1 || d.Battles[0].Ranks[acc.PlayerID] != 1 || d.Battles[0].TrophyDeltas[acc.PlayerID] != 8 {
		t.Fatal("battle result lost after restart")
	}
	got, _ := json.Marshal(d.Meta)
	want, _ := json.Marshal(st.DumpState().Meta)
	if string(got) != string(want) {
		t.Fatal("full metadata changed after checkpoint reload")
	}
	if len(d.Teams) != 1 || len(d.Battles[0].Players) != 2 || d.Battles[0].Players[0].Name != "old name" {
		t.Fatal("immutable roster or party lost")
	}

	one, err := PoolWithSchema(ctx, url+"&pool_max_conns=1", schema)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	short, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := Persist(short, one, st.DumpState()); err != nil {
		t.Fatalf("transaction requires a second connection: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM openfreaks_state`); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Battles) != 1 || len(legacy.Battles[0].Players) != 2 || legacy.Battles[0].Players[0].HPLevel != 3 || legacy.Battles[0].Players[0].Name != "old name" || !legacy.Battles[0].Players[1].IsBot {
		t.Fatal("legacy history roster lost")
	}
	lease, err := AcquireWriter(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := AcquireWriter(ctx, pool); err == nil {
		second.Close()
		t.Fatal("second writer admitted")
	}
	oldToken := lease.Token
	lease.Close()
	lease, err = AcquireWriter(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := PersistOwned(ctx, pool, st.DumpState(), oldToken); err == nil {
		t.Fatal("disconnected old writer overwrote new state")
	}
	if err := PersistOwned(ctx, pool, st.DumpState(), lease.Token); err != nil {
		t.Fatal(err)
	}
	lease.Close()
	var currentSchema string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&currentSchema); err != nil || currentSchema != schema {
		t.Fatal("wrong schema")
	}
	bad := st.DumpState()
	bad.Accounts[acc.PlayerID].Name = "must rollback"
	bad.Accounts[acc.PlayerID].Wallet.Karit = -1
	if err := Persist(ctx, pool, bad); err == nil {
		t.Fatal("invalid wallet accepted")
	}
	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM accounts WHERE player_id=$1`, acc.PlayerID).Scan(&name); err != nil || name != "pgtest" {
		t.Fatal("failed persist changed durable account")
	}
}
