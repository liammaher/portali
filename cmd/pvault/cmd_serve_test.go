package main

import (
	"strings"
	"testing"
)

// The password reaches the server exactly as typed: trimming it would make
// 'pvault unlock' fail for a password that starts or ends with a space.
func TestReadStdinCredentials_KeepsSpaces(t *testing.T) {
	pw, sk, err := readStdinCredentials(strings.NewReader(" correct horse \nabcd \n"))
	if err != nil {
		t.Fatal(err)
	}
	if pw != " correct horse " || sk != "abcd" {
		t.Fatalf("got %q, %q", pw, sk)
	}
	if pw, _, _ := readStdinCredentials(strings.NewReader("pw\r\nsk\r\n")); pw != "pw" {
		t.Fatalf("CRLF line: password %q", pw)
	}
	if _, _, err := readStdinCredentials(strings.NewReader("only-password\n")); err == nil {
		t.Fatal("missing secret key accepted")
	}
}
