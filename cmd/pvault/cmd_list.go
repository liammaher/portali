package main

import (
	"fmt"
	"os"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdList() {
	path := "/vault/fields"
	needGrant("*", "read")
	if len(os.Args) >= 3 {
		if !vault.ValidCategoryName(os.Args[2]) {
			fatal("invalid category %q: only letters, digits, underscore and hyphen", os.Args[2])
		}
		path = "/vault/fields/category/" + os.Args[2]
		needGrant(os.Args[2]+".*", "read")
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
