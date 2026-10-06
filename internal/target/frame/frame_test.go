package frametarget

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/install"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/target"
)

var (
	_ target.Target    = (*Target)(nil)
	_ target.Restarter = (*Target)(nil)
)

type fakeFile struct {
	data []byte
	mode os.FileMode
}

// fakeSession records commands and puts (as "put:<path>") in one sequence and keeps files in memory; Replies maps a command substring to its stdout.
type fakeSession struct {
	mu      sync.Mutex
	home    string
	cmds    []string
	files   map[string]fakeFile
	closed  bool
	Replies map[string]string
	// Seq answers a command substring from a list, one entry per call; the last entry repeats.
	Seq  map[string][]string
	Fail map[string]error
	// FailCmd fails only a command equal to its key, so the "true" probe never matches a script that contains "true".
	FailCmd map[string]error
	// FailOnce fails the first matching command and then clears itself.
	FailOnce map[string]error
}

func newFake() *fakeSession {
	return &fakeSession{home: "/home/steamos", files: map[string]fakeFile{}, Replies: map[string]string{}, Seq: map[string][]string{}, Fail: map[string]error{}, FailCmd: map[string]error{}, FailOnce: map[string]error{}}
}

func (f *fakeSession) Home() string { return f.home }

func (f *fakeSession) Run(ctx context.Context, cmd string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	if err, ok := f.FailCmd[cmd]; ok {
		return "", err
	}
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
	f.cmds = append(f.cmds, "put:"+remote)
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
func (f *fakeSession) Close() error          { f.closed = true; return nil }

func (f *fakeSession) ran(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.cmds {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

// runs counts the recorded commands equal to cmd.
func (f *fakeSession) runs(cmd string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.cmds {
		if c == cmd {
			n++
		}
	}
	return n
}

// first is the position of the first recorded command or put containing sub, or -1.
func (f *fakeSession) first(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.cmds {
		if strings.Contains(c, sub) {
			return i
		}
	}
	return -1
}

func (f *fakeSession) dump() string {
	var lines []string
	for p, ff := range f.files {
		lines = append(lines, fmt.Sprintf("%s %o %d", p, ff.mode, len(ff.data)))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// shortcutsVDF encodes one devkit shortcut entry the way Steam writes it.
func shortcutsVDF(exe string, appID uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x00Shortcuts\x00\x00" + "0\x00")
	b.WriteString("\x02appid\x00")
	b.Write([]byte{byte(appID), byte(appID >> 8), byte(appID >> 16), byte(appID >> 24)})
	b.WriteString("\x01AppName\x00Devkit Game: Trinity\x00")
	b.WriteString("\x01Exe\x00\"" + exe + "\"\x00")
	b.WriteString("\x00tags\x00\x08\x08\x08\x08")
	return b.Bytes()
}

func zipOf(names ...string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range names {
		f, _ := w.Create(name)
		f.Write([]byte("x " + name))
	}
	w.Close()
	return buf.Bytes()
}

var frameFiles = []string{"trinity", "trinity.ded", "trinity_vulkan_aarch64.so", "libopenxr_loader.so.1", "vrpreferences.json", "baseq3/pak8t.pk3", "missionpack/pak3t.pk3"}

func localPaks(t *testing.T) []quake3.Pak {
	dir := t.TempDir()
	var paks []quake3.Pak
	for _, rel := range []string{"baseq3/pak0.pk3", "baseq3/pak1.pk3", "missionpack/pak0.pk3"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("pak "+rel), 0o644)
		paks = append(paks, quake3.Pak{Rel: rel, Path: p, Size: int64(len("pak " + rel))})
	}
	return paks
}

const shortcutsCat = "cat ~/.local/share/Steam/userdata/10005062/config/shortcuts.vdf"

// headset answers every command the way a healthy Frame with one Steam user does.
func headset(sess *fakeSession) {
	sess.Replies["df --output=avail"] = "Avail\n9000000000\n"
	sess.Replies["ls ~/.local/share/Steam/userdata"] = "10005062\n"
	sess.Replies["steam.pipe"] = "OK\n"
	sess.Replies["--dumpapps"] = "steam.app.2634369398 : Trinity : builtin\n"
	sess.Replies[shortcutsCat] = string(shortcutsVDF("/home/steamos/devkit-game/Trinity/trinity", 2634369398))
}

func setup(t *testing.T) (*Target, *fakeSession, install.Options) {
	sess := newFake()
	headset(sess)
	tg := New(sess, func(context.Context) (frame.Session, error) { return nil, errors.New("no reconnect in this test") }, "Trinity")
	tg.Poll = time.Millisecond
	tg.ResponsePath = func() string { return "/tmp/trinity-installer-1/registered" }
	opts := install.Options{
		Fetch: func(context.Context, release.Spec, func(string)) ([]byte, string, error) {
			return zipOf(frameFiles...), "v1", nil
		},
		Paks: localPaks(t),
		Art:  map[string][]byte{"capsule": {1}, "wide": {2}, "hero": {3}, "logo": {4}, "icon": {5}},
	}
	return tg, sess, opts
}

func runFrom(tg *Target, opts install.Options, from int) ([]install.Progress, error) {
	plan, err := target.Plan(context.Background(), tg)
	if err != nil {
		return nil, err
	}
	var events []install.Progress
	err = install.Run(context.Background(), tg, plan, opts, from, func(p install.Progress) { events = append(events, p) })
	return events, err
}

func stepError(t *testing.T, err error, want target.Step) *install.StepError {
	t.Helper()
	var se *install.StepError
	if !errors.As(err, &se) || se.Step != want {
		t.Fatalf("want a failure at %s, got %v", want, err)
	}
	return se
}

func TestPlanIsTheFullFrameList(t *testing.T) {
	tg, _, _ := setup(t)
	plan, err := target.Plan(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	want := []target.Step{target.FetchRelease, target.PrepareDestination, target.PushPackage, target.PushRetailPaks, target.PushPatch, target.RegisterLaunchEntry, target.ReadAppID, target.RegisterVR, target.InstallArtwork}
	if fmt.Sprint(plan) != fmt.Sprint(want) {
		t.Fatalf("%v", plan)
	}
}

func TestRunHappyPath(t *testing.T) {
	tg, sess, opts := setup(t)
	events, err := runFrom(tg, opts, 0)
	if err != nil {
		t.Fatalf("%v\n%s", err, sess.dump())
	}
	files := sess.dump()
	for _, want := range []string{
		"/home/steamos/devkit-game/Trinity/trinity 755",
		"/home/steamos/devkit-game/Trinity/trinity.ded 755",
		"/home/steamos/devkit-game/Trinity/trinity_vulkan_aarch64.so 755",
		"/home/steamos/devkit-game/Trinity/libopenxr_loader.so.1 755",
		"/home/steamos/devkit-game/Trinity/vrpreferences.json 644",
		"/home/steamos/devkit-game/Trinity/baseq3/pak8t.pk3 644",
		"/home/steamos/devkit-game/Trinity/trinity-capsule.png 644",
		"/home/steamos/devkit-game/Trinity/baseq3/pak0.pk3 644",
		"/home/steamos/devkit-game/Trinity/missionpack/pak0.pk3 644",
		"/home/steamos/devkit-game/Trinity-argv.json 644",
		"/home/steamos/devkit-game/Trinity-settings.json 644",
		"/home/steamos/devkit-game/Trinity/trinity.vrmanifest 644",
		"/home/steamos/.local/share/Steam/userdata/10005062/config/grid/2634369398p.png 644",
		"/home/steamos/.local/share/Steam/userdata/10005062/config/grid/2634369398_hero.png 644",
	} {
		if !strings.Contains(files, want) {
			t.Fatalf("missing %q in\n%s", want, files)
		}
	}
	if got := string(sess.files["/home/steamos/devkit-game/Trinity-argv.json"].data); got != `["trinity","+set","vr_enabled","1","+set","vr_mirrorEnabled","0"]` {
		t.Fatalf("argv %q", got)
	}
	if got := string(sess.files["/home/steamos/devkit-game/Trinity-settings.json"].data); !strings.Contains(got, `"compat_tool": "SteamLinuxRuntime_4-arm64"`) {
		t.Fatalf("settings %q", got)
	}
	if got := sess.files["/home/steamos/devkit-game/Trinity/trinity-capsule.png"].data; !bytes.Equal(got, []byte{1}) {
		t.Fatalf("capsule %v", got)
	}
	if m := string(sess.files["/home/steamos/devkit-game/Trinity/trinity.vrmanifest"].data); !strings.Contains(m, "steam.app.2634369398") || !strings.Contains(m, `"arguments": "+set vr_enabled 1 +set vr_mirrorEnabled 0"`) {
		t.Fatal("manifest does not name the app id")
	}
	if sess.ran("--appmanifest /home/steamos/devkit-game/Trinity/trinity.vrmanifest") != 1 || sess.ran("--dumpapps") != 1 {
		t.Fatalf("vrcmd calls: %v", sess.cmds)
	}
	if capsule, manifest := sess.first("put:/home/steamos/devkit-game/Trinity/trinity-capsule.png"), sess.first("--appmanifest"); capsule < 0 || capsule > manifest {
		t.Fatalf("capsule (%d) not in place before the manifest is registered (%d): %v", capsule, manifest, sess.cmds)
	}
	if sess.ran("/tmp/trinity-installer-1/registered") != 1 {
		t.Fatalf("register calls: %v", sess.cmds)
	}
	last := events[len(events)-1]
	if last.Index != 8 || last.Step != target.InstallArtwork || last.State != install.Done {
		t.Fatalf("last event %+v", last)
	}
}

func TestRetryFromFailedStep(t *testing.T) {
	tg, sess, opts := setup(t)
	opts.Carry = &install.Carry{}
	sess.Replies["steam.pipe"] = "NOSTEAM\n"
	_, err := runFrom(tg, opts, 0)
	se := stepError(t, err, target.RegisterLaunchEntry)
	if !strings.Contains(err.Error(), "Steam is not running") {
		t.Fatalf("%v", err)
	}
	sess.Replies["steam.pipe"] = "OK\n"
	dfs, puts := sess.ran("df --output=avail"), len(sess.files)
	if _, err := runFrom(tg, opts, se.Index); err != nil {
		t.Fatal(err)
	}
	if sess.ran("df --output=avail") != dfs || sess.runs("true") != 1 {
		t.Fatalf("retry re-ran earlier steps or skipped the probe: %v", sess.cmds)
	}
	if len(sess.files) <= puts || sess.closed || tg.Session() != sess {
		t.Fatal("retry did not finish on the live session")
	}
}

func TestRetryReconnectsToANewSession(t *testing.T) {
	tg, first, opts := setup(t)
	opts.Carry = &install.Carry{}
	first.Replies["steam.pipe"] = "NOSTEAM\n"
	_, err := runFrom(tg, opts, 0)
	se := stepError(t, err, target.RegisterLaunchEntry)
	first.FailCmd["true"] = errors.New("connection lost")
	second := newFake()
	headset(second)
	tg.reconnect = func(context.Context) (frame.Session, error) { return second, nil }
	if _, err := runFrom(tg, opts, se.Index); err != nil {
		t.Fatalf("%v\n%s", err, second.dump())
	}
	if !first.closed || tg.Session() != second {
		t.Fatal("the dead session was not replaced")
	}
	if second.ran("df --output=avail") != 0 || second.ran("--dumpapps") != 1 {
		t.Fatalf("wrong steps on the new session: %v", second.cmds)
	}
	if _, ok := second.files["/home/steamos/devkit-game/Trinity/trinity.vrmanifest"]; !ok {
		t.Fatalf("manifest missing on the new session:\n%s", second.dump())
	}
}

func TestRetryReportsAFailedReconnect(t *testing.T) {
	tg, sess, opts := setup(t)
	opts.Carry = &install.Carry{}
	sess.Replies["steam.pipe"] = "NOSTEAM\n"
	_, err := runFrom(tg, opts, 0)
	se := stepError(t, err, target.RegisterLaunchEntry)
	sess.FailCmd["true"] = errors.New("connection lost")
	_, err = runFrom(tg, opts, se.Index)
	stepError(t, err, target.RegisterLaunchEntry)
	if !strings.Contains(err.Error(), "reconnecting to the headset") || sess.closed {
		t.Fatalf("%v (closed %v)", err, sess.closed)
	}
}

func TestUserIDRejectsSeveral(t *testing.T) {
	tg, sess, opts := setup(t)
	sess.Replies["ls ~/.local/share/Steam/userdata"] = "10005062\n20000001\n"
	_, err := runFrom(tg, opts, 0)
	stepError(t, err, target.ReadAppID)
	if !strings.Contains(err.Error(), "10005062") || !strings.Contains(err.Error(), "20000001") {
		t.Fatalf("%v", err)
	}
}

func TestAppIDMissing(t *testing.T) {
	tg, sess, opts := setup(t)
	sess.Replies[shortcutsCat] = string(shortcutsVDF("/home/steamos/devkit-game/Other/trinity", 7))
	_, err := runFrom(tg, opts, 0)
	stepError(t, err, target.ReadAppID)
}

func TestAppIDAppearsAfterPolling(t *testing.T) {
	tg, sess, opts := setup(t)
	delete(sess.Replies, shortcutsCat)
	sess.Seq[shortcutsCat] = []string{
		string(shortcutsVDF("/home/steamos/devkit-game/Other/trinity", 7)),
		string(shortcutsVDF("/home/steamos/devkit-game/Other/trinity", 7)),
		string(shortcutsVDF("/home/steamos/devkit-game/Trinity/trinity", 2634369398)),
	}
	if _, err := runFrom(tg, opts, 0); err != nil {
		t.Fatal(err)
	}
	if sess.ran(shortcutsCat) != 3 {
		t.Fatalf("polled %d times", sess.ran(shortcutsCat))
	}
}

func TestAppIDSurvivesCatAndParseErrors(t *testing.T) {
	tg, sess, opts := setup(t)
	delete(sess.Replies, shortcutsCat)
	sess.FailOnce[shortcutsCat] = errors.New("no such file")
	sess.Seq[shortcutsCat] = []string{
		"garbage",
		string(shortcutsVDF("/home/steamos/devkit-game/Trinity/trinity", 2634369398)),
	}
	if _, err := runFrom(tg, opts, 0); err != nil {
		t.Fatal(err)
	}
	if sess.ran(shortcutsCat) != 3 {
		t.Fatalf("cat ran %d times", sess.ran(shortcutsCat))
	}
}

func TestAppIDPersistentErrorKeepsOriginalMessage(t *testing.T) {
	tg, sess, opts := setup(t)
	sess.Fail[shortcutsCat] = errors.New("no such file")
	_, err := runFrom(tg, opts, 0)
	stepError(t, err, target.ReadAppID)
	if !strings.Contains(err.Error(), "Steam has no shortcut for") || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("%v", err)
	}
}

func TestManifestNotListed(t *testing.T) {
	tg, sess, opts := setup(t)
	sess.Replies["--dumpapps"] = "steam.app.1 : Other : steam\n"
	_, err := runFrom(tg, opts, 0)
	stepError(t, err, target.RegisterVR)
}

func TestRegisterVRNeedsTheCapsule(t *testing.T) {
	tg, sess, opts := setup(t)
	delete(opts.Art, "capsule")
	_, err := runFrom(tg, opts, 0)
	stepError(t, err, target.RegisterVR)
	if !strings.Contains(err.Error(), "no artwork for capsule") || sess.ran("--appmanifest") != 0 {
		t.Fatalf("%v: %v", err, sess.cmds)
	}
}

func TestRegisterTimeoutFails(t *testing.T) {
	// The script itself waits; here we pin that the Go side treats a lone "TIMEOUT" as failure, not success.
	tg, sess, opts := setup(t)
	sess.Replies["steam.pipe"] = "TIMEOUT\n"
	_, err := runFrom(tg, opts, 0)
	stepError(t, err, target.RegisterLaunchEntry)
	if !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("%v", err)
	}
}

func TestRestartSteam(t *testing.T) {
	tg, sess, _ := setup(t)
	if err := tg.RestartSteam(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sess.cmds) != 1 || sess.cmds[0] != "systemctl --user restart steam.service" {
		t.Fatalf("%v", sess.cmds)
	}
}

func TestDoneText(t *testing.T) {
	tg, _, _ := setup(t)
	if tg.Done() != "Trinity is in your headset's library under Non-Steam." {
		t.Fatalf("%q", tg.Done())
	}
}

func TestShellQuote(t *testing.T) {
	if shellQuote("10005062") != "10005062" || shellQuote("a b;rm") != "'a b;rm'" || shellQuote("it's") != `'it'\''s'` {
		t.Fatal("quoting")
	}
}
