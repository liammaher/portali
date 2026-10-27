package wincred

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func testTarget(t *testing.T) string {
	b := make([]byte, 8)
	rand.Read(b)
	target := "pvault-test-" + hex.EncodeToString(b)
	t.Cleanup(func() { Delete(target) })
	return target
}

// A secret goes into Credential Manager and comes back, is replaced, and is
// deleted; one too long for a credential is refused.
func TestCredentialManagerRoundTrip(t *testing.T) {
	target := testTarget(t)
	if _, _, err := Read(target); err != ErrNotFound {
		t.Fatalf("before writing: %v", err)
	}
	secret := strings.Repeat("pàss€", 250) // 1250 UTF-16 units: just under the limit
	if err := Write(target, "tester", secret); err != nil {
		t.Fatal(err)
	}
	if got, user, err := Read(target); err != nil || got != secret || user != "tester" {
		t.Fatalf("read %d chars for %q, %v", len(got), user, err)
	}
	if err := Write(target, "tester", "short"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := Read(target); got != "short" {
		t.Fatalf("replaced: %q", got)
	}
	if err := Write(target, "tester", strings.Repeat("x", MaxSecretBytes/2+1)); err == nil {
		t.Fatal("an oversized secret was written")
	}
	if err := Delete(target); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(target); err != ErrNotFound {
		t.Fatalf("after delete: %v", err)
	}
	if err := Delete(target); err != nil {
		t.Fatalf("deleting what isn't there: %v", err)
	}
}
