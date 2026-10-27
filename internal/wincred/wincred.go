// Package wincred keeps secrets in Windows Credential Manager: generic
// credentials of this Windows user, kept on this computer. Like the macOS
// Keychain items pvault writes, they are this user's, readable by the user's
// own programs; unlike the Keychain, Windows has no per-program access list,
// so any program running as the user can read them. Secrets are stored as
// UTF-16, as cmdkey and the Credential Manager window store passwords.
package wincred

import (
	"errors"
	"unicode/utf16"
)

var (
	// ErrNotFound means no credential has that target name.
	ErrNotFound = errors.New("no such credential in Windows Credential Manager")
	// ErrUnsupported means this system has no Credential Manager.
	ErrUnsupported = errors.New("Windows Credential Manager is available only on Windows")
)

// MaxSecretBytes is the most a credential holds (CRED_MAX_CREDENTIAL_BLOB_SIZE),
// so a secret is at most half as many UTF-16 units.
const MaxSecretBytes = 5 * 512

// encode is a secret as Credential Manager stores a password.
func encode(secret string) []byte {
	units := utf16.Encode([]rune(secret))
	b := make([]byte, 2*len(units))
	for i, u := range units {
		b[2*i], b[2*i+1] = byte(u), byte(u>>8)
	}
	return b
}

// decode reads a stored password back.
func decode(b []byte) string {
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}
