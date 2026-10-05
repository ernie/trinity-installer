package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/crypto/ssh"

	"github.com/ernie/trinity-installer/assets/grid"
	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/install"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/release"
)

type ui struct {
	app     fyne.App
	win     fyne.Window
	cfgDir  string
	content *fyne.Container
	logFile *os.File
	logMu   sync.Mutex

	headsets    []frame.Headset
	headset     frame.Headset
	signer      ssh.Signer
	pubLine     string
	sess        frame.Session
	quake3Dir   string
	validation  quake3.Validation
	missionpack bool
	carry       *install.Carry

	// widgets other screens or tests reach into
	quake3Next                  *widget.Button
	baseq3Line, missionpackLine *widget.Label
	headsetManual               *widget.Entry
	headsetNext                 *widget.Button
	headsetStatus               *widget.Label
	doneRestart                 *widget.Button
	rows                        []*widget.Label
	logView                     *widget.Entry
}

func newUI(a fyne.App, w fyne.Window, cfgDir string) *ui {
	u := &ui{app: a, win: w, cfgDir: cfgDir, content: container.NewStack()}
	var err error
	u.logFile, err = os.OpenFile(filepath.Join(cfgDir, "install.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Println("cannot open the install log:", err)
	}
	w.SetContent(container.NewBorder(u.header(), nil, nil, nil, u.content))
	return u
}

func (u *ui) header() fyne.CanvasObject {
	icon := widget.NewIcon(fyne.NewStaticResource("icon.png", grid.Icon))
	title := widget.NewLabelWithStyle("Trinity Installer", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewHBox(icon, title)
}

func (u *ui) show(screen fyne.CanvasObject) {
	u.content.Objects = []fyne.CanvasObject{screen}
	u.content.Refresh()
}

// logf writes to the log file and, on the install screen, the log pane; callable from any goroutine.
func (u *ui) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	u.logMu.Lock()
	if u.logFile != nil {
		fmt.Fprintf(u.logFile, "%s %s\n", time.Now().Format("15:04:05"), line)
	}
	u.logMu.Unlock()
	log.Println(line)
	if u.logView != nil {
		fyne.Do(func() {
			u.logView.SetText(u.logView.Text + line + "\n")
			u.logView.CursorRow = len(u.logView.Text)
		})
	}
}

func (u *ui) pair(ctx context.Context, h frame.Headset, onDialog func()) error {
	var err error
	u.signer, u.pubLine, err = frame.LoadOrCreateKey(u.cfgDir)
	if err != nil {
		return err
	}
	if h.Login == "" {
		client := &http.Client{Timeout: 10 * time.Second}
		if h, err = frame.Lookup(ctx, client, h.Host); err != nil {
			return err
		}
	}
	sess, err := u.connect(ctx, h, onDialog)
	if err != nil {
		return err
	}
	if u.sess != nil {
		u.sess.Close()
	}
	u.headset, u.sess = h, sess
	u.logf("connected to %s as %s", h.Host, h.Login)
	return nil
}

// connect opens a session with the stored key; onDialog, when set, runs before waiting for a pairing approval.
func (u *ui) connect(ctx context.Context, h frame.Headset, onDialog func()) (frame.Session, error) {
	known := filepath.Join(u.cfgDir, "known_hosts")
	sess, err := frame.Connect(ctx, h, u.signer, known)
	if errors.Is(err, frame.ErrNotAuthorized) && onDialog != nil {
		onDialog()
		pairCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		if err := frame.Register(pairCtx, &http.Client{}, h, u.pubLine); err != nil {
			return nil, err
		}
		sess, err = frame.Connect(ctx, h, u.signer, known)
	}
	return sess, err
}

// liveSession replaces a session the headset dropped, so Retry does not fail on a dead link.
func (u *ui) liveSession(ctx context.Context) error {
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, err := u.sess.Run(probe, "true")
	cancel()
	if err == nil {
		return nil
	}
	sess, err := u.connect(ctx, u.headset, nil)
	if err != nil {
		return fmt.Errorf("reconnecting to %s: %w", u.headset.Host, err)
	}
	u.sess.Close()
	u.sess = sess
	u.logf("reconnected to %s", u.headset.Host)
	return nil
}

func (u *ui) installOptions() install.Options {
	return install.Options{
		GameID: "Trinity",
		Fetch: func(ctx context.Context, log func(string)) ([]byte, error) {
			client := &http.Client{Timeout: 10 * time.Minute}
			a, err := release.Latest(ctx, client, release.DefaultAPI)
			if err != nil {
				return nil, err
			}
			log(fmt.Sprintf("latest release %s, %d MB", a.Tag, a.Size>>20))
			last := int64(0)
			return release.Download(ctx, client, a, func(done int64) {
				if done-last > 8<<20 || done == a.Size {
					last = done
					log(fmt.Sprintf("downloaded %d of %d MB", done>>20, a.Size>>20))
				}
			})
		},
		Carry: u.carry,
		Paks:  u.validation.Selected(u.missionpack),
		Art:   grid.Art(),
	}
}

func (u *ui) logPath() string {
	if u.logFile != nil {
		return u.logFile.Name()
	}
	return u.cfgDir
}
