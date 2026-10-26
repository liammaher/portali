package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

const listUsage = `usage: pvault list [category | slug: | slug:category]
  e.g. pvault list cie, pvault list leo: (everything about leo), pvault list leo:cie`

func cmdList() {
	path := "/vault/fields"
	person := ""
	needGrant("*", "read")
	if args := positionalArgs(os.Args[2:], listUsage, 0, 1); len(args) == 1 {
		switch arg := args[0]; {
		case strings.HasSuffix(arg, ":"):
			person = strings.TrimSuffix(arg, ":")
			if !vault.ValidPersonSlug(person) {
				fatal("invalid person %q\n%s", person, listUsage)
			}
			needGrant(person+":*", "read")
		case vault.ValidCategoryName(arg):
			path = "/vault/fields/category/" + arg
			needGrant(arg+".*", "read")
		default:
			fatal("invalid category %q: only letters, digits, underscore and hyphen, with an optional person: prefix", arg)
		}
	}

	resp, err := apiRequest("GET", path, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}

	var fields []vault.FieldInfo
	if err := apiResult(resp, &fields); err != nil {
		fatal("%v", err)
	}

	if person != "" {
		// A full-access grant lists everyone; keep the person's fields.
		kept := fields[:0]
		for _, f := range fields {
			if m, _ := vault.SplitPerson(f.ID); m == person {
				kept = append(kept, f)
			}
		}
		fields = kept
	}
	// Secrets stay off the screen, as in 'pvault person show': a category
	// read returns values, and 'pvault get <id>' shows one on purpose.
	for i := range fields {
		if fields[i].Sensitivity == vault.TierPersonal {
			fields[i].Value = ""
		}
	}
	if len(fields) == 0 {
		fmt.Println("No fields found.")
		return
	}
	printFields(fields)
}
