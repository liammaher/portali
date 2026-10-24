package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// startBackgroundServer spawns 'pvault serve' detached from the terminal,
// passes the credentials over stdin, and waits until the vault reports
// unlocked. The PID is recorded only for a server that is actually running,
// and an early exit is reported instead of claiming success.
// Returns verified=false if the server is still starting after the timeout.
func startBackgroundServer(dir, pw, sk string) (verified bool, err error) {
	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("finding executable: %w", err)
	}

	cmd := exec.Command(exe, "serve", "--password-stdin")
	detachFromTerminal(cmd)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VAULT_DIR=%s", dir))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false, fmt.Errorf("creating stdin pipe: %w", err)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("starting server: %w", err)
	}
	fmt.Fprintf(stdin, "%s\n%s\n", pw, sk)
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
			return false, errors.New("vault server exited during startup — is something else using " + serverAddr() + "? Run 'pvault serve' to see the error")
		case <-deadline:
			writePID(cmd.Process.Pid)
			return false, nil
		case <-tick.C:
			resp, err := publicRequest("GET", "/vault/status", nil)
			if err != nil {
				continue
			}
			var status struct {
				Locked bool `json:"locked"`
			}
			json.NewDecoder(resp.Body).Decode(&status)
			resp.Body.Close()
			if status.Locked {
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
