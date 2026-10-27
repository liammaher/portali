//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// Here the private channel is a named pipe, not a Unix socket: the MCP server
// (Node) and fill.py (Python) can't use AF_UNIX on Windows. Pipe names are
// machine-wide and any account can create one, so the server gives its pipe a
// new random name and writes it to <vault dir>\run\pipe, in a folder only this
// user can enter (vault.RestrictDir). It keeps that file open while it serves,
// and Windows won't delete a file another process holds open: a client that
// can delete it knows the server is gone, and that the name may be someone
// else's by now (the MCP server and fill.py check this). The pipe's DACL lets
// only this user open it, and remote clients are rejected. This CLI also
// checks that the server is this user's and runs this pvault binary, as on
// Linux; like the Unix socket, this catches a stale or mismatched server, not
// a process of this user's that sets out to get around it.

// socketPath is the file naming the server's pipe.
func socketPath() string {
	return filepath.Join(vaultDir(), "run", "pipe")
}

var rePipeName = regexp.MustCompile(`^\\\\\.\\pipe\\pvault-[0-9a-f]{32}$`)

// listenSocket creates the server's pipe under a new random name and records
// the name in path, which it holds open until the listener is closed. A file
// there that can't be removed is held by another live server.
func listenSocket(path string) (net.Listener, error) {
	dir := filepath.Dir(path)
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	if err := vault.RestrictDir(dir); err != nil {
		return nil, fmt.Errorf("make %s private: %w", dir, err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("another vault server is already serving %s (%v)", vaultDir(), err)
	}
	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	name := `\\.\pipe\pvault-` + hex.EncodeToString(b[:])
	ln, err := winio.ListenPipe(name, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + me.User.Sid.String() + ")",
	})
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		ln.Close()
		return nil, err
	}
	if _, err := f.WriteString(name); err != nil {
		f.Close()
		os.Remove(path)
		ln.Close()
		return nil, err
	}
	return &pipeListener{Listener: ln, file: f}, nil
}

// pipeListener removes the file naming the pipe when it closes.
type pipeListener struct {
	net.Listener
	file *os.File
	once sync.Once
}

func (l *pipeListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() {
		l.file.Close()
		os.Remove(l.file.Name())
	})
	return err
}

// serverPipe is the name of this vault's server's pipe, from socketPath.
func serverPipe() (string, error) {
	data, err := os.ReadFile(socketPath())
	if err != nil {
		return "", errNoVaultSocket
	}
	name := strings.TrimSpace(string(data))
	if !rePipeName.MatchString(name) {
		return "", fmt.Errorf("%s doesn't name a vault server's pipe", socketPath())
	}
	return name, nil
}

// dialServerPipe connects to this vault's server's pipe, unverified.
func dialServerPipe(ctx context.Context) (net.Conn, error) {
	name, err := serverPipe()
	if err != nil {
		return nil, err
	}
	conn, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, fmt.Errorf("%s is not your vault server's pipe", name)
		}
		return nil, errNoVaultSocket
	}
	return conn, nil
}

// dialVaultServer connects to the vault's pipe and returns the connection
// only if this pvault binary, run by this user, is serving it.
func dialVaultServer(ctx context.Context) (net.Conn, error) {
	conn, err := dialServerPipe(ctx)
	if err != nil {
		if errors.Is(err, errNoVaultSocket) {
			return nil, err
		}
		return nil, fmt.Errorf("refusing to send your password: %w", err)
	}
	if err := verifyPeer(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("refusing to send your password: %w", err)
	}
	return conn, nil
}

// verifyPeer checks that the process serving the pipe conn is this user's
// and runs the same pvault binary as this one.
func verifyPeer(conn net.Conn) error {
	pid, err := pipeServerPID(conn)
	if err != nil {
		return errUnidentifiedServer(err)
	}
	mine, err := sameUser(pid)
	if err != nil {
		return errUnidentifiedServer(err)
	}
	if !mine {
		return errors.New("the vault server runs as another user")
	}
	exe, err := processExe(pid)
	if err != nil {
		return errUnidentifiedServer(err)
	}
	return checkServerProcess(pid, exe)
}

// untrustedSocketServer returns the pid of the process serving this vault's
// pipe when it is this user's but not a server this pvault trusts
// (verifyPeer), such as another build's; lock stops it over the pipe, which
// sends it nothing secret.
func untrustedSocketServer() (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := dialServerPipe(ctx)
	if err != nil {
		return 0, false
	}
	defer c.Close()
	pid, err := pipeServerPID(c)
	if err != nil || pid == os.Getpid() {
		return 0, false
	}
	if mine, err := sameUser(pid); err != nil || !mine {
		return 0, false
	}
	return pid, verifyPeer(c) != nil
}
