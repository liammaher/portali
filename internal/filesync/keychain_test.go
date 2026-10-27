package filesync

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestChunks(t *testing.T) {
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 5*chunkSize + 7} {
		s := strings.Repeat("x", n)
		parts := splitChunks(s)
		if strings.Join(parts, "") != s {
			t.Errorf("%d bytes don't survive a split", n)
		}
		for _, p := range parts {
			if len(p) > chunkSize {
				t.Errorf("a part of %d bytes", len(p))
			}
		}
		if n > 0 && n <= chunkSize && len(parts) != 1 {
			t.Errorf("%d bytes in %d parts", n, len(parts))
		}
	}
	for n := 1; n <= maxChunks; n += 9 {
		if got, err := parseChunkIndex(chunkIndex(n)); err != nil || got != n {
			t.Errorf("index %d = %d, %v", n, got, err)
		}
	}
	for _, bad := range []string{"", "x", "pvault-chunks:", "pvault-chunks:0", "pvault-chunks:-1", "pvault-chunks:999", "pvault-chunks:2 -s evil"} {
		if _, err := parseChunkIndex(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if chunkService("pvault-files-abc", 3) != "pvault-files-abc.c3" || !reService.MatchString(chunkService(ServiceName(strings.Repeat("a", 32)), 63)) {
		t.Error("chunk service names")
	}
}

// TestKeychainStoreRoundTrip writes to the real login Keychain, so it only
// runs when asked: PVAULT_KEYCHAIN_TEST=1 go test ./internal/filesync/
// It stores a value several kilobytes long (a Microsoft refresh token), reads
// it back, replaces it with a shorter one, and deletes it.
func TestKeychainStoreRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("needs the macOS Keychain")
	}
	if os.Getenv("PVAULT_KEYCHAIN_TEST") != "1" {
		t.Skip("touches the login Keychain; set PVAULT_KEYCHAIN_TEST=1 to run")
	}
	b := make([]byte, 8)
	rand.Read(b)
	service := "pvault-files-test-" + hex.EncodeToString(b)
	var k KeychainStore
	t.Cleanup(func() { k.Delete(service) })

	long := strings.Repeat(`{"refresh_token":"0.AXsA-abc_DEF.ghi"}`, 120) // ~4.5 KB
	if err := k.Save(service, long); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Load(service); err != nil || got != long {
		t.Fatalf("long value: %d bytes back, %v", len(got), err)
	}
	if err := k.Save(service, "short"); err != nil {
		t.Fatal(err)
	}
	if got, _ := k.Load(service); got != "short" {
		t.Fatalf("replaced value = %q", got)
	}
	// The leftover parts of the long value are gone.
	if part, _ := k.loadItem(mustAccount(t), chunkService(service, 3)); part != "" {
		t.Fatal("a part of the old value was left in the Keychain")
	}
	if err := k.Delete(service); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Load(service); err != nil || got != "" {
		t.Fatalf("after delete: %q, %v", got, err)
	}
}

func mustAccount(t *testing.T) string {
	t.Helper()
	a, err := keychainAccount()
	if err != nil {
		t.Fatal(err)
	}
	return a
}
