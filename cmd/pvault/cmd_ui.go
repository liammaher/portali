package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func cmdUI() {
	positionalArgs(os.Args[2:], "usage: pvault ui", 0, 0)
	url, err := uiURL()
	if err != nil {
		fatal("%v", err)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}

	if cmd != nil {
		if err := cmd.Start(); err == nil {
			fmt.Println("Opened vault UI in your browser — enter your profile password there.")
			return
		}
	}

	// Fallback: print URL
	fmt.Println("Open this URL in your browser:")
	fmt.Println()
	fmt.Println("  " + url)
	fmt.Println()
}

// uiURL returns the onboarding page's address once this vault's own server
// is known to answer there. The page asks for the profile password itself,
// so no token is ever put on a command line or in browser history, and it
// must not open on whatever else answers on the vault's port.
func uiURL() (string, error) {
	if err := requireUnlockedServer(); err != nil {
		return "", err
	}
	if err := checkVaultServer(); err != nil {
		return "", err
	}
	return serverAddr() + "/ui", nil
}
