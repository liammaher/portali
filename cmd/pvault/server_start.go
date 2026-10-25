package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// startBackgroundServer spawns 'pvault serve' detached from the terminal,
// passes the credentials over stdin, and waits until the vault reports
// unlocked. With no password it starts the server locked instead, for Touch
// ID to unlock. The PID is recorded only for a server that is actually
// running, and an early exit is reported instead of claiming success.
// Returns verified=false if the server is still starting after the timeout.
func startBackgroundServer(dir, pw, sk string) (verified bool, err error) {
	locked := pw == ""
	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("finding executable: %w", err)
	}

	cmd := exec.Command(exe, "serve", "--password-stdin")
	if locked {
		cmd = exec.Command(exe, "serve", "--locked")
	}
	detachFromTerminal(cmd)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VAULT_DIR=%s", dir))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false, fmt.Errorf("creating stdin pipe: %w", err)
	}
	cmd.Stdout = nil
	// The server's stderr goes to an unlinked temp file, so an early exit
	// can report the server's own reason (socket or port) without tying the
	// long-lived server to a pipe this command closes.
	logFile, err := os.CreateTemp("", "pvault-serve-*.log")
	if err != nil {
		return false, fmt.Errorf("creating server log: %w", err)
	}
	defer logFile.Close()
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		os.Remove(logFile.Name())
		return false, fmt.Errorf("starting server: %w", err)
	}
	os.Remove(logFile.Name())
	if !locked {
		fmt.Fprintf(stdin, "%s\n%s\n", pw, sk)
	}
	stdin.Close()

	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()

	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-exited:
			if reason := lastLine(logFile); reason != "" {
				return false, fmt.Errorf("vault server exited during startup: %s", strings.TrimPrefix(reason, "Error: "))
			}
			return false, errors.New("vault server exited during startup — is something else using " + serverAddr() + "? Run 'pvault serve' to see the error")
		case <-deadline:
			writePID(cmd.Process.Pid)
			return false, nil
		case <-tick.C:
			status, err := serverStatus()
			if err != nil {
				continue
			}
			if status.Locked && !locked {
				// Our server unlocks before it binds, so a locked answer
				// comes from some other server on the port.
				cmd.Process.Kill()
				return false, fmt.Errorf("server on %s is not the one we started (vault still locked) — run 'pvault lock' then 'pvault unlock'", serverAddr())
			}
			writePID(cmd.Process.Pid)
			return true, nil
		}
	}
}

// lastLine returns the last line the exited server wrote to f.
func lastLine(f *os.File) string {
	f.Seek(0, io.SeekStart)
	data, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
