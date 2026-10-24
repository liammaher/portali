package main

import (
	"fmt"
	"os"
	"syscall"
)

func cmdLock() {
	// Stopping the server locks the vault and zeroes the key. Locking is
	// always safe, so this path needs no password.
	if pid, err := readPID(); err == nil {
		if p, err := os.FindProcess(pid); err == nil && p.Signal(syscall.SIGTERM) == nil {
			removeLegacySessionFile()
			removePID()
			fmt.Println("Vault locked (server stopped).")
			return
		}
	}

	// No usable PID file (e.g. the server was started with 'pvault serve'):
	// lock through the API instead.
	resp, err := apiRequest("POST", "/vault/lock", nil)
	if err != nil {
		fatal("could not lock: %v", err)
	}
	if err := apiResult(resp, nil); err != nil {
		fatal("%v", err)
	}
	removeLegacySessionFile()
	removePID()
	fmt.Println("Vault locked.")
}
