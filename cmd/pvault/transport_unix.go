//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Here the private channel is a Unix socket in the vault dir. Only a process
// that can write to <vault dir>/run can put a socket there. That private dir
// is the protection: it stops port squatters and agents kept out of the vault
// dir. The client also checks the listener's uid and that it runs this pvault
// binary (peer_*.go), which catches a stale or mismatched server, but a
// process that can already write to the vault dir can get around that check.

// maxSocketPath is sun_path's size on macOS (Linux allows 108).
const maxSocketPath = 104

func socketPath() string {
	return filepath.Join(vaultDir(), "run", "pvault.sock")
}

// checkSocketDir accepts only a real directory owned by this user that no
// one else can enter.
func checkSocketDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case !info.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	case !ok || int(st.Uid) != os.Getuid():
		return fmt.Errorf("%s is not owned by you", dir)
	case info.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%s must be private (mode 0700), not %#o", dir, info.Mode().Perm())
	}
	return nil
}

// listenSocket creates the server's socket, refusing to take over one that
// another live server answers on.
func listenSocket(path string) (net.Listener, error) {
	if len(path) >= maxSocketPath {
		return nil, fmt.Errorf("socket path %s is too long (max %d bytes) — use a shorter VAULT_DIR", path, maxSocketPath-1)
	}
	dir := filepath.Dir(path)
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	// A dir we own with loose permissions is tightened; a symlink or
	// someone else's dir is refused.
	if info, err := os.Lstat(dir); err == nil && info.IsDir() && info.Mode().Perm() != 0o700 {
		if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) == os.Getuid() {
			os.Chmod(dir, 0o700)
		}
	}
	if err := checkSocketDir(dir); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode().Type() != fs.ModeSocket {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, fmt.Errorf("another vault server is already serving %s", vaultDir())
		}
		os.Remove(path) // left by a server that crashed
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// dialVaultServer connects to the vault dir's socket and returns the
// connection only if this pvault binary, run by this user, is listening.
func dialVaultServer(ctx context.Context) (net.Conn, error) {
	path := socketPath()
	if err := checkSocketDir(filepath.Dir(path)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errNoVaultSocket
		}
		return nil, fmt.Errorf("refusing to send your password: %w", err)
	}
	if info, err := os.Lstat(path); err != nil {
		return nil, errNoVaultSocket
	} else if st, ok := info.Sys().(*syscall.Stat_t); info.Mode().Type() != fs.ModeSocket || !ok || int(st.Uid) != os.Getuid() {
		return nil, fmt.Errorf("refusing to send your password: %s is not your vault server's socket", path)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, errNoVaultSocket
	}
	if err := verifyPeer(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("refusing to send your password: %w", err)
	}
	return conn, nil
}

// peerProcess returns the uid, pid and executable of the process that
// listens on a Unix socket connection: peerCred, and the kernel's path of
// its running file.
func peerProcess(conn *net.UnixConn) (uid, pid int, exe string, err error) {
	if uid, pid, err = peerCred(conn); err != nil {
		return 0, 0, "", err
	}
	exe, err = processExe(pid)
	return uid, pid, exe, err
}

// untrustedSocketServer returns the pid of the process listening on this
// vault's socket when it is this user's but not a server this pvault trusts
// (verifyPeer): another build, such as the one still running after a
// reinstall, whose file may be gone. Only this user can listen in the private
// run dir, so it is this vault's server whatever it runs; lock stops it with
// a signal, which sends it nothing.
func untrustedSocketServer() (int, bool) {
	path := socketPath()
	if checkSocketDir(filepath.Dir(path)) != nil {
		return 0, false
	}
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return 0, false
	}
	defer c.Close()
	uid, pid, err := peerCred(c.(*net.UnixConn))
	if err != nil || uid != os.Getuid() || pid <= 1 || pid == os.Getpid() {
		return 0, false
	}
	return pid, verifyPeer(c) != nil
}

// verifyPeer checks that the process listening on conn is this user's and
// runs the same pvault binary, and build, as this one.
func verifyPeer(conn net.Conn) error {
	uid, pid, exe, err := peerProcess(conn.(*net.UnixConn))
	if err != nil {
		return errUnidentifiedServer(err)
	}
	if uid != os.Getuid() {
		return fmt.Errorf("the vault server runs as another user (uid %d)", uid)
	}
	return checkServerProcess(pid, exe)
}
