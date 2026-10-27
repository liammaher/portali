// Package winhello asks the person at this computer to approve a request with
// Windows Hello (face, fingerprint or PIN), through WinRT's UserConsentVerifier
// called from pure Go. The vault server asks itself, so the answer never
// leaves the process that holds the vault key; the prompt shows the message
// it is given, which the caller words from what it will grant. Elsewhere
// Hello is never available.
package winhello

import "errors"

var (
	// ErrUnsupported means this system has no Windows Hello.
	ErrUnsupported = errors.New("Windows Hello is available only on Windows 11")
	// ErrNotSetUp means Windows Hello has no face, fingerprint or PIN set up
	// for this user, or no device for it.
	ErrNotSetUp = errors.New("Windows Hello isn't set up for you on this computer (Settings > Accounts > Sign-in options)")
	// ErrDisabled means policy turned Windows Hello off.
	ErrDisabled = errors.New("Windows Hello is turned off by policy on this computer")
	// ErrBusy means the device is in use, e.g. by another prompt.
	ErrBusy = errors.New("Windows Hello is busy — try again")
)

// Hello is Windows Hello on this computer.
type Hello struct{}

// The WinRT interfaces used, by IID. The async operations' IIDs are derived
// from their type signatures (iid_test.go recomputes them).
const (
	className = "Windows.Security.Credentials.UI.UserConsentVerifier"
	// IUserConsentVerifierStatics: CheckAvailabilityAsync, RequestVerificationAsync.
	iidStatics = "af4f3f91-564c-4ddc-b8b5-973447627c65"
	// IUserConsentVerifierInterop: RequestVerificationForWindowAsync(HWND, ...).
	iidInterop = "39e050c3-4e74-441a-8dc0-b81104df949c"
	// IAsyncInfo.
	iidAsyncInfo = "00000036-0000-0000-c000-000000000046"
	// IAsyncOperation<UserConsentVerifierAvailability>.
	iidAsyncAvailability = "ddd384f3-d818-5d83-ab4b-32119c28587c"
	// IAsyncOperation<UserConsentVerificationResult>.
	iidAsyncResult = "fd596ffd-2318-558f-9dbe-d21df43764a5"
)

// UserConsentVerifierAvailability and UserConsentVerificationResult values.
const (
	available            = 0
	deviceNotPresent     = 1
	notConfiguredForUser = 2
	disabledByPolicy     = 3
	deviceBusy           = 4
	verified             = 0
	retriesExhausted     = 5
	canceled             = 6
)

// availabilityError is what Available reports for an availability value.
func availabilityError(v int32) error {
	switch v {
	case available:
		return nil
	case deviceNotPresent, notConfiguredForUser:
		return ErrNotSetUp
	case disabledByPolicy:
		return ErrDisabled
	case deviceBusy:
		return nil // there is a device; it is just in use right now
	}
	return errors.New("Windows Hello reported an unknown state")
}

// resultOf is Approve's answer for a verification result: approved, refused
// (the user cancelled or failed too often), or an error.
func resultOf(v int32) (bool, error) {
	switch v {
	case verified:
		return true, nil
	case canceled, retriesExhausted:
		return false, nil
	case deviceNotPresent, notConfiguredForUser:
		return false, ErrNotSetUp
	case disabledByPolicy:
		return false, ErrDisabled
	case deviceBusy:
		return false, ErrBusy
	}
	return false, errors.New("Windows Hello returned an unknown result")
}
