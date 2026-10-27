package main

import (
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// peerCred returns the uid and pid of the process that listens on a Unix
// socket connection, as the kernel records them.
func peerCred(conn *net.UnixConn) (uid, pid int, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if sockErr != nil {
		return 0, 0, sockErr
	}
	return int(cred.Uid), int(cred.Pid), nil
}

// processExe is the kernel's path of pid's running executable
// (/proc/<pid>/exe). After a
// reinstall (rm + cp) the kernel appends " (deleted)" to the old file's
// path; it still names this pvault, so lock can stop the server.
func processExe(pid int) (string, error) {
	exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	return strings.TrimSuffix(exe, " (deleted)"), err
}

func checkSameBuild(int) error { return nil }
