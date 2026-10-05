package install

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/release"
)

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

func frameZip(t *testing.T) []byte { return zipOf(frameFiles...) }

func prefixed(prefix string) []byte {
	var names []string
	for _, n := range frameFiles {
		names = append(names, prefix+n)
	}
	return zipOf(names...)
}

func init() { pollInterval = time.Millisecond }

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

func options(t *testing.T, sess *fakeSession) Options {
	sess.Replies["df --output=avail"] = "Avail\n9000000000\n"
	sess.Replies["ls ~/.local/share/Steam/userdata"] = "10005062\n"
	sess.Replies["steam.pipe"] = "OK\n"
	sess.Replies["--dumpapps"] = "steam.app.2634369398 : Trinity : builtin\n"
	sess.Replies["cat ~/.local/share/Steam/userdata/10005062/config/shortcuts.vdf"] = string(shortcutsVDF("/home/steamos/devkit-game/Trinity/trinity", 2634369398))
	return Options{
		GameID:       "Trinity",
		Fetch:        func(ctx context.Context, log func(string)) ([]byte, error) { return frameZip(t), nil },
		Paks:         localPaks(t),
		Art:          map[string][]byte{"capsule": {1}, "wide": {2}, "hero": {3}, "logo": {4}, "icon": {5}},
		ResponsePath: func() string { return "/tmp/trinity-installer-1/registered" },
	}
}

func collect() (func(Progress), *[]Progress) {
	var all []Progress
	return func(p Progress) { all = append(all, p) }, &all
}

func TestRunHappyPath(t *testing.T) {
	sess := newFake()
	report, events := collect()
	if err := Run(context.Background(), sess, options(t, sess), 0, report); err != nil {
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
	if string(sess.files["/home/steamos/devkit-game/Trinity-argv.json"].data) != `["trinity"]` {
		t.Fatalf("argv %q", sess.files["/home/steamos/devkit-game/Trinity-argv.json"].data)
	}
	if !strings.Contains(string(sess.files["/home/steamos/devkit-game/Trinity-settings.json"].data), `"compat_tool": "SteamLinuxRuntime_4-arm64"`) {
		t.Fatalf("settings %q", sess.files["/home/steamos/devkit-game/Trinity-settings.json"].data)
	}
	if sess.ran("--appmanifest /home/steamos/devkit-game/Trinity/trinity.vrmanifest") != 1 || sess.ran("--dumpapps") != 1 {
		t.Fatalf("vrcmd calls: %v", sess.cmds)
	}
	last := (*events)[len(*events)-1]
	if last.Step != len(StepNames)-1 || last.State != Done {
		t.Fatalf("last event %+v", last)
	}
}

func TestPushPaksSkipsSameSizeAndNeverDeletes(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	sess.files["/home/steamos/devkit-game/Trinity/baseq3/pak0.pk3"] = fakeFile{data: []byte("pak baseq3/pak0.pk3"), mode: 0o644}
	sess.files["/home/steamos/devkit-game/Trinity/missionpack/pak1.pk3"] = fakeFile{data: []byte("kept"), mode: 0o644}
	opts.Paks = opts.Paks[:2] // user dropped Team Arena from the PC copy
	report, _ := collect()
	if err := Run(context.Background(), sess, opts, 0, report); err != nil {
		t.Fatal(err)
	}
	if string(sess.files["/home/steamos/devkit-game/Trinity/missionpack/pak1.pk3"].data) != "kept" {
		t.Fatal("headset pak deleted or replaced")
	}
	if _, ok := sess.files["/home/steamos/devkit-game/Trinity/missionpack/pak0.pk3"]; ok {
		t.Fatal("unselected pak pushed")
	}
}

func TestDiskSpaceGate(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	sess.Replies["df --output=avail"] = "Avail\n10\n"
	report, _ := collect()
	err := Run(context.Background(), sess, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != 3 || !strings.Contains(err.Error(), "free") {
		t.Fatalf("%v", err)
	}
	if _, ok := sess.files["/home/steamos/devkit-game/Trinity/baseq3/pak0.pk3"]; ok {
		t.Fatal("pak pushed despite the gate")
	}
}

func TestRetryFromFailedStep(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	sess.Replies["steam.pipe"] = "NOSTEAM\n"
	report, _ := collect()
	err := Run(context.Background(), sess, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != 4 || !strings.Contains(err.Error(), "Steam is not running") {
		t.Fatalf("%v", err)
	}
	sess.Replies["steam.pipe"] = "OK\n"
	fetches := sess.ran("df --output=avail")
	if err := Run(context.Background(), sess, opts, se.Step, report); err != nil {
		t.Fatal(err)
	}
	if sess.ran("df --output=avail") != fetches {
		t.Fatal("retry re-ran earlier steps")
	}
}

func TestUserIDRejectsSeveral(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	sess.Replies["ls ~/.local/share/Steam/userdata"] = "10005062\n20000001\n"
	report, _ := collect()
	err := Run(context.Background(), sess, opts, 0, report)
	if err == nil || !strings.Contains(err.Error(), "10005062") || !strings.Contains(err.Error(), "20000001") {
		t.Fatalf("%v", err)
	}
}

func TestAppIDMissing(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	sess.Replies["cat ~/.local/share/Steam/userdata/10005062/config/shortcuts.vdf"] = string(shortcutsVDF("/home/steamos/devkit-game/Other/trinity", 7))
	report, _ := collect()
	err := Run(context.Background(), sess, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != 5 {
		t.Fatalf("%v", err)
	}
}

func TestManifestNotListed(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	sess.Replies["--dumpapps"] = "steam.app.1 : Other : steam\n"
	report, _ := collect()
	err := Run(context.Background(), sess, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != 6 {
		t.Fatalf("%v", err)
	}
}

func TestRegisterWaitsForLock(t *testing.T) {
	// The script itself waits; here we pin that the Go side treats a lone "TIMEOUT" as failure, not success.
	sess := newFake()
	opts := options(t, sess)
	sess.Replies["steam.pipe"] = "TIMEOUT\n"
	report, _ := collect()
	err := Run(context.Background(), sess, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != 4 || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("%v", err)
	}
}

func TestPrefixedZipIsStripped(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	opts.Fetch = func(ctx context.Context, log func(string)) ([]byte, error) { return prefixed("frame-arm64/"), nil }
	report, _ := collect()
	if err := Run(context.Background(), sess, opts, 0, report); err != nil {
		t.Fatal(err)
	}
	files := sess.dump()
	for _, want := range []string{
		"/home/steamos/devkit-game/Trinity/trinity 755",
		"/home/steamos/devkit-game/Trinity/vrpreferences.json 644",
		"/home/steamos/devkit-game/Trinity/baseq3/pak8t.pk3 644",
	} {
		if !strings.Contains(files, want) {
			t.Fatalf("missing %q in\n%s", want, files)
		}
	}
	if strings.Contains(files, "frame-arm64") {
		t.Fatalf("prefix leaked:\n%s", files)
	}
}

func TestZipWithoutTrinityFailsAtFetch(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	opts.Fetch = func(ctx context.Context, log func(string)) ([]byte, error) {
		return zipOf("frame-arm64/other", "frame-arm64/baseq3/pak8t.pk3"), nil
	}
	report, _ := collect()
	err := Run(context.Background(), sess, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != 0 || !strings.Contains(err.Error(), "no trinity binary at its root") {
		t.Fatalf("%v", err)
	}
}

func TestZipWithEscapingEntryFails(t *testing.T) {
	for _, bad := range []string{"../x", "/etc/x", "a/../../x"} {
		sess := newFake()
		opts := options(t, sess)
		opts.Fetch = func(ctx context.Context, log func(string)) ([]byte, error) { return zipOf("trinity", bad), nil }
		report, _ := collect()
		err := Run(context.Background(), sess, opts, 0, report)
		var se *StepError
		if !errors.As(err, &se) || se.Step != 0 {
			t.Fatalf("%s: %v", bad, err)
		}
	}
}

func TestPushPaksCancelledWritesNothing(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	ctx, cancel := context.WithCancel(context.Background())
	report, _ := collect()
	st := &state{opts: opts, sess: sess, titleDir: "/home/steamos/devkit-game/Trinity"}
	cancel()
	if err := pushPaks(ctx, st, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if len(sess.files) != 0 {
		t.Fatalf("wrote despite cancel:\n%s", sess.dump())
	}
	err := Run(ctx, sess, opts, 3, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != 3 || len(sess.files) != 0 {
		t.Fatalf("%v", err)
	}
}

func TestAppIDAppearsAfterPolling(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	key := "cat ~/.local/share/Steam/userdata/10005062/config/shortcuts.vdf"
	delete(sess.Replies, key)
	sess.Seq[key] = []string{
		string(shortcutsVDF("/home/steamos/devkit-game/Other/trinity", 7)),
		string(shortcutsVDF("/home/steamos/devkit-game/Other/trinity", 7)),
		string(shortcutsVDF("/home/steamos/devkit-game/Trinity/trinity", 2634369398)),
	}
	report, _ := collect()
	if err := Run(context.Background(), sess, opts, 0, report); err != nil {
		t.Fatal(err)
	}
	if sess.ran(key) != 3 {
		t.Fatalf("polled %d times", sess.ran(key))
	}
}

func TestDFEmptyReplyIsAnError(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	for _, reply := range []string{"", "Avail\nnotanumber\n"} {
		sess.Replies["df --output=avail"] = reply
		report, _ := collect()
		err := Run(context.Background(), sess, opts, 0, report)
		var se *StepError
		if !errors.As(err, &se) || se.Step != 3 || !strings.Contains(err.Error(), "could not read the headset's free space") {
			t.Fatalf("%q: %v", reply, err)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if shellQuote("10005062") != "10005062" || shellQuote("a b;rm") != "'a b;rm'" || shellQuote("it's") != `'it'\''s'` {
		t.Fatal("quoting")
	}
}

func TestAppIDSurvivesCatAndParseErrors(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	key := "cat ~/.local/share/Steam/userdata/10005062/config/shortcuts.vdf"
	delete(sess.Replies, key)
	sess.FailOnce[key] = errors.New("no such file")
	sess.Seq[key] = []string{
		"garbage",
		string(shortcutsVDF("/home/steamos/devkit-game/Trinity/trinity", 2634369398)),
	}
	report, _ := collect()
	if err := Run(context.Background(), sess, opts, 0, report); err != nil {
		t.Fatal(err)
	}
	if sess.ran(key) != 3 {
		t.Fatalf("cat ran %d times", sess.ran(key))
	}
}

func TestAppIDPersistentErrorKeepsOriginalMessage(t *testing.T) {
	sess := newFake()
	opts := options(t, sess)
	key := "cat ~/.local/share/Steam/userdata/10005062/config/shortcuts.vdf"
	sess.Fail[key] = errors.New("no such file")
	report, _ := collect()
	err := Run(context.Background(), sess, opts, 5, report)
	if err == nil || !strings.Contains(err.Error(), "Steam has no shortcut for") || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("%v", err)
	}
}

func TestRetryOnNewSessionCarriesState(t *testing.T) {
	first := newFake()
	opts := options(t, first)
	opts.Carry = &Carry{}
	first.Replies["steam.pipe"] = "NOSTEAM\n"
	report, _ := collect()
	err := Run(context.Background(), first, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != 4 {
		t.Fatalf("%v", err)
	}
	second := newFake()
	options(t, second)
	if err := Run(context.Background(), second, opts, se.Step, report); err != nil {
		t.Fatalf("%v\n%s", err, second.dump())
	}
	if second.ran("df --output=avail") != 0 || len(second.files) == 0 {
		t.Fatalf("earlier steps re-ran on the new session: %v", second.cmds)
	}
}

func TestProgressReaderForwardsSize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, make([]byte, 4321), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pr := &progressReader{r: f, log: func(string) {}}
	if pr.Size() != 4321 || frame.SizeOf(pr) != 4321 {
		t.Fatalf("size %d", pr.Size())
	}
}

func TestZipEntryUploadReportsUncompressedSize(t *testing.T) {
	zr, err := release.OpenZip(zipOf("trinity", "data.bin"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := packageEntries(zr)
	if err != nil || len(entries) == 0 {
		t.Fatalf("%v", err)
	}
	for _, e := range entries {
		rc, err := e.file.Open()
		if err != nil {
			t.Fatal(err)
		}
		got := frame.SizeOf(sizedReader{rc, int64(e.file.UncompressedSize64)})
		rc.Close()
		if got != int64(e.file.UncompressedSize64) || got == 0 {
			t.Fatalf("%s: size %d", e.rel, got)
		}
	}
}
