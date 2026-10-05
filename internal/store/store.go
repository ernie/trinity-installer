package store

import (
	"context"
	"io"
	"os"
)

// Store is the destination side of every push step; the transport differs per target.
type Store interface {
	Put(ctx context.Context, remote string, r io.Reader, size int64, mode os.FileMode) error
	Stat(remote string) (size int64, exists bool, err error)
	MkdirAll(remote string) error
	FreeSpace(ctx context.Context, dir string) (int64, error)
	Join(elem ...string) string
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(b)
}
