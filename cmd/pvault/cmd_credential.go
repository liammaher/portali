package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/lovincyrus/personal-vault/internal/filesync"
	"github.com/lovincyrus/personal-vault/internal/wincred"
)

const credentialUsage = `usage: pvault credential read <name> [--account <user>]
       pvault credential write <name> [--account <user>]`

// credentialNotFound is read's exit status for a missing credential, the one
// macOS's security tool uses (errSecItemNotFound), so the MCP server treats
// both alike.
const credentialNotFound = 44

// cmdCredential reads or writes a secret in Windows Credential Manager for
// what can't reach it itself: the MCP server (Node has no Credential Manager
// API) reads its token and the PEC app password through read. write takes the
// secret from the terminal, or one line of stdin, never the command line, so
// other processes can't see it there. Like the Keychain items pvault writes on
// a Mac, these are readable by the user's own programs.
func cmdCredential() {
	if runtime.GOOS != "windows" {
		fatal("pvault credential is for Windows Credential Manager; on a Mac the Keychain keeps these (security add-generic-password)")
	}
	if len(os.Args) < 4 {
		fatal(credentialUsage)
	}
	name := os.Args[3]
	account := ""
	if err := parseFlags(os.Args[4:], map[string]*string{"--account": &account}); err != nil {
		fatal("%v\n%s", err, credentialUsage)
	}
	if !keychainNamePattern.MatchString(name) {
		fatal("a credential name is 1-64 letters, digits, '.', '_' or '-' (not starting with '-')")
	}
	// A file source's sign-in (its refresh token reaches the files' content)
	// is pvault's own: it isn't handed out, or changed, here.
	if strings.HasPrefix(strings.ToLower(name), filesync.ServiceName("")) {
		fatal("%q holds a file source's sign-in, which pvault keeps to itself", name)
	}
	switch os.Args[2] {
	case "read":
		secret, user, err := wincred.Read(name)
		// A credential saved for someone else counts as none, as a Keychain
		// item of another account would.
		if errors.Is(err, wincred.ErrNotFound) || (err == nil && account != "" && !strings.EqualFold(user, account)) {
			fmt.Fprintf(os.Stderr, "no credential %q in Windows Credential Manager\n", name)
			os.Exit(credentialNotFound)
		}
		if err != nil {
			fatal("%v", err)
		}
		fmt.Println(secret)
	case "write":
		if account == "" {
			var err error
			if account, err = keychainAccount(); err != nil {
				fatal("%v", err)
			}
		}
		secret, err := readSecret()
		if err != nil {
			fatal("%v", err)
		}
		if !utf8.ValidString(secret) {
			fatal("the secret isn't valid text (UTF-8); nothing was saved")
		}
		if err := wincred.Write(name, account, secret); err != nil {
			fatal("%v", err)
		}
		if got, _, err := wincred.Read(name); err != nil || got != secret {
			fatal("Credential Manager didn't keep the secret as written (%v)", err)
		}
		fmt.Printf("Saved %q for %s in Windows Credential Manager.\n", name, account)
	default:
		fatal(credentialUsage)
	}
}

// readSecret asks for a secret in the terminal, without echo, or reads one
// line from stdin. Where stdin isn't a console (Git Bash's terminal, which
// would show what is typed) it says so first.
func readSecret() (string, error) {
	if term.IsTerminal(int(syscall.Stdin)) {
		s, err := promptPassword("Secret: ")
		if err == nil && s == "" {
			err = errors.New("no secret entered")
		}
		return s, err
	}
	fmt.Fprintln(os.Stderr, "Reading the secret from stdin, one line. Typed here it would show: pipe it in, or in Git Bash run 'winpty pvault credential write ...'.")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if line = strings.TrimRight(line, "\r\n"); line == "" {
		if err == nil {
			err = errors.New("no secret on stdin")
		}
		return "", fmt.Errorf("reading the secret: %w", err)
	}
	return line, nil
}
