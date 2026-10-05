package main

import "testing"

func TestVersionString(t *testing.T) {
	if got := versionString("v1.2.3"); got != "Trinity Installer v1.2.3" {
		t.Fatalf("got %q", got)
	}
	if got := versionString(""); got != "Trinity Installer dev" {
		t.Fatalf("got %q", got)
	}
}
