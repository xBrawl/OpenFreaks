package resultoutbox

import (
	"errors"
	"github.com/freaks/openfreaks/internal/battleserver"
	"os"
	"testing"
)

func TestQueuedResultSurvivesReceiverFailureAndRestart(t *testing.T) {
	dir := t.TempDir()
	first, _ := New(dir, func(*battleserver.Result) error { return errors.New("main unavailable") }, nil)
	if err := first.Submit(&battleserver.Result{BattleID: "one-match", FinalTick: 42}); err != nil {
		t.Fatal(err)
	}
	first.Drain()
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("failed delivery lost durable result")
	}
	count := 0
	restarted, _ := New(dir, func(r *battleserver.Result) error {
		if r.BattleID != "one-match" || r.FinalTick != 42 {
			t.Fatal("wrong recovered result")
		}
		count++
		return nil
	}, nil)
	restarted.Drain()
	restarted.Drain()
	files, _ = os.ReadDir(dir)
	if count != 1 || len(files) != 0 {
		t.Fatal("result duplicated or not acknowledged")
	}
}
