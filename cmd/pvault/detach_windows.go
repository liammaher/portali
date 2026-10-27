package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detachFromTerminal starts the background server with no console, in its own
// process group, so closing the terminal that ran 'pvault unlock' or pressing
// Ctrl+C there doesn't stop it. A Windows child gets only the handles it is
// given (stdin, stdout, stderr), so nothing else leaks into the server.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
