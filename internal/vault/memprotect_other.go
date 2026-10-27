//go:build !linux && !darwin && !windows

package vault

func lockMemory(b []byte)   {}
func unlockMemory(b []byte) {}
func disableCoreDumps()     {}
