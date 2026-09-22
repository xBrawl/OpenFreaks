package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/freaks/openfreaks/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	shardPlayer   = "player"
	shardCred     = "cred"
	shardAlliance = "alliance"
	shardBond     = "bond"
	shardReceipt  = "receipt"
	shardReactor  = "reactor"
	shardCore     = "core"
)

const coreKey = "-"

const battlesKey = "battles"

// playerShard держит всё, что принадлежит одному игроку: аккаунт, его мету и
// строку связей. Награда и прогресс должны становиться видимыми одним шагом.
type playerShard struct {
	Account *store.Account    `json:"account"`
	Meta    *store.PlayerMeta `json:"meta,omitempty"`
	Friends map[int64]int32   `json:"friends,omitempty"`
}

type coreShard struct {
	Teams       map[int64]*store.Team             `json:"teams"`
	Arenas      map[string]*store.ArenaTournament `json:"arenas"`
	Allocations map[string]*store.Allocation      `json:"allocations"`
	PlayerSeq   int64                             `json:"playerSeq"`
	AllianceSeq int64                             `json:"allianceSeq"`
	TeamSeq     int64                             `json:"teamSeq"`
}

type shardWrite struct {
	kind    string
	key     string
	payload []byte
	drop    bool
}

func marshalShard(kind, key string, v any) (shardWrite, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return shardWrite{}, fmt.Errorf("checkpoint shard %s/%s: %w", kind, key, err)
	}
	return shardWrite{kind: kind, key: key, payload: b}, nil
}

// checkpointShards собирает записи чекпоинта для изменённых сущностей.
// Отсутствие сущности в состоянии означает удаление её шарда.
func checkpointShards(d *store.Dump, dirty *store.Dirty) ([]shardWrite, error) {
	var out []shardWrite
	meta := d.Meta
	if meta == nil {
		meta = store.NewMeta()
	}

	players := map[int64]bool{}
	for pid := range dirty.Accounts {
		players[pid] = true
	}

	for pid := range dirty.Friends {
		players[pid] = true
	}
	if dirty.All {
		for pid := range d.Accounts {
			players[pid] = true
		}
	}
	for pid := range players {
		account, ok := d.Accounts[pid]
		if !ok {
			out = append(out, shardWrite{kind: shardPlayer, key: keyInt(pid), drop: true})
			continue
		}
		w, err := marshalShard(shardPlayer, keyInt(pid), playerShard{
			Account: account, Meta: meta.Players[pid], Friends: d.Friends[pid],
		})
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	creds := keySet(dirty.Creds, dirty.All, d.Creds)
	for key := range creds {
		c, ok := d.Creds[key]
		if !ok {
			out = append(out, shardWrite{kind: shardCred, key: key, drop: true})
			continue
		}
		w, err := marshalShard(shardCred, key, c)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	alliances := map[int64]bool{}
	for id := range dirty.Alliances {
		alliances[id] = true
	}
	if dirty.All {
		for id := range d.Alliances {
			alliances[id] = true
		}
	}
	for id := range alliances {
		al, ok := d.Alliances[id]
		if !ok {
			out = append(out, shardWrite{kind: shardAlliance, key: keyInt(id), drop: true})
			continue
		}
		w, err := marshalShard(shardAlliance, keyInt(id), al)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	for key := range keySet(dirty.Bonds, dirty.All, meta.Bonds) {
		b, ok := meta.Bonds[key]
		if !ok {
			out = append(out, shardWrite{kind: shardBond, key: key, drop: true})
			continue
		}
		w, err := marshalShard(shardBond, key, b)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	for key := range keySet(dirty.Receipts, dirty.All, meta.Receipts) {
		v, ok := meta.Receipts[key]
		if !ok {
			out = append(out, shardWrite{kind: shardReceipt, key: key, drop: true})
			continue
		}
		w, err := marshalShard(shardReceipt, key, v)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	for key := range keySet(dirty.Reactors, dirty.All, meta.Reactors) {
		r, ok := meta.Reactors[key]
		if !ok {
			out = append(out, shardWrite{kind: shardReactor, key: key, drop: true})
			continue
		}
		w, err := marshalShard(shardReactor, key, r)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	if dirty.All || dirty.Core {
		w, err := marshalShard(shardCore, coreKey, coreShard{
			Teams: d.Teams, Arenas: meta.Arenas, Allocations: meta.Allocations,
			PlayerSeq: d.PlayerSeq, AllianceSeq: d.AllianceSeq, TeamSeq: d.TeamSeq,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	if dirty.All || len(dirty.Battles) > 0 {
		w, err := marshalShard(shardCore, battlesKey, d.Battles)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

func keySet[V any](dirty map[string]bool, all bool, live map[string]V) map[string]bool {
	out := make(map[string]bool, len(dirty))
	for k := range dirty {
		out[k] = true
	}
	if all {
		for k := range live {
			out[k] = true
		}
	}
	return out
}

func keyInt(v int64) string { return fmt.Sprintf("%d", v) }

func queueShards(batch *pgx.Batch, writes []shardWrite) {
	for _, w := range writes {
		if w.drop {
			batch.Queue(`DELETE FROM openfreaks_state_shards WHERE kind=$1 AND key=$2`, w.kind, w.key)
			continue
		}
		batch.Queue(`INSERT INTO openfreaks_state_shards(kind,key,payload) VALUES($1,$2,$3)
            ON CONFLICT(kind,key) DO UPDATE SET payload=EXCLUDED.payload,updated_at=now()`,
			w.kind, w.key, w.payload)
	}
}

// loadShards собирает Dump из шардов. Второй результат — false, если шардов
// нет и нужно читать прежние источники.
func loadShards(ctx context.Context, pool *pgxpool.Pool) (*store.Dump, bool, error) {
	rows, err := pool.Query(ctx, `SELECT kind,key,payload FROM openfreaks_state_shards`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	d := &store.Dump{
		Accounts: map[int64]*store.Account{}, Creds: map[string]*store.Credential{},
		Alliances: map[int64]*store.Alliance{}, Friends: map[int64]map[int64]int32{},
		Teams: map[int64]*store.Team{}, Meta: store.NewMeta(),
	}
	found := false
	for rows.Next() {
		var kind, key string
		var payload []byte
		if err := rows.Scan(&kind, &key, &payload); err != nil {
			return nil, false, err
		}
		found = true
		if err := applyShard(d, kind, key, payload); err != nil {
			return nil, false, err
		}
	}
	if rows.Err() != nil {
		return nil, false, rows.Err()
	}
	if !found {
		return nil, false, nil
	}
	return d, true, nil
}

func applyShard(d *store.Dump, kind, key string, payload []byte) error {
	fail := func(err error) error {
		return fmt.Errorf("invalid checkpoint shard %s/%s: %w", kind, key, err)
	}
	switch kind {
	case shardPlayer:
		var p playerShard
		if err := json.Unmarshal(payload, &p); err != nil {
			return fail(err)
		}
		if p.Account == nil {
			return fail(fmt.Errorf("missing account"))
		}
		d.Accounts[p.Account.PlayerID] = p.Account
		if p.Meta != nil {
			d.Meta.Players[p.Account.PlayerID] = p.Meta
		}
		if p.Friends != nil {
			d.Friends[p.Account.PlayerID] = p.Friends
		}
		if p.Account.PlayerID > d.PlayerSeq {
			d.PlayerSeq = p.Account.PlayerID
		}
	case shardCred:
		var c store.Credential
		if err := json.Unmarshal(payload, &c); err != nil {
			return fail(err)
		}
		d.Creds[key] = &c
	case shardAlliance:
		var al store.Alliance
		if err := json.Unmarshal(payload, &al); err != nil {
			return fail(err)
		}
		d.Alliances[al.ID] = &al
	case shardBond:
		var b store.Bond
		if err := json.Unmarshal(payload, &b); err != nil {
			return fail(err)
		}
		d.Meta.Bonds[key] = &b
	case shardReceipt:
		var v int64
		if err := json.Unmarshal(payload, &v); err != nil {
			return fail(err)
		}
		d.Meta.Receipts[key] = v
	case shardReactor:
		var r store.Reactor
		if err := json.Unmarshal(payload, &r); err != nil {
			return fail(err)
		}
		d.Meta.Reactors[key] = &r
	case shardCore:
		if key == battlesKey {
			var battles []store.BattleRecord
			if err := json.Unmarshal(payload, &battles); err != nil {
				return fail(err)
			}
			d.Battles = battles
			return nil
		}
		var c coreShard
		if err := json.Unmarshal(payload, &c); err != nil {
			return fail(err)
		}
		if c.Teams != nil {
			d.Teams = c.Teams
		}
		if c.Arenas != nil {
			d.Meta.Arenas = c.Arenas
		}
		if c.Allocations != nil {
			d.Meta.Allocations = c.Allocations
		}
		if c.PlayerSeq > d.PlayerSeq {
			d.PlayerSeq = c.PlayerSeq
		}
		d.AllianceSeq = c.AllianceSeq
		d.TeamSeq = c.TeamSeq
	default:

	}
	return nil
}

// convertLegacyCheckpoint переносит единый блоб openfreaks_state в шарды и
// удаляет его в той же транзакции. Промежуточного состояния, в котором Load
// видит и то и другое, не возникает.
func convertLegacyCheckpoint(ctx context.Context, pool *pgxpool.Pool, d *store.Dump) error {
	writes, err := checkpointShards(d, store.FullDirty())
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	queueShards(batch, writes)
	batch.Queue(`DELETE FROM openfreaks_state WHERE singleton`)
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
