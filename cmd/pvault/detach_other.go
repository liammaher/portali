//go:build !linux && !darwin && !windows

package main

import "os/exec"

// detachFromTerminal is a no-op on platforms without POSIX sessions.
func detachFromTerminal(cmd *exec.Cmd) {}
