package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ernie/trinity-installer/internal/steam"
)

const recordName = "trinity-install.json"

// installRecord lists what the installer wrote, so the uninstaller removes exactly that and needs no detection.
type installRecord struct {
	InstallDir        string   `json:"installDir"`
	PaksDir           string   `json:"paksDir"`
	CreatedInstallDir bool     `json:"createdInstallDir"`
	Dirs              []string `json:"dirs"`  // folders the installer created, relative to InstallDir
	Files             []string `json:"files"` // files the installer wrote, relative to InstallDir
	SteamRoot         string   `json:"steamRoot"`
	SteamUser         string   `json:"steamUser"`
	AppID             uint32   `json:"appID"`
	StartMenu         string   `json:"startMenu"`
}

// loadRecord starts from an earlier install's record in the same folder, so a re-install keeps what that one wrote.
func (t *Target) loadRecord() {
	t.rec = installRecord{}
	var prev installRecord
	b, err := os.ReadFile(filepath.Join(t.opts.InstallDir, recordName))
	if err == nil && json.Unmarshal(b, &prev) == nil && samePath(prev.InstallDir, t.opts.InstallDir) {
		t.rec = prev
	}
}

func (t *Target) relToInstall(p string) (string, bool) {
	r, err := filepath.Rel(t.opts.InstallDir, p)
	if err != nil || r != "." && !filepath.IsLocal(r) {
		return "", false
	}
	return filepath.ToSlash(r), true
}

func addUnique(list []string, s string) []string {
	for _, have := range list {
		if have == s {
			return list
		}
	}
	return append(list, s)
}

func (t *Target) recordFile(p string) {
	if rel, ok := t.relToInstall(p); ok && rel != "." {
		t.rec.Files = addUnique(t.rec.Files, rel)
	}
}

func (t *Target) recordDir(p string) {
	if rel, ok := t.relToInstall(p); ok && rel != "." {
		t.rec.Dirs = addUnique(t.rec.Dirs, rel)
	}
}

// noteNewDirs records the folders below InstallDir that writing into dir is about to create.
func (t *Target) noteNewDirs(dir string) {
	for d := dir; ; d = filepath.Dir(d) {
		rel, ok := t.relToInstall(d)
		if !ok || rel == "." {
			return
		}
		if _, err := os.Stat(d); !errors.Is(err, fs.ErrNotExist) {
			return
		}
		t.recordDir(d)
	}
}

// Pushed records a pak the runner wrote; one it found already in place stays the user's.
func (t *Target) Pushed(rel string) {
	t.recordFile(filepath.Join(t.opts.PaksDir, filepath.FromSlash(rel)))
	// Best effort: RegisterLaunchEntry saves the record again and reports a failure there.
	t.saveRecord(context.Background())
}

func (t *Target) saveRecord(ctx context.Context) error {
	if t.opts.GOOS != "windows" {
		return nil
	}
	t.rec.InstallDir, t.rec.PaksDir = t.opts.InstallDir, t.opts.PaksDir
	t.rec.Files = addUnique(t.rec.Files, recordName)
	sort.Strings(t.rec.Files)
	sort.Strings(t.rec.Dirs)
	b, err := json.MarshalIndent(t.rec, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(ctx, filepath.Join(t.opts.InstallDir, recordName), b)
}

// selfExe is a variable so tests copy a stand-in instead of the test binary.
var selfExe = os.Executable

func (t *Target) copyUninstaller(ctx context.Context, log func(string)) error {
	dst := filepath.Join(t.opts.InstallDir, "uninstall.exe")
	self, err := selfExe()
	var b []byte
	if err == nil {
		if same(self, dst) {
			t.recordFile(dst)
			log("uninstall.exe is the running installer; left in place")
			return nil
		}
		b, err = os.ReadFile(self)
	}
	if err != nil {
		log("uninstall.exe not written; the running installer cannot be read: " + err.Error())
		return nil
	}
	if err := writeAtomic(ctx, dst, b); err != nil {
		return err
	}
	t.recordFile(dst)
	log("installed uninstall.exe")
	return nil
}

func same(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	return err == nil && os.SameFile(sa, sb)
}

// registerUninstall runs after the paks are in, so EstimatedSize counts them.
func (t *Target) registerUninstall(ctx context.Context, log func(string)) error {
	dir := t.opts.InstallDir
	uninst := filepath.Join(dir, "uninstall.exe")
	if _, err := os.Stat(uninst); err != nil {
		log("no uninstall.exe, so no Settings > Apps entry")
		return nil
	}
	programs, err := t.startMenu()
	if err != nil {
		return err
	}
	t.rec.StartMenu = filepath.Join(programs, "Trinity.lnk")
	// Without Steam this time, an earlier install's shortcut is still there to remove.
	if t.steamShortcut() {
		t.rec.SteamRoot, t.rec.SteamUser, t.rec.AppID = t.opts.SteamRoot, t.opts.SteamUser, t.appID
	}
	if err := t.saveRecord(ctx); err != nil {
		return err
	}
	for _, v := range [][2]string{
		{"DisplayName", "Trinity"},
		{"DisplayVersion", t.tag},
		{"Publisher", "Trinity"},
		{"DisplayIcon", t.exe() + ",0"},
		{"InstallLocation", dir},
		{"UninstallString", `"` + uninst + `" --uninstall`},
		{"QuietUninstallString", `"` + uninst + `" --uninstall --quiet`},
	} {
		if err := t.registry.SetString(uninstallKey, v[0], v[1]); err != nil {
			return fmt.Errorf("writing the uninstall entry: %w", err)
		}
	}
	for name, v := range map[string]uint32{"EstimatedSize": EstimatedSizeKB(dir, t.rec.Files), "NoModify": 1, "NoRepair": 1} {
		if err := t.registry.SetDWORD(uninstallKey, name, v); err != nil {
			return fmt.Errorf("writing the uninstall entry: %w", err)
		}
	}
	log("uninstall entry written for Settings > Apps")
	return nil
}

// EstimatedSizeKB totals the recorded files under dir; files the installer did not write are not Trinity's size.
func EstimatedSizeKB(dir string, files []string) uint32 {
	var total int64
	for _, rel := range files {
		r := filepath.FromSlash(rel)
		if !filepath.IsLocal(r) {
			continue
		}
		if info, err := os.Lstat(filepath.Join(dir, r)); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return uint32((total + 1023) / 1024)
}

type UninstallOptions struct {
	InstallDir     string
	DeleteSettings bool
}

// ErrSteamRunning and ErrTrinityRunning mark refusals the user clears by closing the program and trying again.
var (
	ErrSteamRunning   = errors.New("Close Steam, then press Retry.")
	ErrTrinityRunning = errors.New("Close Trinity, then uninstall again.")
)

type uninstaller struct {
	registry       registryWriter
	trinityRunning func() (bool, error)
	steamRunning   func() (bool, error)
	steamVRRunning func() (bool, error)
	self           func() (string, error)
	detach         func(cmdLine, dir string) error
}

// Uninstall removes what the Windows install in opts.InstallDir recorded, logging each step and returning every failure; a running Trinity or Steam refuses it before anything is removed.
func Uninstall(ctx context.Context, opts UninstallOptions, log func(string)) []error {
	u := &uninstaller{
		registry:       defaultRegistry(),
		trinityRunning: func() (bool, error) { return processRunning("windows", "trinity") },
		steamRunning:   func() (bool, error) { return processRunning("windows", "steam") },
		steamVRRunning: func() (bool, error) { return processRunning("windows", "vrserver") },
		self:           os.Executable,
		detach:         startDetached,
	}
	return u.run(ctx, opts, log)
}

func (u *uninstaller) run(ctx context.Context, opts UninstallOptions, log func(string)) []error {
	dir := opts.InstallDir
	refuse := func(err error) []error {
		log(err.Error())
		return []error{err}
	}
	// A relative folder would resolve against wherever Settings started the uninstaller.
	if !filepath.IsAbs(dir) {
		return refuse(fmt.Errorf("the recorded install folder %q is not a full path", dir))
	}
	var rec installRecord
	b, recErr := os.ReadFile(filepath.Join(dir, recordName))
	if recErr == nil {
		recErr = json.Unmarshal(b, &rec)
	}
	if recErr == nil && !samePath(rec.InstallDir, dir) {
		return refuse(fmt.Errorf("the install record in %s is for %s; nothing was removed", dir, rec.InstallDir))
	}
	steamOK := rec.SteamRoot != "" && isSteamRoot(rec.SteamRoot)
	if err := u.closed(steamOK); err != nil {
		return refuse(err)
	}
	var errs []error
	fail := func(what string, err error) {
		err = fmt.Errorf("%s: %w", what, err)
		log(err.Error())
		errs = append(errs, err)
	}
	if recErr != nil {
		fail("reading "+recordName, recErr)
	}
	if rec.SteamRoot != "" && !steamOK {
		fail("skipping the Steam steps", fmt.Errorf("%s does not look like a Steam folder", rec.SteamRoot))
	}
	if steamOK && rec.SteamUser != "" {
		if !isSteamUser(rec.SteamRoot, rec.SteamUser) {
			fail("skipping the Steam shortcut and artwork", fmt.Errorf("%s is not a user folder under %s", rec.SteamUser, filepath.Join(rec.SteamRoot, "userdata")))
		} else {
			if err := removeShortcut(ctx, rec.SteamUser, filepath.Join(dir, "trinity.exe"), log); err != nil {
				fail("removing the Steam shortcut", err)
			}
			if rec.AppID != 0 {
				for _, name := range steam.GridFiles(rec.AppID) {
					if err := os.Remove(filepath.Join(rec.SteamUser, "config", "grid", name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
						fail("removing Steam artwork", err)
					}
				}
				log("Steam artwork removed")
			}
		}
	}
	if steamOK {
		if err := removeManifest(ctx, rec.SteamRoot, filepath.Join(dir, "trinity.vrmanifest"), log); err != nil {
			fail("unlisting the SteamVR manifest", err)
		}
	}
	if lnk := startMenuLink(rec.StartMenu, log); lnk != "" {
		if err := os.Remove(lnk); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fail("removing "+lnk, err)
		} else {
			log("Start Menu shortcut removed")
		}
	}
	if recErr == nil {
		u.removeInstalled(dir, rec, log, fail)
	} else {
		fail("leaving "+dir+" in place", fmt.Errorf("without %s the uninstaller cannot tell Trinity's files from yours", recordName))
		u.deleteKey(log, fail)
	}
	if opts.DeleteSettings {
		roaming, err := roamingDir(os.UserHomeDir)
		if err == nil {
			err = os.RemoveAll(filepath.Join(roaming, "Trinity"))
		}
		if err != nil {
			fail("removing your settings", err)
		} else {
			log("settings and downloads removed")
		}
	}
	return errs
}

// removeInstalled removes the recorded files and folders; the record and the entry go only once every file has, so a locked file can be retried.
func (u *uninstaller) removeInstalled(dir string, rec installRecord, log func(string), fail func(string, error)) {
	self, _ := u.self()
	selfInside := false
	remaining := 0
	for _, rel := range rec.Files {
		if rel == recordName {
			continue
		}
		p, err := installedPath(dir, rel)
		if err != nil {
			fail("skipping "+rel, err)
			continue
		}
		if self != "" && samePath(p, self) {
			selfInside = true
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fail("removing "+rel, err)
			remaining++
		}
	}
	dirs := append([]string(nil), rec.Dirs...)
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		return di > dj || di == dj && dirs[i] > dirs[j]
	})
	for _, rel := range dirs {
		p, err := installedPath(dir, rel)
		if err != nil {
			fail("skipping "+rel, err)
			continue
		}
		removeIfEmpty(p, log)
	}
	if remaining > 0 {
		fail("keeping the uninstall entry", fmt.Errorf("%d installed files could not be removed; close any program using them, then uninstall again", remaining))
		return
	}
	if err := os.Remove(filepath.Join(dir, recordName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fail("removing "+recordName, err)
		return
	}
	log("removed the installed files")
	u.deleteKey(log, fail)
	switch {
	case selfInside && strings.ContainsRune(self+dir, '%'):
		fail("leaving "+self, errors.New("cmd.exe would read the % in its path as a variable; delete it yourself"))
	case selfInside:
		if err := u.detach(selfDeleteCommand(self, dir, rec.CreatedInstallDir), os.TempDir()); err != nil {
			fail("scheduling the removal of "+self, err)
		} else {
			log("uninstall.exe goes once this window closes")
		}
	case rec.CreatedInstallDir:
		removeIfEmpty(dir, log)
	}
}

func (u *uninstaller) deleteKey(log func(string), fail func(string, error)) {
	if err := u.registry.DeleteKey(uninstallKey); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fail("removing the uninstall entry", err)
	} else {
		log("uninstall entry removed")
	}
}

func (u *uninstaller) closed(steamToo bool) error {
	type check struct {
		name    string
		running func() (bool, error)
		then    error
	}
	checks := []check{{"Trinity", u.trinityRunning, ErrTrinityRunning}}
	if steamToo {
		// Steam rewrites shortcuts.vdf from memory when it exits, so a removal now would come back.
		checks = append(checks, check{"Steam", u.steamRunning, ErrSteamRunning}, check{"SteamVR", u.steamVRRunning, ErrSteamRunning})
	}
	for _, c := range checks {
		on, err := c.running()
		if err != nil {
			return fmt.Errorf("could not tell whether %s is running: %v. %w", c.name, err, c.then)
		}
		if on {
			return fmt.Errorf("%s is running. %w", c.name, c.then)
		}
	}
	return nil
}

func isLink(fi fs.FileInfo) bool { return fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 }

// installedPath resolves a recorded path inside dir, refusing one that leaves dir or passes through a link or junction.
func installedPath(dir, rel string) (string, error) {
	r := filepath.FromSlash(rel)
	if !filepath.IsLocal(r) {
		return "", errors.New("it is not inside the install folder")
	}
	parts := strings.Split(r, string(filepath.Separator))
	cur := dir
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			break
		}
		if isLink(fi) {
			return "", fmt.Errorf("it is reached through the link %s", cur)
		}
	}
	return filepath.Join(dir, r), nil
}

func removeIfEmpty(p string, log func(string)) {
	fi, err := os.Lstat(p)
	if err != nil {
		return
	}
	if isLink(fi) || !fi.IsDir() {
		log("left " + p + ": it is not a folder the installer made")
		return
	}
	if entries, err := os.ReadDir(p); err != nil || len(entries) > 0 {
		log("left " + p + ": it holds files the installer did not put there")
		return
	}
	if err := os.Remove(p); err != nil {
		log("left " + p + ": " + err.Error())
	}
}

// startMenuLink trusts a recorded path only when it is Trinity.lnk in a Start Menu Programs folder.
func startMenuLink(recorded string, log func(string)) string {
	if recorded != "" && filepath.IsAbs(recorded) && strings.EqualFold(filepath.Base(recorded), "Trinity.lnk") &&
		strings.HasSuffix(strings.ToLower(filepath.ToSlash(filepath.Dir(recorded))), "start menu/programs") {
		return recorded
	}
	if recorded != "" {
		log("ignoring the recorded Start Menu path " + recorded)
	}
	roaming, err := roamingDir(os.UserHomeDir)
	if err != nil {
		return ""
	}
	return filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs", "Trinity.lnk")
}

func isSteamRoot(root string) bool {
	if !filepath.IsAbs(root) {
		return false
	}
	if fi, err := os.Stat(filepath.Join(root, "steam.exe")); err == nil && fi.Mode().IsRegular() {
		return true
	}
	fi, err := os.Stat(filepath.Join(root, "config"))
	return err == nil && fi.IsDir()
}

func isSteamUser(root, user string) bool {
	id := filepath.Base(user)
	if id == "" || strings.Trim(id, "0123456789") != "" {
		return false
	}
	return samePath(filepath.Dir(user), filepath.Join(root, "userdata"))
}

// selfDeleteCommand retries once a second for ten minutes, since Windows cannot delete the exe until the uninstaller exits.
func selfDeleteCommand(exe, dir string, rmdir bool) string {
	done := "(exit)"
	if rmdir {
		done = fmt.Sprintf(`(rmdir "%s" 2>nul & exit)`, dir)
	}
	return fmt.Sprintf(`cmd /d /c for /l %%i in (1,1,600) do @(ping -n 2 127.0.0.1 >nul & del /f /q "%s" 2>nul & if not exist "%s" %s)`, exe, exe, done)
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func removeShortcut(ctx context.Context, user, exe string, log func(string)) error {
	p := filepath.Join(user, "config", "shortcuts.vdf")
	old, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		log("no Steam shortcuts file")
		return nil
	}
	if err != nil {
		return err
	}
	out, removed, err := steam.RemoveShortcut(old, exe)
	if err != nil {
		return err
	}
	if !removed {
		log("no Steam shortcut to remove")
		return nil
	}
	if err := writeAtomic(ctx, p, out); err != nil {
		return err
	}
	log("Steam shortcut removed")
	return nil
}

// removeManifest is listManifest's inverse; it leaves a file it cannot parse untouched.
func removeManifest(ctx context.Context, steamRoot, manifest string, log func(string)) error {
	cfg := filepath.Join(steamRoot, "config", "appconfig.json")
	b, err := os.ReadFile(cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("SteamVR's %s is not readable JSON: %w", cfg, err)
	}
	var paths []string
	if raw, ok := doc["manifest_paths"]; ok {
		if err := json.Unmarshal(raw, &paths); err != nil {
			return fmt.Errorf("SteamVR's %s has unexpected manifest_paths: %w", cfg, err)
		}
	}
	kept := []string{}
	for _, p := range paths {
		if p != manifest && !(runtime.GOOS == "windows" && strings.EqualFold(p, manifest)) {
			kept = append(kept, p)
		}
	}
	if len(kept) == len(paths) {
		log("manifest not listed for SteamVR")
		return nil
	}
	doc["manifest_paths"], _ = json.Marshal(kept)
	out, err := json.MarshalIndent(doc, "", "   ")
	if err != nil {
		return err
	}
	if err := writeAtomic(ctx, cfg, out); err != nil {
		return err
	}
	log("manifest unlisted for SteamVR")
	return nil
}
