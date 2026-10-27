//go:build !windows

package wincred

// Read is never available here.
func Read(target string) (secret, user string, err error) { return "", "", ErrUnsupported }

// Write is never available here.
func Write(target, user, secret string) error { return ErrUnsupported }

// Delete is never available here.
func Delete(target string) error { return ErrUnsupported }
