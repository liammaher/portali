package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/term"
)

const helloUsage = `usage: pvault hello enable|disable|status`

// cmdHello turns Windows Hello approvals on or off for this vault. The vault
// server keeps the setting and shows the prompts itself, so nothing secret is
// stored for it; switching it needs your own session (the typed password, or
// full access you approve), from an interactive terminal.
func cmdHello() {
	if len(os.Args) != 3 {
		fatal(helloUsage)
	}
	if runtime.GOOS != "windows" {
		fatal("Windows Hello is for Windows; on a Mac, approve requests with Touch ID: '%s'", pvCmd("touchid enable"))
	}
	switch os.Args[2] {
	case "enable", "disable":
		on := os.Args[2] == "enable"
		if !term.IsTerminal(int(syscall.Stdin)) {
			fatal("Windows Hello can only be turned on or off from an interactive terminal")
		}
		needGrant("", "") // only your own session may switch it
		resp, err := apiRequest("POST", "/vault/hello", map[string]bool{"enabled": on})
		if err != nil {
			fatal("%v", err)
		}
		if err := apiResult(resp, nil); err != nil {
			fatal("%v", err)
		}
		if on {
			fmt.Println("Windows Hello is on. pvault will ask for your face, fingerprint or PIN instead of your password,")
			fmt.Println("and the prompt names exactly what each request can read or change.")
		} else {
			fmt.Println("Windows Hello turned off: pvault will ask for your password.")
		}
	case "status":
		if err := checkVaultServer(); err != nil {
			if errors.Is(err, errNoVaultSocket) {
				fmt.Printf("Windows Hello: unknown (the vault server isn't running — run '%s')\n", pvCmd("unlock"))
				return
			}
			fatal("%v", err)
		}
		if verifiedApprover == "hello" {
			fmt.Println("Windows Hello: on")
		} else {
			fmt.Printf("Windows Hello: off — run '%s'\n", pvCmd("hello enable"))
		}
	default:
		fatal(helloUsage)
	}
}
