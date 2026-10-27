package filesync

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

// A sign-in several kilobytes long is kept in Credential Manager in chunks,
// read back whole, replaced and deleted.
func TestCredentialStoreRoundTrip(t *testing.T) {
	b := make([]byte, 8)
	rand.Read(b)
	service := "pvault-files-test-" + hex.EncodeToString(b)
	var c CredentialStore
	t.Cleanup(func() { c.Delete(service) })
	long := strings.Repeat(`{"refresh_token":"0.AXsA-abc_DEF.ghi"}`, 120)
	if err := c.Save(service, long); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Load(service); err != nil || got != long {
		t.Fatalf("long value: %d bytes back, %v", len(got), err)
	}
	if err := c.Save(service, "short"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Load(service); got != "short" {
		t.Fatalf("replaced: %q", got)
	}
	if err := c.Delete(service); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Load(service); err != nil || got != "" {
		t.Fatalf("after delete: %q, %v", got, err)
	}
}
