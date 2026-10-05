package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalPutIsAtomicAndSetsMode(t *testing.T) {
	dir := t.TempDir()
	s := Local()
	dst := s.Join(dir, "sub", "file.bin")
	if err := s.Put(context.Background(), dst, bytes.NewReader([]byte("hello")), 5, 0o644); err != nil {
		t.Fatal(err)
	}
	size, exists, err := s.Stat(dst)
	if err != nil || !exists || size != 5 {
		t.Fatalf("stat %d %v %v", size, exists, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(dst)); len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
	if _, exists, _ := s.Stat(s.Join(dir, "nope")); exists {
		t.Fatal("missing file reported present")
	}
}

func TestLocalPutCancelled(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Local().Put(ctx, filepath.Join(dir, "x"), bytes.NewReader(make([]byte, 1<<20)), 1<<20, 0o644)
	if err == nil {
		t.Fatal("cancelled put succeeded")
	}
	if _, exists, _ := Local().Stat(filepath.Join(dir, "x")); exists {
		t.Fatal("partial file left at the destination")
	}
}

func TestLocalFreeSpace(t *testing.T) {
	n, err := Local().FreeSpace(context.Background(), t.TempDir())
	if err != nil || n <= 0 {
		t.Fatalf("%d %v", n, err)
	}
}
