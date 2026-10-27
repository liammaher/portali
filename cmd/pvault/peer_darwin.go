package main

import (
	"bytes"
	"errors"
	"net"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// peerCred returns the uid and pid of the process that listens on a Unix
// socket connection, as the kernel records them.
func peerCred(conn *net.UnixConn) (uid, pid int, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *unix.Xucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if sockErr == nil {
			pid, sockErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		}
	}); err != nil {
		return 0, 0, err
	}
	if sockErr != nil {
		return 0, 0, sockErr
	}
	return int(cred.Uid), pid, nil
}

// processExe is the kernel's record of pid's running file (not the
// process's own memory, as argv or KERN_PROCARGS2 would be): libproc's
// proc_pidpath, which x/sys/unix doesn't wrap:
// proc_info(PROC_INFO_CALL_PIDINFO, pid, PROC_PIDPATHINFO, 0, buf, size).
// It still names the old file after pvault is reinstalled with rm + cp.
func processExe(pid int) (string, error) {
	const (
		procInfoCallPidInfo = 2
		procPidPathInfo     = 11
		maxPathSize         = 4 * 1024 // PROC_PIDPATHINFO_MAXSIZE
	)
	buf := make([]byte, maxPathSize)
	_, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPidInfo, uintptr(pid),
		procPidPathInfo, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if errno != 0 {
		return "", errno
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	if len(buf) == 0 {
		return "", unix.ESRCH
	}
	return string(buf), nil
}

// errOlderBuild means the server runs an earlier build of this pvault file,
// e.g. after 'make install' while it kept running. The Touch ID helper
// refuses such a server, so the CLI refuses it too, before any prompt.
var errOlderBuild = errors.New("the vault server runs an older build of pvault")

// checkSameBuild compares the kernel's cdhash (csops CS_OPS_CDHASH) of the
// running server with this process's. Unsigned builds have none and are
// only compared by path.
func checkSameBuild(pid int) error {
	server, ok1 := runningCDHash(pid)
	self, ok2 := runningCDHash(os.Getpid())
	if ok1 && ok2 && server != self {
		return errOlderBuild
	}
	return nil
}

func runningCDHash(pid int) ([20]byte, bool) {
	const csOpsCDHash = 5
	var h [20]byte
	_, _, errno := unix.Syscall6(unix.SYS_CSOPS, uintptr(pid), csOpsCDHash,
		uintptr(unsafe.Pointer(&h[0])), uintptr(len(h)), 0, 0)
	return h, errno == 0
}
