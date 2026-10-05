package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/ernie/trinity-installer/internal/release"
)

func TestFetchReturnsTheReleaseTag(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			w.Write([]byte("zip"))
			return
		}
		fmt.Fprintf(w, `{"tag_name":"v0.9.1","assets":[{"name":"t.zip","size":3,"browser_download_url":"%s/asset"}]}`, srv.URL)
	}))
	defer srv.Close()
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	raw, tag, err := ui.installOptions().Fetch(context.Background(), release.Spec{API: srv.URL + "/latest", Asset: "t.zip"}, func(string) {})
	if err != nil || string(raw) != "zip" || tag != "v0.9.1" {
		t.Fatalf("%q %q %v", raw, tag, err)
	}
}
