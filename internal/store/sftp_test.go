package store

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

type fakeSession struct {
	files map[string][]byte
	cmds  []string
	df    string
}

func (f *fakeSession) Home() string { return "/home/steamos" }
func (f *fakeSession) Run(ctx context.Context, cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	if strings.HasPrefix(cmd, "df ") {
		return f.df, nil
	}
	return "", nil
}
func (f *fakeSession) Put(ctx context.Context, remote string, r io.Reader, mode os.FileMode) error {
	b, _ := io.ReadAll(r)
	f.files[remote] = b
	return nil
}
func (f *fakeSession) Stat(remote string) (int64, bool, error) {
	b, ok := f.files[remote]
	return int64(len(b)), ok, nil
}
func (f *fakeSession) MkdirAll(string) error { return nil }
func (f *fakeSession) Close() error         { return nil }

func TestSFTPStoreDelegatesAndParsesDF(t *testing.T) {
	fs := &fakeSession{files: map[string][]byte{}, df: "Avail\n123456\n"}
	s := SFTP(fs)
	if err := s.Put(context.Background(), s.Join("/a", "b"), bytes.NewReader([]byte("xy")), 2, 0o644); err != nil {
		t.Fatal(err)
	}
	if string(fs.files["/a/b"]) != "xy" {
		t.Fatalf("%q", fs.files)
	}
	n, err := s.FreeSpace(context.Background(), "/a")
	if err != nil || n != 123456 {
		t.Fatalf("%d %v", n, err)
	}
	fs.df = "garbage"
	if _, err := s.FreeSpace(context.Background(), "/a"); err == nil {
		t.Fatal("garbage df accepted")
	}
}
