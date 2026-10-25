package main

import (
	"bytes"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/unix"
)

// peerProcess returns the uid and executable of the process that listens on
// a Unix socket connection. The path comes from the kernel's record of the
// running file (proc_pidpath), not from the process's own memory the way
// argv or KERN_PROCARGS2 would, so the peer can't rewrite it.
func peerProcess(conn *net.UnixConn) (uid int, exe string, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, "", err
	}
	var pid int
	var cred *unix.Xucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if sockErr == nil {
			pid, sockErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		}
	}); err != nil {
		return 0, "", err
	}
	if sockErr != nil {
		return 0, "", sockErr
	}
	// Only a hardened-runtime pvault ('make build' signs it so) is trusted:
	// without it, a genuine pvault started by someone else could have a
	// library injected through DYLD_INSERT_LIBRARIES.
	if requireHardenedRuntime {
		if err := checkHardenedRuntime(pid); err != nil {
			return 0, "", err
		}
	}
	exe, err = processPath(pid)
	return int(cred.Uid), exe, err
}

// checkHardenedRuntime reads the kernel's code-signing status of pid
// (csops CS_OPS_STATUS), which reflects the running code, not the file.
func checkHardenedRuntime(pid int) error {
	const (
		csOpsStatus = 0
		csValid     = 0x1
		csRuntime   = 0x10000
	)
	var flags uint32
	_, _, errno := unix.Syscall6(unix.SYS_CSOPS, uintptr(pid), csOpsStatus,
		uintptr(unsafe.Pointer(&flags)), unsafe.Sizeof(flags), 0, 0)
	if errno != 0 {
		return errno
	}
	if flags&csValid == 0 || flags&csRuntime == 0 {
		return fmt.Errorf("the vault server isn't a pvault signed with the hardened runtime — build pvault with 'make build'")
	}
	return nil
}

// processPath is libproc's proc_pidpath, which x/sys/unix doesn't wrap:
// proc_info(PROC_INFO_CALL_PIDINFO, pid, PROC_PIDPATHINFO, 0, buf, size).
func processPath(pid int) (string, error) {
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
