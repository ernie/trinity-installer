package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (t *Target) installDMG(ctx context.Context, raw []byte, log func(string)) error {
	tmp, err := os.MkdirTemp("", "trinity-dmg")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	dmg := filepath.Join(tmp, "trinity.dmg")
	if err := os.WriteFile(dmg, raw, 0o644); err != nil {
		return err
	}
	mount := filepath.Join(tmp, "mnt")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		return err
	}
	if out, err := runCommand(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", mount, dmg); err != nil {
		return fmt.Errorf("mounting the disk image: %w: %s", err, out)
	}
	defer func() {
		// A volume left mounted is harmless to the install but should be visible in the log.
		if out, err := runCommand(context.Background(), "hdiutil", "detach", mount); err != nil {
			log(fmt.Sprintf("could not unmount the disk image at %s: %v: %s", mount, err, out))
		}
	}()
	// Copy beside the old app and swap, so a failed copy leaves the previous install working.
	dst := filepath.Join(t.opts.InstallDir, "Trinity.app")
	staged := dst + ".part"
	os.RemoveAll(staged)
	if out, err := runCommand(ctx, "ditto", filepath.Join(mount, "Trinity.app"), staged); err != nil {
		os.RemoveAll(staged)
		return fmt.Errorf("copying Trinity.app: %w: %s", err, out)
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.Rename(staged, dst); err != nil {
		return err
	}
	log("installed " + dst)
	return nil
}
