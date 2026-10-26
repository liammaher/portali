package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdStatus() {
	positionalArgs(os.Args[2:], "usage: pvault status", 0, 0)
	if name := vaultName(); name != "" {
		fmt.Printf("Vault:   %s (%s)\n", name, vaultDir())
	}
	status, err := serverStatus()
	if errors.Is(err, errServerDown) {
		fmt.Printf("Vault is locked (server not running; it listens on %s when unlocked).\n", serverAddr())
		return
	}
	if err != nil {
		fatal("%v", err)
	}

	if !status.Initialized {
		fmt.Printf("Vault is not initialized. Run 'pvault %sinit' first.\n", vaultFlag())
		return
	}

	if status.Locked {
		fmt.Println("Status:  locked")
	} else {
		fmt.Println("Status:  unlocked")
	}
	if status.Kind == vault.KindImpresa {
		fmt.Println("Kind:    business (impresa)")
	} else {
		fmt.Println("Kind:    personal")
	}
	fmt.Printf("Server:  %s\n", serverAddr())
	fmt.Printf("Fields:  %d\n", status.FieldCount)
	if b := status.Backup; b != nil {
		switch {
		case !b.Configured:
			fmt.Printf("Backup:  off — run 'pvault %sbackup --to <folder>'\n", vaultFlag())
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
		cats := make([]string, 0, len(status.Categories))
		width := 20
		for cat := range status.Categories {
			cats = append(cats, cat)
			width = max(width, len(cat))
		}
		sort.Strings(cats)
		for _, cat := range cats {
			fmt.Printf("  %-*s %d fields\n", width, cat, status.Categories[cat])
		}
		if status.Kind == vault.KindImpresa {
			fmt.Printf("Only the business's own data is counted here; see 'pvault %sperson list' for its people.\n", vaultFlag())
		} else {
			fmt.Printf("Family members aren't counted here; see 'pvault %sperson list'.\n", vaultFlag())
		}
	}
}
