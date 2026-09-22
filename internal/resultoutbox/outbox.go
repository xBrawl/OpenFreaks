// Package resultoutbox durably retries Battle results until Main acknowledges
// its committed receipt. Retries after a crash are safe because Main deduplicates.
package resultoutbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/freaks/openfreaks/internal/battleserver"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Outbox struct {
	dir     string
	deliver func(*battleserver.Result) error
	log     *slog.Logger
	mu      sync.Mutex
	drainMu sync.Mutex
	wake    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func New(dir string, deliver func(*battleserver.Result) error, log *slog.Logger) (*Outbox, error) {
	if dir == "" || deliver == nil {
		return nil, fmt.Errorf("result outbox needs a directory and receiver")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	o := &Outbox{dir: dir, deliver: deliver, log: log, done: make(chan struct{}), wake: make(chan struct{}, 1)}

	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json.tmp") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil || info.Size() > 8<<20 {
				continue
			}
			raw, err := os.ReadFile(path)
			var result battleserver.Result
			if err != nil || json.Unmarshal(raw, &result) != nil || result.BattleID == "" {
				continue
			}
			sum := sha256.Sum256([]byte(result.BattleID))
			target := hex.EncodeToString(sum[:]) + ".json"
			if entry.Name() != target+".tmp" {
				continue
			}
			if _, err = os.Stat(filepath.Join(dir, target)); os.IsNotExist(err) {
				_ = os.Rename(path, filepath.Join(dir, target))
			}
		}
	}
	return o, nil
}
func (o *Outbox) Submit(r *battleserver.Result) error {
	if r == nil || r.BattleID == "" {
		return fmt.Errorf("invalid result")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(r.BattleID))
	name := hex.EncodeToString(sum[:]) + ".json"
	path := filepath.Join(o.dir, name)
	o.mu.Lock()
	defer o.mu.Unlock()
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	for _, e := range []error{writeErr, syncErr, closeErr} {
		if e != nil {
			return e
		}
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
	return nil
}

const (
	drainBudget      = 10 * time.Second
	maxResultBytes   = 8 << 20
	quarantineSuffix = ".bad"
)

func (o *Outbox) Drain() {
	o.drainMu.Lock()
	defer o.drainMu.Unlock()
	entries, err := os.ReadDir(o.dir)
	if err != nil {
		return
	}
	started := time.Now()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		if time.Since(started) > drainBudget {
			o.log.Warn("result outbox drain hit its time budget", "dir", o.dir)
			return
		}
		path := filepath.Join(o.dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > maxResultBytes {
			o.quarantine(path, "oversized")
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var result battleserver.Result
		if json.Unmarshal(raw, &result) != nil {
			o.quarantine(path, "unparseable")
			continue
		}
		if err := o.deliver(&result); err == nil {
			_ = os.Remove(path)
		} else {
			o.log.Warn("battle result awaits retry", "battle", result.BattleID, "err", err)
			continue
		}
	}
}

func (o *Outbox) quarantine(path, reason string) {
	target := path + quarantineSuffix
	if err := os.Rename(path, target); err != nil {
		o.log.Error("could not quarantine result", "path", path, "reason", reason, "err", err)
		return
	}
	o.log.Error("battle result quarantined", "path", target, "reason", reason)
}

func (o *Outbox) PendingCount() (count int, bytes int64) {
	entries, err := os.ReadDir(o.dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if info, err := e.Info(); err == nil {
			count++
			bytes += info.Size()
		}
	}
	return count, bytes
}
func (o *Outbox) Run() {
	o.Drain()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-o.done:
			return
		case <-ticker.C:
			o.Drain()
		case <-o.wake:
			o.Drain()
		}
	}
}
func (o *Outbox) Close() { o.once.Do(func() { close(o.done) }); o.Drain() }
