// Command fetchadb downloads Android platform-tools and keeps adb, its Windows DLLs and NOTICE.txt for embedding.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var keep = map[string][]string{
	"windows": {"adb.exe", "AdbWinApi.dll", "AdbWinUsbApi.dll", "NOTICE.txt"},
	"darwin":  {"adb", "NOTICE.txt"},
	"linux":   {"adb", "NOTICE.txt"},
}

// The timeout keeps a stalled download from hanging CI.
var client = &http.Client{Timeout: 10 * time.Minute}

func main() {
	out := flag.String("out", "internal/adb/bin", "directory holding one folder per OS")
	flag.Parse()
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if err := fetch(goos, filepath.Join(*out, goos)); err != nil {
			fmt.Fprintf(os.Stderr, "fetchadb %s: %v\n", goos, err)
			os.Exit(1)
		}
	}
}

func fetch(goos, dir string) error {
	url := "https://dl.google.com/android/repository/platform-tools-latest-" + goos + ".zip"
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return err
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range keep[goos] {
		data, err := read(files, "platform-tools/"+name)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o755)
		if name == "NOTICE.txt" {
			mode = 0o644
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
			return err
		}
	}
	props, err := read(files, "platform-tools/source.properties")
	if err != nil {
		return err
	}
	version := revision(props)
	if version == "" {
		return fmt.Errorf("%s: no Pkg.Revision in source.properties", url)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: platform-tools %s\n", goos, version)
	return nil
}

func read(files map[string]*zip.File, name string) ([]byte, error) {
	f, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("the zip has no %s", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func revision(props []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(props))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok && strings.TrimSpace(k) == "Pkg.Revision" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
