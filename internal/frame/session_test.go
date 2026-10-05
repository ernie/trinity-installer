package frame

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type bare struct{}

func (bare) Read([]byte) (int, error) { return 0, os.ErrClosed }

func TestCtxReaderForwardsSize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, make([]byte, 1234), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := (ctxReader{context.Background(), f}).Size(); got != 1234 {
		t.Fatalf("size %d", got)
	}
	if got := (ctxReader{context.Background(), bare{}}).Size(); got != 0 {
		t.Fatalf("size %d for a reader without one", got)
	}
}
