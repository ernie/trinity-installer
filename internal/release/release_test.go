package release

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeZip(t *testing.T) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("trinity")
	f.Write([]byte("binary"))
	w.Close()
	return buf.Bytes()
}

func server(t *testing.T, body []byte) *httptest.Server {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.40.0","assets":[{"name":"other.zip","size":1,"browser_download_url":"%s/other"},{"name":"trinity-frame-arm64.zip","size":%d,"browser_download_url":"%s/frame"}]}`, srv.URL, len(body), srv.URL)
	})
	mux.HandleFunc("/frame", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestPicksFrameAsset(t *testing.T) {
	body := fakeZip(t)
	srv := server(t, body)
	a, err := Latest(context.Background(), srv.Client(), srv.URL+"/latest", AssetName)
	if err != nil {
		t.Fatal(err)
	}
	if a.Tag != "v0.40.0" || a.Name != AssetName || a.Size != int64(len(body)) || a.URL != srv.URL+"/frame" {
		t.Fatalf("%+v", a)
	}
}

func TestDownloadChecksSizeAndOpens(t *testing.T) {
	body := fakeZip(t)
	srv := server(t, body)
	a, _ := Latest(context.Background(), srv.Client(), srv.URL+"/latest", AssetName)
	var seen int64
	b, err := Download(context.Background(), srv.Client(), a, func(done int64) { seen = done })
	if err != nil || seen != int64(len(body)) {
		t.Fatalf("err %v seen %d", err, seen)
	}
	zr, err := OpenZip(b)
	if err != nil || len(zr.File) != 1 || zr.File[0].Name != "trinity" {
		t.Fatalf("zip %v %v", err, zr)
	}
	a.Size++
	if _, err := Download(context.Background(), srv.Client(), a, nil); err == nil {
		t.Fatal("size mismatch accepted")
	}
}

func TestLatestWithoutFrameAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v1","assets":[]}`)
	}))
	defer srv.Close()
	if _, err := Latest(context.Background(), srv.Client(), srv.URL, AssetName); err == nil {
		t.Fatal("missing asset accepted")
	}
}
