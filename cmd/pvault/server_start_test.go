package main

import (
	"os"
	"testing"
)

func TestLastLine(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString("Vault server starting\nError: start server: another vault server is already serving /x\n")
	if got := lastLine(f); got != "Error: start server: another vault server is already serving /x" {
		t.Fatalf("lastLine = %q", got)
	}
}
