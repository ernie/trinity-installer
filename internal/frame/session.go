package frame

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var ErrNotAuthorized = errors.New("the headset has not accepted this installer's key")

type Session interface {
	Home() string
	Run(ctx context.Context, cmd string) (string, error)
	Put(ctx context.Context, remote string, r io.Reader, mode os.FileMode) error
	Stat(remote string) (size int64, exists bool, err error)
	MkdirAll(remote string) error
	Close() error
}

type sshSession struct {
	client *ssh.Client
	sftp   *sftp.Client
	home   string
	quit   chan struct{}
	once   sync.Once
}

// Connect opens ssh with the paired key; an unknown host key is recorded, a changed one is refused.
func Connect(ctx context.Context, h Headset, signer ssh.Signer, knownHostsPath string) (Session, error) {
	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(knownHostsPath); errors.Is(err, os.ErrNotExist) {
		os.WriteFile(knownHostsPath, nil, 0o600)
	}
	check, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, err
	}
	hostKey := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n"
			f, ferr := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_WRONLY, 0o600)
			if ferr != nil {
				return ferr
			}
			defer f.Close()
			_, ferr = f.WriteString(line)
			return ferr
		}
		if err != nil {
			return fmt.Errorf("the headset's ssh host key changed; remove it from %s if the headset was reset: %w", knownHostsPath, err)
		}
		return nil
	}
	cfg := &ssh.ClientConfig{
		User:            h.Login,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKey,
		Timeout:         10 * time.Second,
	}
	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", h.sshAddr())
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(cfg.Timeout))
	c, chans, reqs, err := ssh.NewClientConn(conn, h.sshAddr(), cfg)
	if err == nil {
		conn.SetDeadline(time.Time{})
	}
	if err != nil {
		conn.Close()
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, ErrNotAuthorized
		}
		return nil, err
	}
	client := ssh.NewClient(c, chans, reqs)
	sf, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("sftp: %w", err)
	}
	s := &sshSession{client: client, sftp: sf, quit: make(chan struct{})}
	go s.keepAlive()
	home, err := s.Run(ctx, "echo $HOME")
	if err != nil {
		s.Close()
		return nil, err
	}
	s.home = strings.TrimSpace(home)
	return s, nil
}

// keepAlive closes the client after three unanswered probes, so a stalled link fails an upload instead of hanging it.
func (s *sshSession) keepAlive() {
	const interval = 15 * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()
	misses := 0
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
		}
		// SendRequest blocks until the mux closes on a silent link, so the reply is awaited beside a timer.
		replied := make(chan error, 1)
		go func() {
			_, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil)
			replied <- err
		}()
		timeout := time.NewTimer(interval)
		select {
		case <-s.quit:
			timeout.Stop()
			return
		case err := <-replied:
			timeout.Stop()
			if err != nil {
				misses++
			} else {
				misses = 0
			}
		case <-timeout.C:
			misses++
		}
		if misses >= 3 {
			s.client.Close()
			return
		}
	}
}

func (s *sshSession) Home() string { return s.home }

func (s *sshSession) Run(ctx context.Context, cmd string) (string, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var out, errOut bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &errOut
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		sess.Signal(ssh.SIGKILL)
		sess.Close()
		// A stalled link may never acknowledge the close, so stop waiting; the buffers stay with the goroutine.
		select {
		case <-done:
			return out.String(), ctx.Err()
		case <-time.After(time.Second):
			return "", ctx.Err()
		}
	case err := <-done:
		if err != nil {
			return out.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(errOut.String()))
		}
		return out.String(), nil
	}
}

// Put uploads to remote+".part" and renames, so remote never holds a partial file.
func (s *sshSession) Put(ctx context.Context, remote string, r io.Reader, mode os.FileMode) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.MkdirAll(path.Dir(remote)); err != nil {
		return err
	}
	part := remote + ".part"
	defer func() {
		if err != nil {
			s.sftp.Remove(part)
		}
	}()
	f, err := s.sftp.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err = io.Copy(f, ctxReader{ctx, r}); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = s.sftp.Chmod(part, mode); err != nil {
		return err
	}
	if err = s.sftp.PosixRename(part, remote); err != nil {
		err = s.sftp.Rename(part, remote)
	}
	return err
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Size() int64 { return SizeOf(c.r) }

// SizeOf reports a reader's total size when it exposes one, else 0; pkg/sftp needs it to write concurrently.
func SizeOf(r io.Reader) int64 {
	switch r := r.(type) {
	case interface{ Size() int64 }:
		return r.Size()
	case interface{ Stat() (os.FileInfo, error) }:
		if fi, err := r.Stat(); err == nil {
			return fi.Size()
		}
	}
	return 0
}

func (c ctxReader) Read(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(b)
}

func (s *sshSession) Stat(remote string) (int64, bool, error) {
	st, err := s.sftp.Stat(remote)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return st.Size(), true, nil
}

func (s *sshSession) MkdirAll(remote string) error { return s.sftp.MkdirAll(remote) }

func (s *sshSession) Close() error {
	s.once.Do(func() { close(s.quit) })
	s.sftp.Close()
	return s.client.Close()
}
