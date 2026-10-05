package frametarget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/steam"
	"github.com/ernie/trinity-installer/internal/store"
	"github.com/ernie/trinity-installer/internal/target"
)

const (
	compatTool   = "SteamLinuxRuntime_4-arm64"
	vrcmdPrefix  = "cd /opt/steamvr/bin/linuxarm64 && /opt/steamvr/bin/vrenv.sh ./vrcmd "
	pollInterval = time.Second
)

type Connector func(ctx context.Context) (frame.Session, error)

type Target struct {
	sess      frame.Session
	reconnect Connector
	gameID    string
	titleDir  string
	userID    string
	// ResponsePath is overridden by tests; it names the devkit register response file.
	ResponsePath func() string
	// Poll is overridden by tests to shorten the app id wait.
	Poll time.Duration
}

func New(sess frame.Session, reconnect Connector, gameID string) *Target {
	return &Target{sess: sess, reconnect: reconnect, gameID: gameID, Poll: pollInterval,
		ResponsePath: func() string { return fmt.Sprintf("/tmp/trinity-installer-%d/registered", time.Now().UnixNano()) }}
}

func (t *Target) Session() frame.Session { return t.sess }
func (t *Target) Name() string           { return "Steam Frame" }
func (t *Target) Asset() release.Spec    { return release.FrameSpec() }
func (t *Target) Store() store.Store     { return store.SFTP(t.sess) }
func (t *Target) Done() string           { return "Trinity is in your headset's library under Non-Steam." }

func (t *Target) Applicable(context.Context) ([]target.Step, error) {
	return []target.Step{target.PushPatch, target.RegisterLaunchEntry, target.ReadAppID, target.RegisterVR, target.InstallArtwork}, nil
}

func (t *Target) Reconnect(ctx context.Context) error {
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, err := t.sess.Run(probe, "true")
	cancel()
	if err == nil {
		return nil
	}
	sess, err := t.reconnect(ctx)
	if err != nil {
		return fmt.Errorf("reconnecting to the headset: %w", err)
	}
	t.sess.Close()
	t.sess = sess
	return nil
}

// RestartSteam makes Steam reread its grid folder, which it otherwise only does at startup.
func (t *Target) RestartSteam(ctx context.Context) error {
	_, err := t.sess.Run(ctx, "systemctl --user restart steam.service")
	return err
}

func (t *Target) PrepareDestination(ctx context.Context, log func(string)) (string, error) {
	t.titleDir = path.Join(t.sess.Home(), "devkit-game", t.gameID)
	if err := t.sess.MkdirAll(t.titleDir); err != nil {
		return "", err
	}
	argv, _ := json.Marshal([]string{"trinity"})
	settings, _ := json.MarshalIndent(map[string]string{"compat_tool": compatTool, "steam_play": "0"}, "", "  ")
	base := path.Join(t.sess.Home(), "devkit-game", t.gameID)
	if err := t.sess.Put(ctx, base+"-argv.json", bytes.NewReader(argv), 0o644); err != nil {
		return "", err
	}
	if err := t.sess.Put(ctx, base+"-settings.json", bytes.NewReader(settings), 0o644); err != nil {
		return "", err
	}
	log("title dir " + t.titleDir)
	return t.titleDir, nil
}

func (t *Target) PushPackage(ctx context.Context, pkg *release.Package, log func(string)) error {
	s := t.Store()
	for _, e := range pkg.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := e.Open()
		if err != nil {
			return err
		}
		err = s.Put(ctx, path.Join(t.titleDir, e.Rel), rc, e.Size(), packageMode(e.Rel))
		rc.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", e.Rel, err)
		}
		log("pushed " + e.Rel)
	}
	return nil
}

func packageMode(name string) os.FileMode {
	base := path.Base(name)
	if base == "trinity" || base == "trinity.ded" || strings.Contains(base, ".so") {
		return 0o755
	}
	return 0o644
}

func (t *Target) RegisterLaunchEntry(ctx context.Context, log func(string)) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := t.sess.Run(ctx, steam.RegisterShortcutScript(t.gameID, t.ResponsePath()))
	if err != nil {
		return err
	}
	if err := steam.ParseRegisterOutput(out); err != nil {
		return err
	}
	log("shortcut registered with Steam")
	return nil
}

func (t *Target) steamUser(ctx context.Context) (string, error) {
	if t.userID != "" {
		return t.userID, nil
	}
	out, err := t.sess.Run(ctx, "ls ~/.local/share/Steam/userdata")
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
	t.userID = ids[0]
	return t.userID, nil
}

// ReadAppID gives Steam ten poll intervals to write the shortcut it just registered.
func (t *Target) ReadAppID(ctx context.Context, log func(string)) (uint32, error) {
	uid, err := t.steamUser(ctx)
	if err != nil {
		return 0, err
	}
	exe := path.Join(t.titleDir, "trinity")
	deadline := time.Now().Add(10 * t.Poll)
	var last error
	for {
		// Steam may not have written the file yet, or may be mid-write.
		out, err := t.sess.Run(ctx, "cat ~/.local/share/Steam/userdata/"+shellQuote(uid)+"/config/shortcuts.vdf")
		var shortcuts []steam.Shortcut
		if err == nil {
			shortcuts, err = steam.ParseShortcuts([]byte(out))
		}
		last = err
		if err == nil {
			if id, ok := steam.FindAppID(shortcuts, exe); ok {
				log(fmt.Sprintf("app id %d", id))
				return id, nil
			}
		}
		if time.Now().After(deadline) {
			if last != nil {
				return 0, fmt.Errorf("Steam has no shortcut for %s yet; retry after Steam finishes registering: %w", t.titleDir, last)
			}
			return 0, fmt.Errorf("Steam has no shortcut for %s yet; retry after Steam finishes registering", t.titleDir)
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(t.Poll):
		}
	}
}

func (t *Target) RegisterVR(ctx context.Context, appID uint32, art map[string][]byte, log func(string)) error {
	// The manifest's image_path names the capsule, so it must be in place before SteamVR reads the manifest.
	capsule, ok := art["capsule"]
	if !ok {
		return errors.New("no artwork for capsule")
	}
	if err := t.sess.Put(ctx, path.Join(t.titleDir, "trinity-capsule.png"), bytes.NewReader(capsule), 0o644); err != nil {
		return err
	}
	manifest := path.Join(t.titleDir, "trinity.vrmanifest")
	if err := t.sess.Put(ctx, manifest, bytes.NewReader(steam.Manifest(t.titleDir, "trinity", appID, "binary_path_linux_arm")), 0o644); err != nil {
		return err
	}
	if _, err := t.sess.Run(ctx, vrcmdPrefix+"--appmanifest "+manifest); err != nil {
		return fmt.Errorf("SteamVR did not accept the manifest (is SteamVR running on the headset?): %w", err)
	}
	out, err := t.sess.Run(ctx, vrcmdPrefix+"--dumpapps")
	if err != nil {
		return err
	}
	key := fmt.Sprintf("steam.app.%d", appID)
	if !strings.Contains(out, key) {
		return fmt.Errorf("SteamVR does not list %s after registration", key)
	}
	log("SteamVR knows " + key)
	return nil
}

func (t *Target) InstallArtwork(ctx context.Context, appID uint32, art map[string][]byte, log func(string)) error {
	uid, err := t.steamUser(ctx)
	if err != nil {
		return err
	}
	grid := path.Join(t.sess.Home(), ".local/share/Steam/userdata", uid, "config/grid")
	for slot, name := range steam.GridFiles(appID) {
		png, ok := art[slot]
		if !ok {
			return fmt.Errorf("no artwork for %s", slot)
		}
		if err := t.sess.Put(ctx, path.Join(grid, name), bytes.NewReader(png), 0o644); err != nil {
			return err
		}
		log("installed " + name)
	}
	return nil
}

// shellQuote leaves plain ids bare and single-quotes anything else.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_.-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
