package battleserver

import (
	"bytes"
	"github.com/freaks/openfreaks/internal/protocol"
	"testing"
)

func TestReplayArchiveIsBoundToContentAndCannotEscapeDirectory(t *testing.T) {
	dir := t.TempDir()
	data := &ReplayData{Version: 1, BattleID: "../../outside", Fingerprint: "catalog-a", Frames: []ReplayFrame{{Tick: 1, Data: []byte{1, 2, 3}}, {Tick: 2, Data: []byte{4, 5, 6}}}}
	if err := saveReplay(dir, data); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadReplay(dir, data.BattleID, "catalog-a")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.Frames[1].Data, data.Frames[1].Data) {
		t.Fatal("archive changed snapshot")
	}
	if _, err := loadReplay(dir, data.BattleID, "different-catalog"); err == nil {
		t.Fatal("played replay against mismatched models")
	}
}

func TestReplaySeekUsesAbsoluteTargetWhenPausedClientLags(t *testing.T) {
	frames := make([]ReplayFrame, 16002)
	for i := range frames {
		frames[i] = ReplayFrame{Tick: int32(i), Data: []byte{1}}
	}
	for _, base := range []int{187, 231, 500} {
		if got := replaySeekIndex(frames, base, protocol.PlayerAction{X: 80, Y: 1}); got != 80 {
			t.Fatalf("base %d: target %d", base, got)
		}
	}
	if got := replaySeekIndex(frames, 231, protocol.PlayerAction{X: -107}); got != 124 {
		t.Fatalf("legacy target %d", got)
	}
	dir := t.TempDir()
	data := &ReplayData{Version: 1, BattleID: "long-match", Frames: frames}
	if err := saveReplay(dir, data); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReplay(dir, data.BattleID, ""); err != nil {
		t.Fatal(err)
	}
}
