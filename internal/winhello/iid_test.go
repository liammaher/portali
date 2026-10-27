package winhello

import (
	"crypto/sha1"
	"fmt"
	"testing"
)

// pinterfaceIID derives the IID of a parameterized WinRT interface from its
// type signature, as the Windows Runtime does: a version-5 UUID (SHA-1) in
// the WinRT namespace 11f47ad5-7b73-42c0-abae-878b1e16adee.
func pinterfaceIID(signature string) string {
	ns := []byte{0x11, 0xf4, 0x7a, 0xd5, 0x7b, 0x73, 0x42, 0xc0, 0xab, 0xae, 0x87, 0x8b, 0x1e, 0x16, 0xad, 0xee}
	sum := sha1.Sum(append(ns, signature...))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

const iidAsyncOperation = "{9fc2b0bb-e446-44e2-aa61-9cab8f636af2}"

func TestAsyncOperationIIDs(t *testing.T) {
	// A published one first, so the derivation itself is checked.
	if got := pinterfaceIID("pinterface(" + iidAsyncOperation + ";b1)"); got != "cdb5efb3-5788-509d-9be1-71ccb8a3362a" {
		t.Fatalf("IAsyncOperation<Boolean> = %s", got)
	}
	for name, c := range map[string]struct{ enum, want string }{
		"availability": {"Windows.Security.Credentials.UI.UserConsentVerifierAvailability", iidAsyncAvailability},
		"result":       {"Windows.Security.Credentials.UI.UserConsentVerificationResult", iidAsyncResult},
	} {
		if got := pinterfaceIID("pinterface(" + iidAsyncOperation + ";enum(" + c.enum + ";i4))"); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

func TestResults(t *testing.T) {
	if ok, err := resultOf(verified); !ok || err != nil {
		t.Errorf("verified: %v %v", ok, err)
	}
	for _, v := range []int32{canceled, retriesExhausted} {
		if ok, err := resultOf(v); ok || err != nil {
			t.Errorf("%d: %v %v", v, ok, err)
		}
	}
	for _, v := range []int32{deviceNotPresent, notConfiguredForUser, disabledByPolicy, deviceBusy, 99} {
		if ok, err := resultOf(v); ok || err == nil {
			t.Errorf("%d: %v %v", v, ok, err)
		}
	}
	if availabilityError(available) != nil || availabilityError(deviceBusy) != nil || availabilityError(notConfiguredForUser) == nil {
		t.Error("availability")
	}
}
