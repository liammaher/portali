package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"golang.org/x/sys/windows"
)

// pipeServerPID is the pid of the process serving the pipe conn, as the
// kernel records it.
func pipeServerPID(conn net.Conn) (int, error) {
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, errors.New("not a named pipe")
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(f.Fd()), &pid); err != nil {
		return 0, err
	}
	return int(pid), nil
}

// sameUser reports whether pid runs as this process's user (its token's SID).
func sameUser(pid int) (bool, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return false, err
	}
	defer tok.Close()
	theirs, err := tok.GetTokenUser()
	if err != nil {
		return false, err
	}
	mine, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false, err
	}
	return theirs.User.Sid.Equals(mine.User.Sid), nil
}

// processExe is the path of pid's running executable, as the kernel has it.
func processExe(pid int) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768) // the longest Windows path
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// checkSameBuild compares only paths here, as on Linux.
func checkSameBuild(int) error { return nil }

// stillActive is GetExitCodeProcess's code for a process that hasn't exited.
const stillActive = 259

func processRunning(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == stillActive
}

// askServerToStop asks the server pid to lock and exit, as SIGTERM does
// elsewhere: Windows has none, so pvault lock asks over the pipe (POST
// /vault/stop). Only this user can open the pipe, which is what being able to
// signal the server means elsewhere, and the request carries nothing secret.
func askServerToStop(pid int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialServerPipe(ctx)
	if err != nil {
		return false
	}
	if got, err := pipeServerPID(conn); err != nil || got != pid {
		conn.Close()
		return false
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext:       func(context.Context, string, string) (net.Conn, error) { return conn, nil },
			DisableKeepAlives: true,
		},
	}
	resp, err := client.Post("http://localhost/vault/stop", "application/json", nil)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusAccepted
}

func addrInUse(err error) bool {
	return errors.Is(err, windows.WSAEADDRINUSE)
}
