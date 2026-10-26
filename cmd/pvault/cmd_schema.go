package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdSchema() {
	jsonFlag := false
	for _, arg := range os.Args[2:] {
		if arg != "--json" {
			fatal("unknown option %q\nusage: pvault schema [--json]", arg)
		}
		jsonFlag = true
	}

	if jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(vault.RecommendedSchema)
		return
	}

	fmt.Println("Recommended Vault Schema")
	fmt.Println("========================")
	fmt.Println("A family member's fields use the same IDs with their slug in front, e.g. leo:cie.numero.")
	fmt.Println()
	for _, cat := range vault.RecommendedSchema.Categories {
		fmt.Printf("%s — %s\n", cat.Name, cat.Description)
		if cat.Scope == vault.ScopeOwner {
			fmt.Println("  (yours only: family members don't have this category)")
		}
		if len(cat.Fields) == 0 {
			fmt.Println("  (user-defined fields)")
		}
		for _, f := range cat.Fields {
			fmt.Printf("  %-35s %s", f.ID, f.Description)
			if f.Sensitivity != "standard" {
				fmt.Printf(" [%s]", f.Sensitivity)
			}
			switch f.For {
			case vault.ForPerson:
				fmt.Print(" [family members only: <slug>:" + f.ID + "]")
			case vault.ForOwner:
				fmt.Print(" [yours only]")
			}
			fmt.Println()
		}
		fmt.Println()
	}
}
