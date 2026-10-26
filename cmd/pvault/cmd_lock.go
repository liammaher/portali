package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

func cmdLock() {
	positionalArgs(os.Args[2:], "usage: pvault lock", 0, 0)
	// Stopping our own server locks the vault and zeroes the key. Locking is
	// always safe, so this path needs no password. The PID is only trusted
	// if it still belongs to a pvault process, never a reused PID, and it is
	// tried before probing so a slow server can't be mistaken for a stopped one.
	if stopOwnServer() {
		fmt.Println("Vault locked (server stopped).")
		return
	}
	removeStalePID()

	if !portHasVault() {
		// Nothing to lock. Clean up files a crashed server may have left.
		removeLegacySessionFile()
		fmt.Println("Vault is already locked (server not running).")
		return
	}

	// The running server wasn't started by 'pvault unlock' (e.g. 'pvault
	// serve'), so lock it through the API instead, unless it's locked
	// already (no key in memory, and the API would refuse).
	if !isVaultUnlocked() {
		fmt.Println("Vault is already locked (server still running).")
		return
	}
	resp, err := apiRequest("POST", "/vault/lock", nil)
	if err != nil {
		fatal("could not lock: %v", err)
	}
	if err := apiResult(resp, nil); err != nil {
		fatal("%v", err)
	}
	removeLegacySessionFile()
	fmt.Println("Vault locked.")
}

// stopOwnServer stops the server 'pvault unlock' started, if its recorded PID
// still belongs to a pvault process, and reports whether it did.
func stopOwnServer() bool {
	pid, err := readPID()
	if err != nil || !isVaultServerProcess(pid) {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil || p.Signal(syscall.SIGTERM) != nil {
		return false
	}
	// The server writes any pending backup before it locks, which can take a
	// moment; never report "locked" while it is still running.
	if !waitForServerExit(3 * time.Second) {
		fmt.Fprintln(os.Stderr, "Writing the last backup before locking...")
		if !waitForServerExit(2 * time.Minute) {
			fatal("the vault server (pid %d) is still running and may still be unlocked — run '%s' again", pid, pvCmd("lock"))
		}
	}
	removeLegacySessionFile()
	removePID()
	return true
}

// waitForServerExit polls until the server stops answering or the timeout
// passes, and reports whether it stopped.
func waitForServerExit(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && portHasVault() {
		time.Sleep(100 * time.Millisecond)
	}
	return !portHasVault()
}
