// Command winres writes the installer's Windows resources as a .syso that go build links in.
// fyne package would flatten the icon to one 256px image, which Explorer shrinks badly; this keeps every size in the .ico.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/josephspurrier/goversioninfo"
)

const name = "Trinity Installer"

func main() {
	icon := flag.String("icon", "assets/trinity.ico", "multi-size icon")
	version := flag.String("version", "0.0.0", "x.y.z version, without the v")
	out := flag.String("out", "cmd/trinity-installer/rsrc_windows_amd64.syso", "the .syso to write; its _windows_amd64 suffix keeps other builds from linking it")
	flag.Parse()
	if err := write(*icon, *version, *out); err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
}

func write(icon, version, out string) error {
	fv, err := goversioninfo.NewFileVersion(version)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "winres")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	// The same manifest fyne package writes, so the switch away from it changes nothing but the icon.
	manifest := filepath.Join(dir, "app.manifest")
	body := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0" xmlns:asmv3="urn:schemas-microsoft-com:asm.v3">
<assemblyIdentity version="` + fv.GetVersionString() + `" processorArchitecture="*" name="` + name + `" type="win32"/>
</assembly>
`
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		return err
	}
	vi := &goversioninfo.VersionInfo{IconPath: icon, ManifestPath: manifest}
	vi.ProductName = name
	vi.FileDescription = name
	vi.StringFileInfo.ProductVersion = fv.GetVersionString()
	vi.FixedFileInfo.FileVersion = fv
	vi.Build()
	vi.Walk()
	return vi.WriteSyso(out, "amd64")
}
