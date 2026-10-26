package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// Named vaults. Besides the default vault (~/.pvault), each vault is a
// sibling directory ~/.pvault-<name> (e.g. a business vault, ~/.pvault-studio)
// with its own server, socket, port and Touch ID enrollment. Every command
// takes --vault <name> as its first argument, or PVAULT_VAULT.

const defaultPort = 7200

// Ports named vaults are given, one each, at init.
const (
	firstNamedPort = 7201
	lastNamedPort  = 7299
)

// selectVault reads a leading --vault <name> (or PVAULT_VAULT) and turns it
// into VAULT_DIR for this process and every one it starts (serve, the Touch
// ID helper, which only read VAULT_DIR), returning the remaining arguments.
func selectVault(args []string) ([]string, error) {
	name, fromFlag := "", false
	if len(args) >= 2 && (args[1] == "--vault" || strings.HasPrefix(args[1], "--vault=")) {
		if v, ok := strings.CutPrefix(args[1], "--vault="); ok {
			name = v
			args = append([]string{args[0]}, args[2:]...)
		} else {
			if len(args) < 3 {
				return nil, errors.New("--vault needs a name, e.g. pvault --vault studio status")
			}
			name = args[2]
			args = append([]string{args[0]}, args[3:]...)
		}
		fromFlag = true
	} else if v := os.Getenv("PVAULT_VAULT"); v != "" {
		name = v
	}
	os.Unsetenv("PVAULT_VAULT")
	if name == "" {
		return args, nil
	}
	if !vault.ValidPersonSlug(name) {
		return nil, fmt.Errorf("invalid vault name %q: lower-case letters, digits and hyphens, starting with a letter", name)
	}
	dir := namedVaultDir(name)
	if d := os.Getenv("VAULT_DIR"); d != "" && filepath.Clean(d) != dir {
		what := "PVAULT_VAULT"
		if fromFlag {
			what = "--vault"
		}
		return nil, fmt.Errorf("%s %s means %s, but VAULT_DIR is %s: use one or the other", what, name, dir, d)
	}
	os.Setenv("VAULT_DIR", dir)
	return args, nil
}

// homeDir is the user's home directory.
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// namedVaultDir is the directory of the vault called name.
func namedVaultDir(name string) string {
	return filepath.Join(homeDir(), ".pvault-"+name)
}

// vaultName names the current vault for messages: "" for the default one,
// the name for ~/.pvault-<name>, or the directory for any other VAULT_DIR.
func vaultName() string {
	dir := filepath.Clean(vaultDir())
	if dir == filepath.Join(homeDir(), ".pvault") {
		return ""
	}
	if filepath.Dir(dir) == homeDir() {
		if name, ok := strings.CutPrefix(filepath.Base(dir), ".pvault-"); ok && vault.ValidPersonSlug(name) {
			return name
		}
	}
	return dir
}

// pvCmd is a pvault command line for messages, with --vault when the
// current vault needs it: pvCmd("unlock") is "pvault --vault studio unlock".
func pvCmd(args string) string {
	return "pvault " + vaultFlag() + args
}

// vaultFlag is the --vault argument that reaches the current vault, for
// commands printed in messages ("" for the default vault).
func vaultFlag() string {
	if name := vaultName(); name != "" && !strings.ContainsRune(name, filepath.Separator) {
		return "--vault " + name + " "
	}
	return ""
}

// portPath is the file holding the vault's TCP port. It is plain text and
// unauthenticated on purpose: the CLI reads it without opening vault.db, and
// checkVaultServer still checks that the server on the socket listens there.
func portPath(dir string) string {
	return filepath.Join(dir, "port")
}

// readPort returns the port in dir's port file; ok is false when there is
// none, as for vaults from before named vaults (which use 7200).
func readPort(dir string) (port int, ok bool, err error) {
	data, err := os.ReadFile(portPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	port, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port < 1024 || port > 65535 {
		return 0, false, fmt.Errorf("%s holds %q, not a port number", portPath(dir), strings.TrimSpace(string(data)))
	}
	return port, true, nil
}

// vaultPort is the current vault's port: its port file, else 7200.
func vaultPort() int {
	port, ok, err := readPort(vaultDir())
	if err != nil {
		fatal("%v", err)
	}
	if !ok {
		return defaultPort
	}
	return port
}

// writePort saves dir's port file.
func writePort(dir string, port int) error {
	return os.WriteFile(portPath(dir), []byte(strconv.Itoa(port)+"\n"), 0600)
}

// pickPort chooses the port for a new vault in dir: 7200 for the default
// vault, else the lowest of 7201-7299 that no other vault has and nothing is
// listening on.
func pickPort(dir string) (int, error) {
	if filepath.Clean(dir) == filepath.Join(homeDir(), ".pvault") {
		return defaultPort, nil
	}
	taken := map[int]bool{defaultPort: true}
	others, _ := filepath.Glob(filepath.Join(homeDir(), ".pvault-*"))
	for _, other := range append(others, filepath.Join(homeDir(), ".pvault")) {
		if filepath.Clean(other) == filepath.Clean(dir) {
			continue
		}
		if port, ok, _ := readPort(other); ok {
			taken[port] = true
		}
	}
	for port := firstNamedPort; port <= lastNamedPort; port++ {
		if taken[port] {
			continue
		}
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port in %d-%d for a new vault", firstNamedPort, lastNamedPort)
}

// ensurePort gives dir a port file if it has none (a new or restored vault).
func ensurePort(dir string) (int, error) {
	if port, ok, err := readPort(dir); err != nil || ok {
		return port, err
	}
	port, err := pickPort(dir)
	if err != nil {
		return 0, err
	}
	return port, writePort(dir, port)
}

// verifiedKind is the kind of vault (persona, impresa) the server reported
// the last time checkVaultServer verified it, "" before.
var verifiedKind string

// serverKind returns the vault server's kind: as checkVaultServer verified
// it, or on platforms without the socket check, as its status says.
func serverKind() (string, error) {
	if verifiedKind == "" {
		if err := checkVaultServer(); err != nil {
			return "", err
		}
	}
	if verifiedKind == "" {
		status, err := serverStatus()
		if err != nil {
			return "", err
		}
		verifiedKind = status.Kind
		if verifiedKind == "" {
			verifiedKind = vault.KindPersona
		}
	}
	return verifiedKind, nil
}
