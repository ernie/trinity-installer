package frame

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeDevkit mimics /usr/share/steamos-devkit/steamos-devkit-service.py.
func fakeDevkit(t *testing.T, register func(body string) (int, string)) (*httptest.Server, *http.Client) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login-name", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "steamos") })
	mux.HandleFunc("/properties.json", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"txtvers":1,"login":"steamos"}`) })
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		code, body := register(string(b))
		w.WriteHeader(code)
		io.WriteString(w, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

func TestLookup(t *testing.T) {
	srv, client := fakeDevkit(t, nil)
	host := strings.TrimPrefix(srv.URL, "http://")
	h, err := Lookup(context.Background(), client, host)
	if err != nil || h.Login != "steamos" || h.Host != host {
		t.Fatalf("%+v %v", h, err)
	}
	if _, err := Lookup(context.Background(), client, "127.0.0.1:1"); err == nil {
		t.Fatal("dead host accepted")
	}
}

func TestRegisterSendsTheKeyLine(t *testing.T) {
	var got string
	srv, client := fakeDevkit(t, func(body string) (int, string) { got = body; return 200, "Registered\n" })
	h := Headset{Host: strings.TrimPrefix(srv.URL, "http://")}
	if err := Register(context.Background(), client, h, "ssh-rsa AAAA trinity-installer"); err != nil {
		t.Fatal(err)
	}
	if got != "ssh-rsa AAAA trinity-installer" {
		t.Fatalf("body %q", got)
	}
}

func TestRegisterRejected(t *testing.T) {
	srv, client := fakeDevkit(t, func(string) (int, string) { return 403, `{"error":"declined on device"}` })
	h := Headset{Host: strings.TrimPrefix(srv.URL, "http://")}
	err := Register(context.Background(), client, h, "ssh-rsa AAAA trinity-installer")
	if err == nil || !strings.Contains(err.Error(), "declined on device") {
		t.Fatalf("%v", err)
	}
}

func TestRegisterHonorsContext(t *testing.T) {
	srv, client := fakeDevkit(t, func(string) (int, string) { time.Sleep(2 * time.Second); return 200, "Registered\n" })
	h := Headset{Host: strings.TrimPrefix(srv.URL, "http://")}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := Register(ctx, client, h, "ssh-rsa AAAA trinity-installer"); err == nil {
		t.Fatal("timeout ignored")
	}
}
