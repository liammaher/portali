package wincred

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW  = advapi32.NewProc("CredReadW")
	procCredWriteW = advapi32.NewProc("CredWriteW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

// credential is CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// Read returns the secret stored under target and the user name it was saved
// with, or ErrNotFound.
func Read(target string) (secret, user string, err error) {
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", "", err
	}
	var c *credential
	r, _, e := procCredReadW.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c)))
	if r == 0 {
		if errors.Is(e, windows.ERROR_NOT_FOUND) {
			return "", "", ErrNotFound
		}
		return "", "", fmt.Errorf("CredRead %s: %w", target, e)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(c)))
	if c.CredentialBlobSize > 0 {
		blob := unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)
		secret = decode(blob)
		clear(blob) // not left in the freed buffer
	}
	return secret, windows.UTF16PtrToString(c.UserName), nil
}

// Write saves secret under target for user, replacing what was there.
func Write(target, user, secret string) error {
	blob := encode(secret)
	defer clear(blob)
	if len(blob) > MaxSecretBytes {
		return fmt.Errorf("the secret is too long for Windows Credential Manager (%d bytes, at most %d)", len(blob), MaxSecretBytes)
	}
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	u, err := windows.UTF16PtrFromString(user)
	if err != nil {
		return err
	}
	c := credential{
		Type:               credTypeGeneric,
		TargetName:         t,
		CredentialBlobSize: uint32(len(blob)),
		Persist:            credPersistLocalMachine,
		UserName:           u,
	}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	if r, _, e := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return fmt.Errorf("CredWrite %s: %w", target, e)
	}
	return nil
}

// Delete removes target; one that isn't there is already deleted.
func Delete(target string) error {
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	if r, _, e := procCredDelete.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0); r == 0 && !errors.Is(e, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("CredDelete %s: %w", target, e)
	}
	return nil
}
