package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/ernie/trinity-installer/internal/frame"
)

type sftpStore struct{ s frame.Session }

func SFTP(s frame.Session) Store { return sftpStore{s} }

func (sftpStore) Join(elem ...string) string { return path.Join(elem...) }

func (s sftpStore) MkdirAll(p string) error { return s.s.MkdirAll(p) }

func (s sftpStore) Stat(p string) (int64, bool, error) { return s.s.Stat(p) }

func (s sftpStore) Put(ctx context.Context, remote string, r io.Reader, size int64, mode os.FileMode) error {
	return s.s.Put(ctx, remote, sized{r, size}, mode)
}

// sized lets the session's sftp writes run concurrently, which needs the reader's size.
type sized struct {
	io.Reader
	n int64
}

func (s sized) Size() int64 { return s.n }

func (s sftpStore) FreeSpace(ctx context.Context, dir string) (int64, error) {
	out, err := s.s.Run(ctx, "df --output=avail -B1 "+shellQuote(dir))
	if err != nil {
		return 0, fmt.Errorf("checking free space: %w", err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, fmt.Errorf("could not read free space: %s", strings.TrimSpace(out))
	}
	n, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("could not read free space: %s", strings.TrimSpace(out))
	}
	return n, nil
}

func shellQuote(s string) string {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '/' || c == '.' || c == '_' || c == '-') {
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}
