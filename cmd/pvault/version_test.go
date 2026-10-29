package main

import "testing"

func TestVersionLine(t *testing.T) {
	if got := versionLine(); got != "pvault dev" {
		t.Fatalf("a build from source says %q, not %q", got, "pvault dev")
	}
	old := version
	defer func() { version = old }()
	version = "0.1.0"
	if got := versionLine(); got != "pvault 0.1.0" {
		t.Fatalf("a release build says %q", got)
	}
}
