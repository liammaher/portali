package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

func cmdUI() {
	if err := requireUnlockedServer(); err != nil {
		fatal("%v", err)
	}

	// The page asks for the profile password itself, so no token is ever put
	// on a command line or in browser history where other processes can see it.
	url := serverAddr() + "/ui"

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
