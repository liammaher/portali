//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// detachFromTerminal starts the background server in its own session so it
// has no controlling terminal and is not killed by SIGHUP when the terminal
// that ran 'pvault unlock' closes.
//
// It also marks every inherited descriptor above stderr close-on-exec.
// Descriptors this process received without that flag (e.g. the write end of
// a shell pipe in 'pvault unlock | tee log') would otherwise leak into the
// long-lived server and keep the caller waiting for EOF forever.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return
	}
	for _, e := range entries {
		if fd, err := strconv.Atoi(e.Name()); err == nil && fd > 2 {
			syscall.CloseOnExec(fd)
		}
	}
}
