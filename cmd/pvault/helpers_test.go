package main

import "testing"

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
