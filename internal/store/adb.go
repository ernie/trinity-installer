package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/ernie/trinity-installer/internal/adb"
)

type adbStore struct {
	a      *adb.ADB
	serial string
}

func ADB(a *adb.ADB, serial string) Store { return adbStore{a, serial} }

func (adbStore) Join(elem ...string) string { return path.Join(elem...) }

func (s adbStore) MkdirAll(p string) error {
	_, err := s.a.Shell(context.Background(), s.serial, "mkdir -p "+shellQuote(p))
	return err
}

func (s adbStore) Stat(p string) (int64, bool, error) {
	out, err := s.a.Shell(context.Background(), s.serial, "stat -c %s "+shellQuote(p))
	if strings.Contains(out, "No such file") {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("could not read the size of %s: %s", p, strings.TrimSpace(out))
	}
	return n, true, nil
}

// Put stages the reader in a local temp file because adb push only takes a path.
func (s adbStore) Put(ctx context.Context, remote string, r io.Reader, size int64, mode os.FileMode) error {
	f, err := os.CreateTemp("", "trinity-push-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = io.Copy(f, ctxReader{ctx, r})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return s.a.Push(ctx, s.serial, f.Name(), remote)
}

func (s adbStore) FreeSpace(ctx context.Context, dir string) (int64, error) {
	out, err := s.a.Shell(ctx, s.serial, "df -k "+shellQuote(dir))
	if err != nil {
		return 0, fmt.Errorf("checking free space: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("could not read free space: %s", strings.TrimSpace(out))
	}
	n, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("could not read free space: %s", strings.TrimSpace(out))
	}
	return n * 1024, nil
}
