package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	var scope, ttl string
	flags := map[string]*string{"--scope": &scope, "--ttl": &ttl}
	if err := parseFlags([]string{"--scope", "identity.*", "--ttl", "1h"}, flags); err != nil || scope != "identity.*" || ttl != "1h" {
		t.Fatalf("parseFlags = %v, scope %q, ttl %q", err, scope, ttl)
	}
	for _, args := range [][]string{
		{"--tll", "1h"},             // typo
		{"--scope"},                 // missing value
		{"--scope", "--ttl", "1h"},  // value is another flag
		{"stray"},                   // positional argument
		{"--scope", "a.*", "extra"}, // trailing argument
	} {
		if err := parseFlags(args, flags); err == nil {
			t.Errorf("parseFlags(%q) accepted", args)
		}
	}
}

func TestCheckArgs(t *testing.T) {
	if err := checkArgs([]string{"identity"}, 0, 1); err != nil {
		t.Errorf("one category: %v", err)
	}
	for _, args := range [][]string{
		{"--bogus"},                  // read as a category name before
		{"identity", "residenza"},    // too many
		{"identity.full_name", "-f"}, // an option
	} {
		if err := checkArgs(args, 0, 1); err == nil {
			t.Errorf("%q accepted", args)
		}
	}
}

func TestSetValueArg(t *testing.T) {
	if v, given, err := setValueArg([]string{"Via Roma 1"}); err != nil || !given || v != "Via Roma 1" {
		t.Errorf("one value: %q %v %v", v, given, err)
	}
	if _, given, err := setValueArg(nil); err != nil || given {
		t.Errorf("no value: %v %v", given, err)
	}
	// Joined before, so "--sensitivity critical" became part of the value.
	for _, args := range [][]string{{"value", "--sensitivity", "critical"}, {"Mario", "Rossi"}, {"--sensitivity"}} {
		if _, _, err := setValueArg(args); err == nil {
			t.Errorf("%q accepted", args)
		}
	}
}

// A PID file naming a live process stays (its own pvault needs it to stop
// that server); one naming a finished process goes.
func TestRemoveStalePIDKeepsLiveServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses true(1)")
	}
	shortVaultDir(t)
	writePID(os.Getpid())
	removeStalePID()
	if _, err := readPID(); err != nil {
		t.Fatal("removed the PID file of a running process")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	writePID(cmd.Process.Pid)
	removeStalePID()
	if _, err := readPID(); err == nil {
		t.Fatal("kept the PID file of a finished process")
	}
}

// A repeated flag is an error: with the last one winning,
// --max-sensitivity public --max-sensitivity critical made a critical token.
func TestParseFlags_RefusesRepeat(t *testing.T) {
	var tier string
	err := parseFlags([]string{"--max-sensitivity", "public", "--max-sensitivity", "critical"}, map[string]*string{"--max-sensitivity": &tier})
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("err = %v, want given twice", err)
	}
}

func TestParseServeArgs(t *testing.T) {
	if stdin, locked, err := parseServeArgs([]string{"--password-stdin"}); err != nil || !stdin || locked {
		t.Fatalf("--password-stdin: %v %v %v", stdin, locked, err)
	}
	if stdin, locked, err := parseServeArgs(nil); err != nil || stdin || locked {
		t.Fatalf("no options: %v %v %v", stdin, locked, err)
	}
	for _, bad := range [][]string{{"--pasword-stdin"}, {"extra"}, {"--password-stdin", "--locked"}} {
		if _, _, err := parseServeArgs(bad); err == nil {
			t.Errorf("parseServeArgs(%q) accepted", bad)
		}
	}
}
