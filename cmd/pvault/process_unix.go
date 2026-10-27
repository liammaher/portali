//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

func processRunning(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

// askServerToStop sends pid SIGTERM, which a vault server takes as lock.
func askServerToStop(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.SIGTERM) == nil
}

func addrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
