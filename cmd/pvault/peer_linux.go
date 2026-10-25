package main

import (
	"net"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// peerProcess returns the uid and executable of the process that listens on
// a Unix socket connection. /proc/<pid>/exe is the kernel's link to the
// running file, which the peer can't rewrite.
func peerProcess(conn *net.UnixConn) (uid int, exe string, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, "", err
	}
	var cred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, "", err
	}
	if sockErr != nil {
		return 0, "", sockErr
	}
	exe, err = os.Readlink("/proc/" + strconv.Itoa(int(cred.Pid)) + "/exe")
	return int(cred.Uid), exe, err
}
