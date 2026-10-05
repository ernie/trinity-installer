package release

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

type Entry struct {
	Rel  string
	File *zip.File
}

func (e Entry) Open() (io.ReadCloser, error) { return e.File.Open() }
func (e Entry) Size() int64                  { return int64(e.File.UncompressedSize64) }

type Package struct {
	Spec    Spec
	Raw     []byte
	Entries []Entry
}

// Open validates a downloaded asset; zips get their shared top-level directory stripped, like the engine's updater does.
func Open(spec Spec, raw []byte) (*Package, error) {
	p := &Package{Spec: spec, Raw: raw}
	if spec.Kind != KindZip {
		return p, nil
	}
	zr, err := OpenZip(raw)
	if err != nil {
		return nil, fmt.Errorf("the downloaded release is not a zip: %w", err)
	}
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			continue
		}
		rel := path.Clean(name)
		// Windows reads a colon in the first segment as a drive or stream name, never a folder inside the install dir.
		head, _, _ := strings.Cut(rel, "/")
		if strings.HasPrefix(name, "/") || rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(head, ":") {
			return nil, fmt.Errorf("the release zip has an unsafe entry %q", f.Name)
		}
		p.Entries = append(p.Entries, Entry{Rel: rel, File: f})
	}
	if len(p.Entries) > 0 {
		first, _, _ := strings.Cut(p.Entries[0].Rel, "/")
		shared := true
		for _, e := range p.Entries {
			head, rest, found := strings.Cut(e.Rel, "/")
			if !found || head != first || rest == "" {
				shared = false
				break
			}
		}
		// A shared directory that is the spec's own root directory is content, not a wrapper.
		if rootDir, _, ok := strings.Cut(spec.Root, "/"); ok && rootDir == first {
			shared = false
		}
		if shared {
			for i := range p.Entries {
				p.Entries[i].Rel = strings.TrimPrefix(p.Entries[i].Rel, first+"/")
			}
		}
	}
	for _, e := range p.Entries {
		if e.Rel == spec.Root {
			return p, nil
		}
	}
	return nil, errors.New("the release zip has no " + spec.Root + " at its root")
}
