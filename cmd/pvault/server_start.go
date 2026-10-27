package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// startBackgroundServer spawns 'pvault serve' detached from the terminal,
// passes the credentials over stdin, and waits until the vault reports
// unlocked. With no password it starts the server locked instead, for Touch
// ID to unlock. The PID is recorded only for a server that is actually
// running, and an early exit is reported instead of claiming success.
// Returns verified=false if the server is still starting after the timeout.
func startBackgroundServer(pw, sk string) (verified bool, err error) {
	locked := pw == ""
	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("finding executable: %w", err)
	}

	mode := "--password-stdin"
	if locked {
		mode = "--locked"
	}
	cmd := exec.Command(exe, "serve", mode)
	detachFromTerminal(cmd)
	if !locked {
		cmd.Stdin = strings.NewReader(pw + "\n" + sk + "\n")
	}
	// The server's stderr goes to a file, so an early exit can report the
	// server's own reason (socket or port) without tying the long-lived
	// server to a pipe this command closes.
	logFile, err := newServerLog()
	if err != nil {
		return false, fmt.Errorf("creating server log: %w", err)
	}
	defer logFile.Close()
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		unlinkServerLog(logFile)
		return false, fmt.Errorf("starting server: %w", err)
	}
	unlinkServerLog(logFile)

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
			return false, errors.New("vault server exited during startup — is something else using " + serverAddr() + "? Run '" + pvCmd("serve") + "' to see the error")
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
				return false, fmt.Errorf("server on %s is not the one we started (vault still locked) — run '%s' then '%s'", serverAddr(), pvCmd("lock"), pvCmd("unlock"))
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

// newServerLog is an unlinked temp file, except on Windows, which can't
// delete a file that is open: there it is <vault dir>\run\serve.log, which
// each start overwrites.
func newServerLog() (*os.File, error) {
	if runtime.GOOS != "windows" {
		return os.CreateTemp("", "pvault-serve-*.log")
	}
	dir := filepath.Join(vaultDir(), "run")
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	if err := vault.RestrictDir(dir); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "serve.log"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
}

func unlinkServerLog(f *os.File) {
	if runtime.GOOS != "windows" {
		os.Remove(f.Name())
	}
}
