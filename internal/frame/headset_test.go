package frame

import "testing"

func TestDevkitURL(t *testing.T) {
	if got := devkitURL("frame.local", "/login-name"); got != "http://frame.local:32000/login-name" {
		t.Fatal(got)
	}
	if got := devkitURL("127.0.0.1:8080", "/register"); got != "http://127.0.0.1:8080/register" {
		t.Fatal(got)
	}
}

func TestSSHAddr(t *testing.T) {
	cases := []struct {
		h    Headset
		want string
	}{
		{Headset{Host: "frame.local"}, "frame.local:22"},
		{Headset{Host: "frame.local:32000"}, "frame.local:22"},
		{Headset{Host: "frame.local", Addr: "192.168.1.9"}, "192.168.1.9:22"},
	}
	for _, c := range cases {
		if got := c.h.sshAddr(); got != c.want {
			t.Errorf("%+v: %s, want %s", c.h, got, c.want)
		}
	}
}
