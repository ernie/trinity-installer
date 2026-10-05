package frame

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateKeyIsStable(t *testing.T) {
	dir := t.TempDir()
	s1, pub1, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub1, "ssh-rsa ") || strings.Contains(pub1, "\n") || !strings.Contains(pub1, " trinity-installer@") {
		t.Fatalf("pub line %q", pub1)
	}
	if _, err := os.Stat(filepath.Join(dir, "id_rsa")); err != nil {
		t.Fatal("private key not saved")
	}
	s2, pub2, err := LoadOrCreateKey(dir)
	if err != nil || pub2 != pub1 || string(s2.PublicKey().Marshal()) != string(s1.PublicKey().Marshal()) {
		t.Fatalf("second load differs: %v", err)
	}
}
