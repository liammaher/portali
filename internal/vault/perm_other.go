//go:build !windows

package vault

// RestrictDir is a no-op here: folders are created mode 0700.
func RestrictDir(string) error { return nil }
