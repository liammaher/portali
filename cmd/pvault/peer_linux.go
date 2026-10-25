package main

import (
	"net"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// peerProcess returns the uid, pid and executable of the process that
// listens on a Unix socket connection. /proc/<pid>/exe is the kernel's link
// to the running file.
func peerProcess(conn *net.UnixConn) (uid, pid int, exe string, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, "", err
	}
	var cred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, "", err
	}
	if sockErr != nil {
		return 0, 0, "", sockErr
	}
	exe, err = processExe(int(cred.Pid))
	return int(cred.Uid), int(cred.Pid), exe, err
}

// processExe is the kernel's path of pid's running executable.
func processExe(pid int) (string, error) {
	return os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
}

func checkSameBuild(int) error { return nil }
