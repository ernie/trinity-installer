package patch

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func patchZip(wrapper string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range []string{"baseq3/pak1.pk3", "baseq3/pak2.pk3", "missionpack/pak1.pk3"} {
		f, _ := w.Create(wrapper + n)
		f.Write([]byte("pak " + n))
	}
	w.Close()
	return buf.Bytes()
}

func TestFetchEULAAndZip(t *testing.T) {
	body := patchZip("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/quake3-eula.txt":
			w.Write([]byte("LICENSE TEXT"))
		case "/downloads/quake3-1.32-pk3s.zip":
			w.Write(body)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	text, err := FetchEULA(context.Background(), srv.Client(), srv.URL+"/quake3-eula.txt")
	if err != nil || text != "LICENSE TEXT" {
		t.Fatalf("%q %v", text, err)
	}
	var last int64
	raw, err := FetchZip(context.Background(), srv.Client(), srv.URL+"/downloads/quake3-1.32-pk3s.zip", func(done, total int64) { last = done })
	if err != nil || last != int64(len(body)) {
		t.Fatalf("%v %d", err, last)
	}
	s, err := OpenSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := s.Entry("missionpack/pak1.pk3"); !ok || e.Size() != int64(len("pak missionpack/pak1.pk3")) {
		t.Fatalf("%+v %v", e, ok)
	}
	if _, err := FetchEULA(context.Background(), srv.Client(), srv.URL+"/missing"); err == nil {
		t.Fatal("404 accepted")
	}
}

func TestOpenSetStripsWrapperAndRequiresPak1(t *testing.T) {
	s, err := OpenSet(patchZip("quake3-1.32-pk3s/"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"baseq3/pak1.pk3", "baseq3/pak2.pk3", "missionpack/pak1.pk3"} {
		if _, ok := s.Entry(rel); !ok {
			t.Fatalf("%s missing after the wrapper strip", rel)
		}
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("readme.txt")
	f.Write([]byte("x"))
	w.Close()
	if _, err := OpenSet(buf.Bytes()); err == nil {
		t.Fatal("zip without the patch accepted")
	}
}

func TestFetchZipRejectsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	if _, err := FetchZip(context.Background(), srv.Client(), srv.URL, nil); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("%v", err)
	}
}

func TestFetchEULARefusesACutOffLicense(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := 1 << 20
		if r.URL.Path == "/over" {
			size++
		}
		w.Write(bytes.Repeat([]byte("x"), size))
	}))
	defer srv.Close()
	if text, err := FetchEULA(context.Background(), srv.Client(), srv.URL+"/full"); err != nil || len(text) != 1<<20 {
		t.Fatalf("a 1 MiB license: %d %v", len(text), err)
	}
	// Showing a truncated license would let "I have read" unlock on text that is not the whole license.
	if _, err := FetchEULA(context.Background(), srv.Client(), srv.URL+"/over"); err == nil {
		t.Fatal("a license over 1 MiB was cut off silently")
	}
}
