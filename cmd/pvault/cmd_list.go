package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

const listUsage = `usage: pvault list [category | member: | member:category]
  e.g. pvault list cie, pvault list leo: (everything about leo), pvault list leo:cie`

func cmdList() {
	path := "/vault/fields"
	member := ""
	needGrant("*", "read")
	if args := positionalArgs(os.Args[2:], listUsage, 0, 1); len(args) == 1 {
		switch arg := args[0]; {
		case strings.HasSuffix(arg, ":"):
			member = strings.TrimSuffix(arg, ":")
			if !vault.ValidMemberSlug(member) {
				fatal("invalid member %q\n%s", member, listUsage)
			}
			needGrant(member+":*", "read")
		case vault.ValidCategoryName(arg):
			path = "/vault/fields/category/" + arg
			needGrant(arg+".*", "read")
		default:
			fatal("invalid category %q: only letters, digits, underscore and hyphen, with an optional member: prefix", arg)
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

	if member != "" {
		// A full-access grant lists everyone; keep the member's fields.
		kept := fields[:0]
		for _, f := range fields {
			if m, _ := vault.SplitMember(f.ID); m == member {
				kept = append(kept, f)
			}
		}
		fields = kept
	}
	if len(fields) == 0 {
		fmt.Println("No fields found.")
		return
	}
	printFields(fields)
}
