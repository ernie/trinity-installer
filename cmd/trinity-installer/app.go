package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/crypto/ssh"

	"github.com/ernie/trinity-installer/assets/grid"
	"github.com/ernie/trinity-installer/internal/adb"
	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/install"
	"github.com/ernie/trinity-installer/internal/patch"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/target"
	frametarget "github.com/ernie/trinity-installer/internal/target/frame"
	"github.com/ernie/trinity-installer/internal/target/local"
)

type ui struct {
	app     fyne.App
	win     fyne.Window
	cfgDir  string
	content *fyne.Container
	logFile *os.File
	logMu   sync.Mutex
	goos    string

	headsets   []frame.Headset
	headset    frame.Headset
	signer     ssh.Signer
	pubLine    string
	quake3Dir  string
	validation quake3.Validation
	carry      *install.Carry
	target     target.Target
	plan       []target.Step
	pc         local.Options
	device     adb.Device
	devices    []adb.Device
	adb        *adb.ADB
	patchSet   *patch.Set
	// eulaAccepted keeps the license accepted when the user comes back to its screen.
	eulaAccepted bool

	// widgets other screens or tests reach into
	targetButtons               []*widget.Button
	pcFolder                    *widget.Entry
	pcSteam                     *widget.Check
	pcSteamNote                 *widget.Label
	pcNext                      *widget.Button
	deviceList                  *widget.List
	deviceNext                  *widget.Button
	deviceStatus                *widget.Label
	eulaScroll                  *container.Scroll
	eulaAgree                   *widget.Check
	eulaNext                    *widget.Button
	eulaRetry                   *widget.Button
	eulaScrolled                func(fyne.Position)
	installRetry, installBack   *widget.Button
	quake3Next                  *widget.Button
	baseq3Line, missionpackLine *widget.Label
	headsetManual               *widget.Entry
	headsetNext                 *widget.Button
	headsetStatus               *widget.Label
	doneRestart                 *widget.Button
	uninstallRemove             *widget.Button
	uninstallCancel             *widget.Button
	uninstallSettings           *widget.Check
	uninstallStatus             *widget.Label
	rows                        []string
	rowsView                    *widget.Label
	logView                     *widget.Entry
}

func newUI(a fyne.App, w fyne.Window, cfgDir string) *ui {
	u := &ui{app: a, win: w, cfgDir: cfgDir, content: container.NewStack()}
	var err error
	u.logFile, err = os.OpenFile(filepath.Join(cfgDir, "install.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Println("cannot open the install log:", err)
	}
	w.SetContent(container.NewBorder(nil, nil, u.panel(), nil, u.content))
	return u
}

// panelSize is the side panel cut from the SteamVR capsule; the window is this plus the 460 px content column.
var panelSize = fyne.NewSize(300, 520)

func (u *ui) panel() fyne.CanvasObject {
	img := canvas.NewImageFromResource(fyne.NewStaticResource("panel.png", grid.Panel))
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(panelSize)
	return img
}

// contentInset keeps the text off the window and panel edges; the log pane and lists inherit it too.
const contentInset = 12

func (u *ui) show(screen fyne.CanvasObject) {
	u.content.Objects = []fyne.CanvasObject{container.New(layout.NewCustomPaddedLayout(contentInset, contentInset, contentInset, contentInset), screen)}
	u.content.Refresh()
}

// buttonGap keeps neighboring navigation buttons from touching.
const buttonGap = 10

// buttonRow right-aligns a screen's navigation buttons with a gap between them.
func buttonRow(buttons ...fyne.CanvasObject) fyne.CanvasObject {
	items := []fyne.CanvasObject{layout.NewSpacer()}
	for i, b := range buttons {
		if i > 0 {
			b = container.New(layout.NewCustomPaddedLayout(0, 0, buttonGap, 0), b)
		}
		items = append(items, b)
	}
	return container.NewHBox(items...)
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
		addr := h.Addr
		if h, err = frame.Lookup(ctx, client, h.Host); err != nil {
			return err
		}
		h.Addr = addr
	}
	sess, err := u.connect(ctx, h, onDialog)
	if err != nil {
		return err
	}
	u.markPaired(h.Host)
	u.closeSession()
	u.headset = h
	u.target = frametarget.New(sess, func(ctx context.Context) (frame.Session, error) { return u.connect(ctx, u.headset, nil) }, "Trinity")
	u.logf("connected to %s as %s", h.Host, h.Login)
	return nil
}

// markPaired records a headset that accepted the key; known_hosts cannot tell, since ssh fills it before authenticating.
func (u *ui) markPaired(host string) {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
			return r
		}
		return '_'
	}, host)
	if err := os.WriteFile(filepath.Join(u.cfgDir, "paired-"+name), nil, 0o600); err != nil {
		u.logf("could not record the pairing: %v", err)
	}
}

func (u *ui) paired() bool {
	stamps, _ := filepath.Glob(filepath.Join(u.cfgDir, "paired-*"))
	return len(stamps) > 0
}

func (u *ui) closeSession() {
	if ft, ok := u.target.(*frametarget.Target); ok {
		ft.Session().Close()
	}
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

func (u *ui) installOptions() install.Options {
	opts := install.Options{
		Fetch: func(ctx context.Context, spec release.Spec, log func(string)) ([]byte, string, error) {
			client := &http.Client{Timeout: 10 * time.Minute}
			a, err := release.Latest(ctx, client, spec.API, spec.Asset)
			if err != nil {
				return nil, "", err
			}
			log(fmt.Sprintf("latest release %s, %d MB", a.Tag, a.Size>>20))
			last := int64(0)
			raw, err := release.Download(ctx, client, a, func(done int64) {
				if done-last > 8<<20 || done == a.Size {
					last = done
					log(fmt.Sprintf("downloaded %d of %d MB", done>>20, a.Size>>20))
				}
			})
			return raw, a.Tag, err
		},
		Carry: u.carry,
		Paks:  u.validation.LocalPaks(),
		Art:   grid.Art(),
	}
	if rels := u.validation.NeededPatch(); len(rels) > 0 {
		opts.PatchRels = rels
		opts.Patch = u.patchSet
	}
	return opts
}

func (u *ui) logPath() string {
	if u.logFile != nil {
		return u.logFile.Name()
	}
	return u.cfgDir
}
