package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/term"
)

const touchIDUsage = `usage: pvault touchid enable|disable|status
  enable   save your profile password for Touch ID (asks for it once)
  disable  remove the saved password from the Keychain
  status   show whether Touch ID is set up`

func cmdTouchID() {
	if len(os.Args) < 3 {
		fatal(touchIDUsage)
	}
	if runtime.GOOS == "windows" {
		fatal("Touch ID is for Macs; on Windows, approve requests with Windows Hello: '%s'", pvCmd("hello enable"))
	}
	switch os.Args[2] {
	case "enable":
		cmdTouchIDEnable()
	case "disable":
		// Interactive only, like enable, so an agent can't switch it off.
		if !term.IsTerminal(int(syscall.Stdin)) {
			fatal("Touch ID can only be turned off from an interactive terminal")
		}
		if _, err := runTouchID("", "remove"); err != nil {
			fatal("%v", err)
		}
		fmt.Println("Touch ID turned off; the saved password was removed from the Keychain.")
	case "status":
		st := touchIDStatus()
		switch {
		case st.helper == "":
			fmt.Printf("Touch ID: not available (%s is not installed next to pvault)\n", touchIDHelperName)
		case !st.enrolled:
			fmt.Printf("Touch ID: off — run 'pvault %stouchid enable'\n", vaultFlag())
		default:
			fmt.Println("Touch ID: on")
		}
	default:
		fatal(touchIDUsage)
	}
}

// cmdTouchIDEnable checks the password, then hands it to the helper, which
// asks for a fingerprint and stores it. Only a person at a terminal can do
// this, so an agent can't enroll a password it learned some other way.
func cmdTouchIDEnable() {
	if _, ok := touchIDHelper(); !ok {
		fatal("%s is not installed next to pvault — run 'make build'", touchIDHelperName)
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		fatal("Touch ID can only be turned on from an interactive terminal")
	}
	pw, err := promptPassword("Profile password: ")
	if err != nil {
		fatal("reading password: %v", err)
	}
	sk, err := readSecretKey()
	if err != nil {
		fatal("%v", err)
	}
	// Checked against the database directly, so no grant is issued just
	// to test the password. Only a person at a terminal gets here.
	if err := checkCredentials(pw, sk); err != nil {
		fatal("%v", err)
	}
	// The helper sends the password only to a server running this binary.
	self, err := os.Executable()
	if err == nil {
		self, err = filepath.EvalSymlinks(self)
	}
	if err != nil {
		fatal("finding pvault: %v", err)
	}
	fmt.Println("Touch your fingerprint sensor to confirm…")
	if _, err := runTouchID(pw, "enroll", "--server", self); err != nil {
		fatal("%v", err)
	}
	fmt.Println("Touch ID is on. pvault will ask for your fingerprint instead of your password,")
	fmt.Println("and the prompt names exactly what each request can read or change.")
}
