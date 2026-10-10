package release

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func sumsLine(body []byte, name string) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:]) + "  " + name + "\n"
}

func server(t *testing.T, body []byte) *httptest.Server {
	return serverWithSums(t, body, sumsLine([]byte("other"), "other.zip")+sumsLine(body, FrameSpec().Asset))
}

func serverWithSums(t *testing.T, body []byte, sums string) *httptest.Server {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.40.0","assets":[{"name":"other.zip","size":1,"browser_download_url":"%s/other"},{"name":"trinity-frame-arm64.zip","size":%d,"browser_download_url":"%s/frame"},{"name":"sha256sums.txt","size":%d,"browser_download_url":"%s/sums"}]}`, srv.URL, len(body), srv.URL, len(sums), srv.URL)
	})
	mux.HandleFunc("/frame", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sums)) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestPicksFrameAsset(t *testing.T) {
	body := fakeZip(t)
	srv := server(t, body)
	a, err := Latest(context.Background(), srv.Client(), srv.URL+"/latest", FrameSpec().Asset)
	if err != nil {
		t.Fatal(err)
	}
	if a.Tag != "v0.40.0" || a.Name != FrameSpec().Asset || a.Size != int64(len(body)) || a.URL != srv.URL+"/frame" {
		t.Fatalf("%+v", a)
	}
}

func TestDownloadChecksSizeAndOpens(t *testing.T) {
	body := fakeZip(t)
	srv := server(t, body)
	a, _ := Latest(context.Background(), srv.Client(), srv.URL+"/latest", FrameSpec().Asset)
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
	if _, err := Latest(context.Background(), srv.Client(), srv.URL, FrameSpec().Asset); err == nil {
		t.Fatal("missing asset accepted")
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	body := fakeZip(t)
	srv := serverWithSums(t, body, sumsLine([]byte("tampered"), FrameSpec().Asset))
	a, err := Latest(context.Background(), srv.Client(), srv.URL+"/latest", FrameSpec().Asset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Download(context.Background(), srv.Client(), a, nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("mismatch accepted: %v", err)
	}
}

func TestDownloadRejectsMissingChecksumEntry(t *testing.T) {
	body := fakeZip(t)
	srv := serverWithSums(t, body, sumsLine(body, "other.zip"))
	a, _ := Latest(context.Background(), srv.Client(), srv.URL+"/latest", FrameSpec().Asset)
	if _, err := Download(context.Background(), srv.Client(), a, nil); err == nil {
		t.Fatal("asset without a checksum entry accepted")
	}
}

func TestLatestRequiresChecksums(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v1","assets":[{"name":"trinity-frame-arm64.zip","size":3,"browser_download_url":"%s/frame"}]}`, srv.URL)
	}))
	defer srv.Close()
	if _, err := Latest(context.Background(), srv.Client(), srv.URL, FrameSpec().Asset); err == nil {
		t.Fatal("release without sha256sums.txt accepted")
	}
}

func TestChecksumForAcceptsBinaryMarker(t *testing.T) {
	sums := "0000  other.zip\nabcd *trinity-frame-arm64.zip\n"
	if got, ok := checksumFor([]byte(sums), FrameSpec().Asset); !ok || got != "abcd" {
		t.Fatalf("%q %v", got, ok)
	}
	if _, ok := checksumFor([]byte(sums), "missing.zip"); ok {
		t.Fatal("found a missing entry")
	}
}
