package patch

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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
	if r := s.Rels(); len(r) != 3 || r[0] != "baseq3/pak1.pk3" {
		t.Fatalf("%v", r)
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
