package main

import (
	"errors"
	"fmt"
	"os"
	"time"
)

func cmdStatus() {
	positionalArgs(os.Args[2:], "usage: pvault status", 0, 0)
	status, err := serverStatus()
	if errors.Is(err, errServerDown) {
		fmt.Println("Vault is locked (server not running).")
		return
	}
	if err != nil {
		fatal("%v", err)
	}

	if !status.Initialized {
		fmt.Println("Vault is not initialized. Run 'pvault init' first.")
		return
	}

	if status.Locked {
		fmt.Println("Status:  locked")
	} else {
		fmt.Println("Status:  unlocked")
	}
	fmt.Printf("Fields:  %d\n", status.FieldCount)
	if b := status.Backup; b != nil {
		switch {
		case !b.Configured:
			fmt.Println("Backup:  off — run 'pvault backup --to <folder>'")
		case b.Error != "":
			fmt.Printf("Backup:  FAILING — %s\n", b.Error)
		case b.Last == "":
			fmt.Println("Backup:  folder set, no backup yet")
		default:
			last := b.Last
			if t, err := time.Parse(time.RFC3339, b.Last); err == nil {
				last = t.Local().Format("2006-01-02 15:04")
			}
			fmt.Printf("Backup:  last %s\n", last)
		}
	}
	if len(status.Categories) > 0 {
		fmt.Println("Categories:")
		for cat, count := range status.Categories {
			fmt.Printf("  %-20s %d fields\n", cat, count)
		}
	}
}
