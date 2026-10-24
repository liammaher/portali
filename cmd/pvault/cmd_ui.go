package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

func cmdUI() {
	// Verify vault is reachable before asking for the password
	resp, err := publicRequest("GET", "/vault/status", nil)
	if err != nil {
		fatal("cannot reach vault server — is it running? Try 'pvault unlock'")
	}
	resp.Body.Close()

	// The page gets its own short-lived grant; rerun 'pvault ui' after it expires.
	token, err := promptGrant("15m")
	if err != nil {
		fatal("%v", err)
	}

	url := serverAddr() + "/ui#token=" + token

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
			fmt.Println("Opened vault UI in your browser.")
			return
		}
	}

	// Fallback: print URL
	fmt.Println("Open this URL in your browser:")
	fmt.Println()
	fmt.Println("  " + url)
	fmt.Println()
}
