package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync"
	"time"
)

// A full-state checkpoint has exactly one writer per database/schema. A second
// Main process must fail startup instead of overwriting newer player balances.
type WriterLease struct {
	Token string
	Close func()
}

func AcquireWriter(ctx context.Context, pool *pgxpool.Pool) (*WriterLease, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended(current_database()||':'||current_schema()||':openfreaks-writer',0))`).Scan(&acquired)
	if err != nil || !acquired {
		conn.Release()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("another Main owns this database schema")
	}
	var once sync.Once
	closeLease := func() {
		once.Do(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended(current_database()||':'||current_schema()||':openfreaks-writer',0))`)
			if err != nil {
				_ = conn.Conn().Close(cleanup)
			}
			conn.Release()
		})
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		closeLease()
		return nil, err
	}
	lease := &WriterLease{Token: hex.EncodeToString(token[:]), Close: closeLease}
	if _, err := conn.Exec(ctx, `INSERT INTO openfreaks_writer(singleton,token) VALUES(TRUE,$1) ON CONFLICT(singleton) DO UPDATE SET token=EXCLUDED.token`, lease.Token); err != nil {
		closeLease()
		return nil, err
	}
	return lease, nil
}
