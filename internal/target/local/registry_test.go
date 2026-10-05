package local

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ernie/trinity-installer/internal/release"
)

// fakeRegistry records HKEY_CURRENT_USER writes instead of touching the real registry.
type fakeRegistry struct {
	keys    map[string]map[string]any
	deletes []string
	failDel error
}

func newFakeRegistry() *fakeRegistry { return &fakeRegistry{keys: map[string]map[string]any{}} }

func init() { defaultRegistry = func() registryWriter { return newFakeRegistry() } }

func (r *fakeRegistry) set(key, name string, v any) error {
	if r.keys[key] == nil {
		r.keys[key] = map[string]any{}
	}
	r.keys[key][name] = v
	return nil
}
func (r *fakeRegistry) SetString(key, name, value string) error       { return r.set(key, name, value) }
func (r *fakeRegistry) SetDWORD(key, name string, value uint32) error { return r.set(key, name, value) }
func (r *fakeRegistry) GetString(key, name string) (string, error) {
	s, ok := r.keys[key][name].(string)
	if !ok {
		return "", fs.ErrNotExist
	}
	return s, nil
}
func (r *fakeRegistry) DeleteKey(key string) error {
	r.deletes = append(r.deletes, key)
	if r.failDel != nil {
		return r.failDel
	}
	if _, ok := r.keys[key]; !ok {
		return fs.ErrNotExist
	}
	delete(r.keys, key)
	return nil
}

// stubCommands makes every shell-out succeed without running.
func stubCommands(t *testing.T) {
	old := runCommand
	runCommand = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	t.Cleanup(func() { runCommand = old })
}

// fakeSelf points the uninstall.exe copy at a stand-in for the running installer; an empty body leaves it missing.
func fakeSelf(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "trinity-installer.exe")
	if body != "" {
		os.WriteFile(p, []byte(body), 0o755)
	}
	old := selfExe
	selfExe = func() (string, error) { return p, nil }
	t.Cleanup(func() { selfExe = old })
	return p
}

// pkgOf is a release package of the given files and sizes, as release.Open would hand it over.
func pkgOf(t *testing.T, tag string, files map[string]int) *release.Package {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, size := range files {
		f, _ := w.Create(name)
		f.Write(make([]byte, size))
	}
	w.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	p := &release.Package{Tag: tag}
	for _, f := range zr.File {
		p.Entries = append(p.Entries, release.Entry{Rel: f.Name, File: f})
	}
	return p
}

func readRecord(t *testing.T, dir string) installRecord {
	var rec installRecord
	b, err := os.ReadFile(filepath.Join(dir, recordName))
	if err != nil || json.Unmarshal(b, &rec) != nil {
		t.Fatalf("%s %v", b, err)
	}
	return rec
}

func writeRecord(t *testing.T, dir string, rec installRecord) {
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(dir, recordName), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallEntryWritten(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "installer")
	roaming := t.TempDir()
	t.Setenv("APPDATA", roaming)
	install := filepath.Join(t.TempDir(), "Trinity")
	reg := newFakeRegistry()
	tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install, StartMenu: true})
	tg.registry = reg
	ctx, log := context.Background(), func(string) {}
	if _, err := tg.PrepareDestination(ctx, log); err != nil {
		t.Fatal(err)
	}
	if err := tg.PushPackage(ctx, pkgOf(t, "v0.9.1", map[string]int{"trinity.exe": 3000}), log); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(install, "uninstall.exe")); err != nil || string(b) != "installer" {
		t.Fatalf("uninstall.exe %q %v", b, err)
	}
	// An install that stops at a later step must still leave a working Settings > Apps entry.
	if reg.keys[uninstallKey]["InstallLocation"] != install || reg.keys[uninstallKey]["UninstallString"] == nil {
		t.Fatalf("no entry once the files are in: %+v", reg.keys)
	}
	before := reg.keys[uninstallKey]["EstimatedSize"].(uint32)
	// A pushed pak counts; a file the installer did not write does not.
	os.WriteFile(filepath.Join(install, "baseq3", "pak0.pk3"), make([]byte, 5000), 0o644)
	tg.Pushed("baseq3/pak0.pk3")
	if after := reg.keys[uninstallKey]["EstimatedSize"].(uint32); after < before+4 {
		t.Fatalf("EstimatedSize %d after a 5000-byte pak, %d before", after, before)
	}
	os.WriteFile(filepath.Join(install, "notes.txt"), make([]byte, 100000), 0o644)
	if err := tg.RegisterLaunchEntry(ctx, log); err != nil {
		t.Fatal(err)
	}
	rec, _ := os.Stat(filepath.Join(install, recordName))
	size := uint32((3000 + 5000 + len("installer") + int(rec.Size()) + 1023) / 1024)
	want := map[string]any{
		"DisplayName":          "Trinity",
		"DisplayVersion":       "v0.9.1",
		"Publisher":            "Trinity",
		"DisplayIcon":          filepath.Join(install, "trinity.exe") + ",0",
		"InstallLocation":      install,
		"UninstallString":      `"` + filepath.Join(install, "uninstall.exe") + `" --uninstall`,
		"QuietUninstallString": `"` + filepath.Join(install, "uninstall.exe") + `" --uninstall --quiet`,
		"EstimatedSize":        size,
		"NoModify":             uint32(1),
		"NoRepair":             uint32(1),
	}
	if len(reg.keys) != 1 || !reflect.DeepEqual(reg.keys[uninstallKey], want) {
		t.Fatalf("%+v\nwant %+v", reg.keys, want)
	}
	if uninstallKey != `Software\Microsoft\Windows\CurrentVersion\Uninstall\Trinity` {
		t.Fatal(uninstallKey)
	}
	if err := tg.RegisterLaunchEntry(ctx, log); err != nil {
		t.Fatal(err)
	}
	if len(reg.keys) != 1 || !reflect.DeepEqual(reg.keys[uninstallKey], want) {
		t.Fatalf("a re-run must overwrite the one entry: %+v", reg.keys)
	}
	got := readRecord(t, install)
	wantRec := installRecord{
		InstallDir:        install,
		PaksDir:           install,
		CreatedInstallDir: true,
		Dirs:              []string{"baseq3", "missionpack"},
		Files:             []string{"baseq3/pak0.pk3", "trinity-install.json", "trinity.exe", "uninstall.exe"},
		StartMenu:         filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs", "Trinity.lnk"),
	}
	if !reflect.DeepEqual(got, wantRec) {
		t.Fatalf("%+v\nwant %+v", got, wantRec)
	}
}

func TestRecordNotesOnlyWhatTheInstallerCreated(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "installer")
	t.Setenv("APPDATA", t.TempDir())
	// An existing folder with its own baseq3, as when installing into a Quake III folder.
	install := t.TempDir()
	os.MkdirAll(filepath.Join(install, "baseq3"), 0o755)
	os.MkdirAll(filepath.Join(install, "docs"), 0o755)
	tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install})
	ctx, log := context.Background(), func(string) {}
	tg.PrepareDestination(ctx, log)
	if err := tg.PushPackage(ctx, pkgOf(t, "v1", map[string]int{"trinity.exe": 1, "docs/a.txt": 1, "lib/x/y.dll": 1}), log); err != nil {
		t.Fatal(err)
	}
	rec := readRecord(t, install)
	if rec.CreatedInstallDir || !reflect.DeepEqual(rec.Dirs, []string{"lib", "lib/x", "missionpack"}) {
		t.Fatalf("%+v", rec)
	}
	if !reflect.DeepEqual(rec.Files, []string{"docs/a.txt", "lib/x/y.dll", "trinity-install.json", "trinity.exe", "uninstall.exe"}) {
		t.Fatalf("%v", rec.Files)
	}
}

func TestReinstallMergesThePreviousRecord(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "installer")
	t.Setenv("APPDATA", t.TempDir())
	install := filepath.Join(t.TempDir(), "Trinity")
	steamRoot := t.TempDir()
	os.MkdirAll(filepath.Join(steamRoot, "config"), 0o755)
	user := filepath.Join(steamRoot, "userdata", "10005062")
	ctx, log := context.Background(), func(string) {}
	first := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot, SteamUser: user, SteamVRRoot: t.TempDir()})
	first.steamRunning = func() (bool, error) { return false, nil }
	first.steamVRRunning = func() (bool, error) { return false, nil }
	first.PrepareDestination(ctx, log)
	first.PushPackage(ctx, pkgOf(t, "v1", map[string]int{"trinity.exe": 1}), log)
	os.WriteFile(filepath.Join(install, "baseq3", "pak0.pk3"), []byte("p"), 0o644)
	first.Pushed("baseq3/pak0.pk3")
	if err := first.RegisterLaunchEntry(ctx, log); err != nil {
		t.Fatal(err)
	}
	if err := first.RegisterVR(ctx, first.appID, map[string][]byte{"capsule": {1}}, log); err != nil {
		t.Fatal(err)
	}
	// The second install skips Steam and finds the pak already there, so the runner reports nothing for it.
	second := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install})
	second.PrepareDestination(ctx, log)
	second.PushPackage(ctx, pkgOf(t, "v2", map[string]int{"trinity.exe": 1}), log)
	if err := second.RegisterLaunchEntry(ctx, log); err != nil {
		t.Fatal(err)
	}
	rec := readRecord(t, install)
	if rec.SteamRoot != steamRoot || rec.SteamUser != user || rec.AppID != first.appID || rec.AppID == 0 {
		t.Fatalf("the earlier Steam shortcut was forgotten: %+v", rec)
	}
	if !rec.CreatedInstallDir || !reflect.DeepEqual(rec.Files, []string{"baseq3/pak0.pk3", "trinity-capsule.png", "trinity-install.json", "trinity.exe", "trinity.vrmanifest", "uninstall.exe"}) {
		t.Fatalf("%+v", rec)
	}
}

func TestARecordFromAnotherFolderIsNotMerged(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "installer")
	install := t.TempDir()
	writeRecord(t, install, installRecord{InstallDir: filepath.Join(t.TempDir(), "Elsewhere"), Files: []string{"../../victim.txt"}, CreatedInstallDir: true})
	tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install})
	tg.PrepareDestination(context.Background(), func(string) {})
	tg.PushPackage(context.Background(), pkgOf(t, "v1", map[string]int{"trinity.exe": 1}), func(string) {})
	rec := readRecord(t, install)
	if rec.CreatedInstallDir || !reflect.DeepEqual(rec.Files, []string{"trinity-install.json", "trinity.exe", "uninstall.exe"}) {
		t.Fatalf("%+v", rec)
	}
}

func TestUnreadableInstallerSkipsUninstallExe(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "")
	t.Setenv("APPDATA", t.TempDir())
	install := t.TempDir()
	reg := newFakeRegistry()
	tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install})
	tg.registry = reg
	var lines []string
	log := func(l string) { lines = append(lines, l) }
	if err := tg.PushPackage(context.Background(), &release.Package{}, log); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(install, "uninstall.exe")); err == nil {
		t.Fatal("uninstall.exe written without a readable installer")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "uninstall.exe") {
		t.Fatalf("the skip went unlogged: %q", lines)
	}
	if rec := readRecord(t, install); strings.Contains(strings.Join(rec.Files, ","), "uninstall.exe") {
		t.Fatalf("%v", rec.Files)
	}
	// An entry whose uninstaller is missing would offer a Settings button that cannot work.
	if len(reg.keys) != 0 {
		t.Fatalf("%+v", reg.keys)
	}
	if err := tg.RegisterLaunchEntry(context.Background(), log); err != nil || len(reg.keys) != 0 {
		t.Fatalf("%v %+v", err, reg.keys)
	}
}

func TestNoUninstallerOffWindows(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "installer")
	for _, goos := range []string{"linux", "darwin"} {
		home := t.TempDir()
		install := filepath.Join(home, "trinity")
		reg := newFakeRegistry()
		tg := New(Options{GOOS: goos, InstallDir: install, PaksDir: install})
		tg.home = home
		tg.registry = reg
		tg.PrepareDestination(context.Background(), func(string) {})
		if err := tg.PushPackage(context.Background(), pkgOf(t, "v1", map[string]int{"trinity": 1}), func(string) {}); err != nil {
			t.Fatal(err)
		}
		tg.Pushed("baseq3/pak0.pk3")
		if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"uninstall.exe", recordName} {
			if _, err := os.Stat(filepath.Join(install, name)); err == nil {
				t.Fatalf("%s: %s written", goos, name)
			}
		}
		if len(reg.keys) != 0 {
			t.Fatalf("%s: %+v", goos, reg.keys)
		}
	}
}

func TestEstimatedSizeKB(t *testing.T) {
	a := t.TempDir()
	os.WriteFile(filepath.Join(a, "x"), make([]byte, 1500), 0o644)
	os.MkdirAll(filepath.Join(a, "sub"), 0o755)
	os.WriteFile(filepath.Join(a, "sub", "y"), make([]byte, 1000), 0o644)
	os.WriteFile(filepath.Join(a, "user.pk3"), make([]byte, 50000), 0o644)
	if got := EstimatedSizeKB(a, []string{"x", "sub/y", "missing", "../x"}); got != 3 {
		t.Fatalf("%d", got)
	}
}

func TestSetRegistryForTest(t *testing.T) {
	old := defaultRegistry
	defer func() { defaultRegistry = old }()
	SetRegistryForTest()
	r := defaultRegistry()
	if _, ok := r.(*memRegistry); !ok {
		t.Fatalf("%T", r)
	}
	r.SetString(uninstallKey, "InstallLocation", `C:\T`)
	if dir, err := InstalledDir(); err != nil || dir != `C:\T` {
		t.Fatalf("%q %v", dir, err)
	}
	if err := r.DeleteKey(uninstallKey); err != nil {
		t.Fatal(err)
	}
	if _, err := InstalledDir(); err == nil {
		t.Fatal("the deleted key still answers")
	}
}
