package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/lovincyrus/personal-vault/internal/vault"
	"golang.org/x/term"
)

const backupUsage = "usage: pvault backup [--to <folder>]"
const restoreUsage = "usage: pvault restore <file.pvbackup> [--check] [--ask-key]"

// iCloudBackupDir is the suggested off-machine folder on macOS.
func iCloudBackupDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs", "pvault-backups")
}

func cmdBackup() {
	dir := ""
	if err := parseFlags(os.Args[2:], map[string]*string{"--to": &dir}); err != nil {
		fatal("%v\n%s", err, backupUsage)
	}

	body := map[string]string{}
	if dir != "" {
		// The server creates the folder once it has checked it.
		abs, err := filepath.Abs(dir)
		if err != nil {
			fatal("%v", err)
		}
		body["dir"] = abs
	}

	// Backups are session-only, so this asks for full access.
	resp, err := apiRequest("POST", "/vault/backup", body)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := apiResult(resp, &result); err != nil {
		if err.Error() == vault.ErrNoBackupDir.Error() {
			fatal("no backup folder set yet. To keep backups in iCloud Drive, run:\n  pvault backup --to %q", iCloudBackupDir())
		}
		fatal("%v", err)
	}

	fmt.Printf("Encrypted backup written to %s\n", result.Path)
	if dir != "" {
		fmt.Println("From now on the vault also backs itself up to this folder a minute after each change and when it locks.")
	}
	fmt.Println("Restoring needs your profile password and secret key, which are not in the backup.")
	fmt.Println("Check a backup with: pvault restore <file> --check")
}

func cmdRestore() {
	file, check, askKey := "", false, false
	for _, arg := range os.Args[2:] {
		switch {
		case arg == "--check":
			check = true
		case arg == "--ask-key":
			askKey = true
		case file == "" && !strings.HasPrefix(arg, "-"):
			file = arg
		default:
			fatal(restoreUsage)
		}
	}
	if file == "" {
		fatal(restoreUsage)
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		fatal("restore needs your profile password, which can only be entered in an interactive terminal")
	}

	pw, err := promptPassword("Profile password: ")
	if err != nil {
		fatal("reading password: %v", err)
	}
	promptKey := func() string {
		sk, err := promptPassword("Secret key (32 hex characters): ")
		if err != nil {
			fatal("reading secret key: %v", err)
		}
		return sk
	}
	// On this machine the secret key is on disk; on a new one it comes from
	// wherever it was kept. --ask-key types it anyway, to test that copy.
	sk, err := readSecretKey()
	typed := askKey || err != nil
	if typed {
		sk = promptKey()
	}
	dir := vaultDir()
	run := func(sk string) (*vault.BackupInfo, error) {
		if check {
			return vault.CheckBackup(file, pw, sk)
		}
		return vault.RestoreBackup(file, dir, pw, sk)
	}
	info, err := run(sk)
	if errors.Is(err, vault.ErrBackupUnreadable) && !typed {
		fmt.Fprintln(os.Stderr, "That backup doesn't open with this password and this machine's secret key.")
		fmt.Fprintln(os.Stderr, "If it was made with another secret key, enter it now (Ctrl-C to stop).")
		info, err = run(promptKey())
	}
	if err != nil {
		fatal("%v", err)
	}

	if check {
		fmt.Printf("Backup OK: made %s, %d fields, %d documents. It unlocks with this password and secret key.\n",
			info.Created.Local().Format("2006-01-02 15:04"), info.Fields, info.Documents)
		return
	}

	// A port isn't part of the backup: the restored vault gets one here.
	if _, err := ensurePort(dir); err != nil {
		fatal("restored, but %v", err)
	}
	fmt.Printf("Restored the backup made %s (%d fields, %d documents) into %s.\n",
		info.Created.Local().Format("2006-01-02 15:04"), info.Fields, info.Documents, dir)
	v := vaultFlag()
	fmt.Printf("Next: run 'pvault %sunlock'. If you use Touch ID, run 'pvault %stouchid enable' again,\n", v, v)
	fmt.Printf("and 'pvault %sbackup --to <folder>' if the backup folder isn't on this machine.\n", v)
}
