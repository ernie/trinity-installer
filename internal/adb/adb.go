package adb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Device struct {
	Serial string
	Model  string
	State  string
}

type ADB struct {
	path   string
	runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

var runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	return cmd.CombinedOutput()
}

// Fake returns an ADB whose commands go to run instead of a binary, so other packages can test against it.
func Fake(run func(ctx context.Context, name string, args ...string) ([]byte, error)) *ADB {
	return &ADB{path: "adb", runner: run}
}

// Bundled unpacks the embedded platform-tools adb, with its Apache notice, once per version into the config dir.
func Bundled(cfgDir string) (*ADB, error) {
	files, version, err := bundledFiles()
	if err != nil {
		return nil, err
	}
	exe, err := extract(filepath.Join(cfgDir, "adb", version), files)
	if err != nil {
		return nil, err
	}
	return &ADB{path: exe}, nil
}

// extract writes files into dir unless adb is already there; adb goes last because its presence marks the extraction complete.
func extract(dir string, files map[string][]byte) (string, error) {
	exe := filepath.Join(dir, adbName)
	if _, err := os.Stat(exe); err == nil {
		return exe, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		if name != adbName {
			names = append(names, name)
		}
	}
	for _, name := range append(names, adbName) {
		mode := os.FileMode(0o755)
		if name == "NOTICE.txt" {
			mode = 0o644
		}
		// A rename lands a whole file, so a run killed mid-write never leaves a truncated adb that later runs trust.
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p+".tmp", files[name], mode); err != nil {
			return "", err
		}
		if err := os.Rename(p+".tmp", p); err != nil {
			return "", err
		}
	}
	return exe, nil
}

func (a *ADB) run(ctx context.Context, serial string, args ...string) (string, error) {
	full := append([]string{"-s", serial}, args...)
	if serial == "" {
		full = args
	}
	run := runCommand
	if a.runner != nil {
		run = a.runner
	}
	out, err := run(ctx, a.path, full...)
	if err != nil {
		return string(out), fmt.Errorf("adb %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (a *ADB) Devices(ctx context.Context) ([]Device, error) {
	out, err := a.run(ctx, "", "devices", "-l")
	if err != nil {
		return nil, err
	}
	return ParseDevices(out), nil
}

// ParseDevices reads only what follows the header; adb prints server start and version notices before it.
func ParseDevices(out string) []Device {
	var devices []Device
	listed := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "List of devices attached") {
			listed = true
			continue
		}
		fields := strings.Fields(line)
		if !listed || len(fields) < 2 {
			continue
		}
		d := Device{Serial: fields[0], State: fields[1]}
		if d.State == "no" && len(fields) > 2 && fields[2] == "permissions" {
			d.State = "no permissions"
		}
		for _, f := range fields[2:] {
			if v, ok := strings.CutPrefix(f, "model:"); ok {
				d.Model = strings.ReplaceAll(v, "_", " ")
			}
		}
		devices = append(devices, d)
	}
	return devices
}

func (a *ADB) Install(ctx context.Context, serial, apk string) error {
	// -g grants the storage, microphone and eye tracking permissions up front, so the game never stops to ask in the headset.
	_, err := a.run(ctx, serial, "install", "-r", "-g", apk)
	return err
}

func (a *ADB) Push(ctx context.Context, serial, local, remote string) error {
	_, err := a.run(ctx, serial, "push", local, remote)
	return err
}

func (a *ADB) Shell(ctx context.Context, serial, cmd string) (string, error) {
	return a.run(ctx, serial, "shell", cmd)
}
