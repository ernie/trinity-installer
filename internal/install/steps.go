package install

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/steam"
)

const (
	compatTool  = "SteamLinuxRuntime_4-arm64"
	vrcmdPrefix = "cd /opt/steamvr/bin/linuxarm64 && /opt/steamvr/bin/vrenv.sh ./vrcmd "
	spaceMargin = 256 << 20
)

func defaultResponsePath() string {
	return fmt.Sprintf("/tmp/trinity-installer-%d/registered", time.Now().UnixNano())
}

func fetchRelease(ctx context.Context, st *state, log func(string)) error {
	b, err := st.opts.Fetch(ctx, log)
	if err != nil {
		return err
	}
	zr, err := release.OpenZip(b)
	if err != nil {
		return fmt.Errorf("the downloaded release is not a zip: %w", err)
	}
	if _, err := packageEntries(zr); err != nil {
		return err
	}
	st.zip = b
	log(fmt.Sprintf("release zip: %d bytes", len(b)))
	return nil
}

func prepareTitle(ctx context.Context, st *state, log func(string)) error {
	st.titleDir = path.Join(st.sess.Home(), "devkit-game", st.opts.GameID)
	if err := st.sess.MkdirAll(st.titleDir); err != nil {
		return err
	}
	argv, _ := json.Marshal([]string{"trinity"})
	settings, _ := json.MarshalIndent(map[string]string{"compat_tool": compatTool, "steam_play": "0"}, "", "  ")
	base := path.Join(st.sess.Home(), "devkit-game", st.opts.GameID)
	if err := st.sess.Put(ctx, base+"-argv.json", bytes.NewReader(argv), 0o644); err != nil {
		return err
	}
	if err := st.sess.Put(ctx, base+"-settings.json", bytes.NewReader(settings), 0o644); err != nil {
		return err
	}
	log("title dir " + st.titleDir)
	return nil
}

func packageMode(name string) os.FileMode {
	base := path.Base(name)
	if base == "trinity" || base == "trinity.ded" || strings.Contains(base, ".so") {
		return 0o755
	}
	return 0o644
}

type pkgEntry struct {
	file *zip.File
	rel  string
}

// packageEntries strips the release's shared top-level directory and checks the tree is safe and launchable.
func packageEntries(zr *zip.Reader) ([]pkgEntry, error) {
	var entries []pkgEntry
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		rel := path.Clean(name)
		if strings.HasPrefix(name, "/") || rel == ".." || strings.HasPrefix(rel, "../") {
			return nil, fmt.Errorf("the release zip has an unsafe entry %q", f.Name)
		}
		entries = append(entries, pkgEntry{file: f, rel: rel})
	}
	if len(entries) > 0 {
		first, _, _ := strings.Cut(entries[0].rel, "/")
		shared := true
		for _, e := range entries {
			head, rest, found := strings.Cut(e.rel, "/")
			if !found || head != first || rest == "" {
				shared = false
				break
			}
		}
		if shared {
			for i := range entries {
				entries[i].rel = strings.TrimPrefix(entries[i].rel, first+"/")
			}
		}
	}
	for _, e := range entries {
		if e.rel == "trinity" {
			return entries, nil
		}
	}
	return nil, errors.New("the release zip has no trinity binary at its root")
}

func pushPackage(ctx context.Context, st *state, log func(string)) error {
	zr, err := release.OpenZip(st.zip)
	if err != nil {
		return err
	}
	entries, err := packageEntries(zr)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := e.file.Open()
		if err != nil {
			return err
		}
		err = st.sess.Put(ctx, path.Join(st.titleDir, e.rel), sizedReader{rc, int64(e.file.UncompressedSize64)}, packageMode(e.rel))
		rc.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", e.rel, err)
		}
		log("pushed " + e.rel)
	}
	if err := st.sess.Put(ctx, path.Join(st.titleDir, "trinity-capsule.png"), bytes.NewReader(st.opts.Art["capsule"]), 0o644); err != nil {
		return err
	}
	return nil
}

func pushPaks(ctx context.Context, st *state, log func(string)) error {
	var todo []int
	var need int64
	for i, p := range st.opts.Paks {
		size, exists, err := st.sess.Stat(path.Join(st.titleDir, p.Rel))
		if err != nil {
			return err
		}
		if exists && size == p.Size {
			log("already there: " + p.Rel)
			continue
		}
		todo = append(todo, i)
		need += p.Size
	}
	if len(todo) == 0 {
		return nil
	}
	out, err := st.sess.Run(ctx, "df --output=avail -B1 "+st.titleDir)
	if err != nil {
		return fmt.Errorf("checking free space: %w", err)
	}
	fields := strings.Fields(out)
	var avail int64
	if len(fields) > 0 {
		avail, err = strconv.ParseInt(fields[len(fields)-1], 10, 64)
	}
	if len(fields) == 0 || err != nil {
		return fmt.Errorf("could not read the headset's free space: %s", strings.TrimSpace(out))
	}
	if avail < need+spaceMargin {
		return fmt.Errorf("the headset has %d MB free but the paks need %d MB", avail>>20, (need+spaceMargin)>>20)
	}
	for _, i := range todo {
		p := st.opts.Paks[i]
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := os.Open(p.Path)
		if err != nil {
			return err
		}
		err = st.sess.Put(ctx, path.Join(st.titleDir, p.Rel), &progressReader{r: f, total: p.Size, name: p.Rel, log: log}, 0o644)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", p.Rel, err)
		}
		log(fmt.Sprintf("pushed %s (%d MB)", p.Rel, p.Size>>20))
	}
	return nil
}

// sizedReader exposes the known size of a stream so sftp can write it concurrently.
type sizedReader struct {
	io.Reader
	size int64
}

func (s sizedReader) Size() int64 { return s.size }

// progressReader logs every 16 MB so a stalled upload is visible in the log.
type progressReader struct {
	r          io.Reader
	total      int64
	name       string
	log        func(string)
	done, last int64
}

func (p *progressReader) Size() int64 { return frame.SizeOf(p.r) }

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.done-p.last >= 16<<20 {
		p.last = p.done
		p.log(fmt.Sprintf("pushed %d of %d MB of %s", p.done>>20, p.total>>20, p.name))
	}
	return n, err
}

func registerShortcut(ctx context.Context, st *state, log func(string)) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := st.sess.Run(ctx, steam.RegisterShortcutScript(st.opts.GameID, st.opts.ResponsePath()))
	if err != nil {
		return err
	}
	if err := steam.ParseRegisterOutput(out); err != nil {
		return err
	}
	log("shortcut registered with Steam")
	return nil
}

func userID(ctx context.Context, st *state) (string, error) {
	if st.userID != "" {
		return st.userID, nil
	}
	out, err := st.sess.Run(ctx, "ls ~/.local/share/Steam/userdata")
	if err != nil {
		return "", err
	}
	var ids []string
	for _, f := range strings.Fields(out) {
		if f != "0" && f != "anonymous" {
			ids = append(ids, f)
		}
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("expected one Steam user on the headset, found %d: %s", len(ids), strings.Join(ids, ", "))
	}
	st.userID = ids[0]
	return st.userID, nil
}

func readAppID(ctx context.Context, st *state, log func(string)) error {
	uid, err := userID(ctx, st)
	if err != nil {
		return err
	}
	exe := path.Join(st.titleDir, "trinity")
	deadline := time.Now().Add(10 * pollInterval)
	var last error
	for {
		// Steam may not have written the file yet, or may be mid-write.
		out, err := st.sess.Run(ctx, "cat ~/.local/share/Steam/userdata/"+shellQuote(uid)+"/config/shortcuts.vdf")
		var shortcuts []steam.Shortcut
		if err == nil {
			shortcuts, err = steam.ParseShortcuts([]byte(out))
		}
		last = err
		if err == nil {
			if id, ok := steam.FindAppID(shortcuts, exe); ok {
				st.appID = id
				log(fmt.Sprintf("app id %d", id))
				return nil
			}
		}
		if time.Now().After(deadline) {
			if last != nil {
				return fmt.Errorf("Steam has no shortcut for %s yet; retry after Steam finishes registering: %w", st.titleDir, last)
			}
			return fmt.Errorf("Steam has no shortcut for %s yet; retry after Steam finishes registering", st.titleDir)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func registerManifest(ctx context.Context, st *state, log func(string)) error {
	manifest := path.Join(st.titleDir, "trinity.vrmanifest")
	if err := st.sess.Put(ctx, manifest, bytes.NewReader(steam.Manifest(st.titleDir, st.appID)), 0o644); err != nil {
		return err
	}
	if _, err := st.sess.Run(ctx, vrcmdPrefix+"--appmanifest "+manifest); err != nil {
		return fmt.Errorf("SteamVR did not accept the manifest (is SteamVR running on the headset?): %w", err)
	}
	out, err := st.sess.Run(ctx, vrcmdPrefix+"--dumpapps")
	if err != nil {
		return err
	}
	key := fmt.Sprintf("steam.app.%d", st.appID)
	if !strings.Contains(out, key) {
		return fmt.Errorf("SteamVR does not list %s after registration", key)
	}
	log("SteamVR knows " + key)
	return nil
}

func installArt(ctx context.Context, st *state, log func(string)) error {
	uid, err := userID(ctx, st)
	if err != nil {
		return err
	}
	grid := path.Join(st.sess.Home(), ".local/share/Steam/userdata", uid, "config/grid")
	for slot, name := range steam.GridFiles(st.appID) {
		png, ok := st.opts.Art[slot]
		if !ok {
			return fmt.Errorf("no artwork for %s", slot)
		}
		if err := st.sess.Put(ctx, path.Join(grid, name), bytes.NewReader(png), 0o644); err != nil {
			return err
		}
		log("installed " + name)
	}
	return nil
}

// pollInterval paces readAppID, which gives Steam ten intervals to write the shortcut.
var pollInterval = time.Second

// shellQuote leaves plain ids bare and single-quotes anything else.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_.-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
