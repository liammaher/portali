//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// The profile password and secret key only travel over a Unix socket in the
// vault dir, never over TCP. Anything can listen on 127.0.0.1:7200 while the
// real server is down; only a process that can write to <vault dir>/run can
// put a socket there. That private dir is the protection: it stops port
// squatters and agents kept out of the vault dir. The client also checks the
// listener's uid and that it runs this pvault binary (peer_*.go), which
// catches a stale or mismatched server, but a process that can already write
// to the vault dir can get around that check.

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

var errNoVaultSocket = errors.New("this vault's server isn't running")

// verifyPeer checks that the process listening on conn is this user's and
// runs the same pvault binary, and build, as this one.
func verifyPeer(conn net.Conn) error {
	uid, pid, exe, err := peerProcess(conn.(*net.UnixConn))
	if err != nil {
		return fmt.Errorf("cannot identify the vault server (%v) — run 'pvault lock', then 'pvault unlock'", err)
	}
	if uid != os.Getuid() {
		return fmt.Errorf("the vault server runs as another user (uid %d)", uid)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !samePath(exe, self) {
		return fmt.Errorf("the vault server is %s, not this pvault (%s) — stop it with '%s lock', then run 'pvault unlock'", exe, self, exe)
	}
	if err := checkSameBuild(pid); err != nil {
		return fmt.Errorf("%w (pvault was rebuilt or reinstalled since it started) — run 'pvault lock', then 'pvault unlock'", err)
	}
	return nil
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// socketClient sends requests only to a verified vault server, one
// connection per request so each one is checked.
var socketClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext:       func(ctx context.Context, _, _ string) (net.Conn, error) { return dialVaultServer(ctx) },
		DisableKeepAlives: true,
	},
}

// credentialRequest sends a request carrying the profile password to the
// verified vault server. Its Host is loopback, as the server requires.
func credentialRequest(method, path string, body any) (*http.Response, error) {
	resp, err := doRequestWith(socketClient, "http://localhost", method, path, body, "")
	// Drop net/http's `Post "http://localhost/...":` prefix.
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return resp, err
}

// checkVaultServer verifies the vault server before the user is asked for a
// password or fingerprint that would go to it.
func checkVaultServer() error {
	resp, err := credentialRequest("GET", "/vault/status", nil)
	if err != nil {
		// A server from before the socket, or a program squatting the port.
		if errors.Is(err, errNoVaultSocket) && portHasVault() {
			return fmt.Errorf("%w: the server on %s is an older pvault or another program — run 'pvault lock', then 'pvault unlock'", errNoVaultSocket, serverAddr())
		}
		return err
	}
	defer resp.Body.Close()
	// Tokens and the UI's password go to VAULT_ADDR over TCP, so it must be
	// the server just verified, not something else on another port or on
	// localhost's other address.
	var status struct {
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status); err != nil {
		return fmt.Errorf("vault server status: %w", err)
	}
	if u, err := url.Parse(serverAddr()); err != nil || u.Host != status.Addr {
		return fmt.Errorf("VAULT_ADDR %s isn't this vault's server, which listens on %s — set VAULT_ADDR=http://%s or unset it", serverAddr(), status.Addr, status.Addr)
	}
	return nil
}

// isVaultServerProcess reports whether pid runs this pvault binary, by the
// kernel's path of its executable (which survives a reinstall with rm + cp),
// so a planted PID file can't get another process signalled.
func isVaultServerProcess(pid int) bool {
	exe, err := processExe(pid)
	if err != nil {
		return false
	}
	self, err := os.Executable()
	return err == nil && samePath(exe, self)
}
