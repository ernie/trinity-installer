package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

type fakeFile struct {
	data []byte
	mode os.FileMode
}

// fakeSession records commands and keeps files in memory; Replies maps a command substring to its stdout.
type fakeSession struct {
	mu      sync.Mutex
	home    string
	cmds    []string
	files   map[string]fakeFile
	Replies map[string]string
	// Seq answers a command substring from a list, one entry per call; the last entry repeats.
	Seq  map[string][]string
	Fail map[string]error
	// FailOnce fails the first matching command and then clears itself.
	FailOnce map[string]error
}

func newFake() *fakeSession {
	return &fakeSession{home: "/home/steamos", files: map[string]fakeFile{}, Replies: map[string]string{}, Seq: map[string][]string{}, Fail: map[string]error{}, FailOnce: map[string]error{}}
}

func (f *fakeSession) Home() string { return f.home }

func (f *fakeSession) Run(ctx context.Context, cmd string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	for k, err := range f.Fail {
		if strings.Contains(cmd, k) {
			return "", err
		}
	}
	for k, err := range f.FailOnce {
		if strings.Contains(cmd, k) {
			delete(f.FailOnce, k)
			return "", err
		}
	}
	for k, seq := range f.Seq {
		if strings.Contains(cmd, k) {
			out := seq[0]
			if len(seq) > 1 {
				f.Seq[k] = seq[1:]
			}
			return out, nil
		}
	}
	for k, out := range f.Replies {
		if strings.Contains(cmd, k) {
			return out, nil
		}
	}
	return "", nil
}

func (f *fakeSession) Put(ctx context.Context, remote string, r io.Reader, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.Fail["put:"+remote]; ok {
		return err
	}
	f.files[remote] = fakeFile{data: b, mode: mode}
	return nil
}

func (f *fakeSession) Stat(remote string) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ff, ok := f.files[remote]
	if !ok {
		return 0, false, nil
	}
	return int64(len(ff.data)), true, nil
}

func (f *fakeSession) MkdirAll(string) error { return nil }
func (f *fakeSession) Close() error          { return nil }

func (f *fakeSession) ran(sub string) int {
	n := 0
	for _, c := range f.cmds {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

func (f *fakeSession) dump() string {
	var b strings.Builder
	for p, ff := range f.files {
		fmt.Fprintf(&b, "%s %o %d\n", p, ff.mode, len(ff.data))
	}
	return b.String()
}
