package store

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type local struct{}

func Local() Store { return local{} }

func (local) Join(elem ...string) string { return filepath.Join(elem...) }

func (local) MkdirAll(p string) error { return os.MkdirAll(p, 0o755) }

func (local) Stat(p string) (int64, bool, error) {
	st, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return st.Size(), true, nil
}

func (l local) Put(ctx context.Context, dst string, r io.Reader, size int64, mode os.FileMode) (err error) {
	if err := l.MkdirAll(filepath.Dir(dst)); err != nil {
		return err
	}
	part := dst + ".part"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(part)
		}
	}()
	if _, err = io.Copy(f, ctxReader{ctx, r}); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(part, mode); err != nil {
		return err
	}
	return os.Rename(part, dst)
}

func (local) FreeSpace(ctx context.Context, dir string) (int64, error) { return freeSpace(dir) }
