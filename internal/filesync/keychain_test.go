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
		if got, gen, err := parseChunkIndex(chunkIndex(n, "0a1b2c3d")); err != nil || got != n || gen != "0a1b2c3d" {
			t.Errorf("index %d = %d %q, %v", n, got, gen, err)
		}
	}
	for _, bad := range []string{"", "x", "pvault-chunks:", "pvault-chunks:0:0a1b2c3d", "pvault-chunks:-1:0a1b2c3d", "pvault-chunks:999:0a1b2c3d",
		"pvault-chunks:2", "pvault-chunks:2:", "pvault-chunks:2:xyz", "pvault-chunks:2:0a1b2c3d -s evil", "pvault-chunks:2:0A1B2C3D"} {
		if _, _, err := parseChunkIndex(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if chunkService("pvault-files-abc", "0a1b2c3d", 3) != "pvault-files-abc.g0a1b2c3d.c3" ||
		!reService.MatchString(chunkService(ServiceName(strings.Repeat("a", 32)), "0a1b2c3d", 63)) {
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
	// The old generation's parts are gone: a part of it can't be found by index.
	if idx, _ := (keychainItems{mustAccount(t)}).loadItem(service); idx == "" {
		t.Fatal("no index after a save")
	} else if _, gen, _ := parseChunkIndex(idx); gen == "" {
		t.Fatal("index without a generation")
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
