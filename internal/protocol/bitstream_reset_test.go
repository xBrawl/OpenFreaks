package protocol

import (
	"bytes"
	"testing"
)

func TestBitWriterResetMatchesFreshWriter(t *testing.T) {
	fill := func(w *BitWriter, seed int) {
		for i := 0; i < 400; i++ {
			w.WriteBool(i%3 == 0)
			w.WriteUnsigned((i*seed)%251+1, 8)
			w.WriteSized((i*seed)%60000+1, 4)
		}
	}

	fresh := NewBitWriter(8)
	fill(fresh, 7)
	want := append([]byte(nil), fresh.Bytes()...)

	reused := NewBitWriter(8)
	fill(reused, 13)
	reused.Reset()
	fill(reused, 7)

	if !bytes.Equal(want, reused.Bytes()) {
		t.Fatal("после Reset кодирование отличается от свежего writer")
	}
}
