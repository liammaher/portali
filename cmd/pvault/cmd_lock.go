package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func cmdLock() {
	// Stopping our own server locks the vault and zeroes the key. Locking is
	// always safe, so this path needs no password. The PID is only trusted
	// if it still belongs to a pvault process, never a reused PID, and it is
	// tried before probing so a slow server can't be mistaken for a stopped one.
	if stopOwnServer() {
		fmt.Println("Vault locked (server stopped).")
		return
	}
	removePID()

	if !portHasVault() {
		// Nothing to lock. Clean up files a crashed server may have left.
		removeLegacySessionFile()
		fmt.Println("Vault is already locked (server not running).")
		return
	}

	// The running server wasn't started by 'pvault unlock' (e.g. 'pvault
	// serve'), so lock it through the API instead.
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
	waitForServerExit(3 * time.Second)
	removeLegacySessionFile()
	removePID()
	return true
}

// isVaultServerProcess reports whether pid is a running pvault process.
func isVaultServerProcess(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	name := filepath.Base(strings.TrimSpace(string(out)))
	if strings.Contains(name, "pvault") {
		return true
	}
	self, err := os.Executable()
	return err == nil && name == filepath.Base(self)
}

// waitForServerExit polls until the server stops answering or the timeout passes.
func waitForServerExit(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && portHasVault() {
		time.Sleep(100 * time.Millisecond)
	}
}
