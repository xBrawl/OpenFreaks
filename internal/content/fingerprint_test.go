package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFingerprintChangesWithCatalogAndIgnoresStaleFile(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "models"), 0700)
	p := filepath.Join(root, "models", "characters.yaml")
	os.WriteFile(p, []byte("first"), 0600)
	a := Load(root)
	os.WriteFile(p, []byte("second"), 0600)
	b := Load(root)
	if a.FingerprintChecksum() == b.FingerprintChecksum() {
		t.Fatal("unchanged fingerprint")
	}
	if !strings.Contains(string(b.FingerprintFile().Content), b.FingerprintChecksum()) {
		t.Fatal("stale manifest served")
	}
}
