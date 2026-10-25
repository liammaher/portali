package main

import (
	"fmt"
	"os"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdList() {
	path := "/vault/fields"
	needGrant("*", "read")
	if args := positionalArgs(os.Args[2:], "usage: pvault list [category]", 0, 1); len(args) == 1 {
		if !vault.ValidCategoryName(args[0]) {
			fatal("invalid category %q: only letters, digits, underscore and hyphen", args[0])
		}
		path = "/vault/fields/category/" + args[0]
		needGrant(args[0]+".*", "read")
	}

	resp, err := apiRequest("GET", path, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}

	var fields []vault.FieldInfo
	if err := apiResult(resp, &fields); err != nil {
		fatal("%v", err)
	}

	if len(fields) == 0 {
		fmt.Println("No fields found.")
		return
	}

	for _, f := range fields {
		sens := ""
		if f.Sensitivity != "" && f.Sensitivity != "standard" {
			sens = fmt.Sprintf(" [%s]", f.Sensitivity)
		}
		if f.Value != "" {
			fmt.Printf("%-35s %s%s\n", f.ID, f.Value, sens)
		} else {
			fmt.Printf("%-35s (v%d)%s\n", f.ID, f.Version, sens)
		}
	}
}
