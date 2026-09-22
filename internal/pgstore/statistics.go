package pgstore

import (
	"context"
	"github.com/freaks/openfreaks/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// One-time import also upgrades early Go checkpoints; counts cover every durable
// result, independently of the short recent-history ring buffer.
func loadResultRanks(ctx context.Context, pool *pgxpool.Pool, d *store.Dump) error {
	if d.Meta == nil {
		d.Meta = store.NewMeta()
	}
	missing := map[int64]bool{}
	for id := range d.Accounts {
		meta := d.Meta.Player(id)
		if meta.ResultRanks == nil {
			missing[id] = true
			meta.ResultRanks = map[int]map[int]int{}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	rows, err := pool.Query(ctx, `SELECT player_id,game_mode_model_id,rank,COUNT(*) FROM battle_results GROUP BY player_id,game_mode_model_id,rank`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		var mode, rank, count int
		if err := rows.Scan(&pid, &mode, &rank, &count); err != nil {
			return err
		}
		if !missing[pid] {
			continue
		}
		m := d.Meta.Player(pid).ResultRanks
		if m[mode] == nil {
			m[mode] = map[int]int{}
		}
		m[mode][rank] = count
	}
	return rows.Err()
}
