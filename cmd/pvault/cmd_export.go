package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdExport() {
	includePersonal := false
	for _, arg := range os.Args[2:] {
		if arg != "--include-personal" {
			fatal("unknown option %q\nusage: pvault export [--include-personal]", arg)
		}
		includePersonal = true
	}

	needGrant("*", "read")
	resp, err := apiRequest("GET", "/vault/context", nil)
	if err != nil {
		fatal("request failed: %v", err)
	}

	var ctx vault.ContextBundle
	if err := apiResult(resp, &ctx); err != nil {
		fatal("%v", err)
	}

	// Personal fields (SPID login, CIE PIN/PUK, ...) are left out unless
	// asked for, so a routine `pvault export > backup.json` never writes them
	// to disk unencrypted.
	omitted := 0
	if !includePersonal {
		for cat, fields := range ctx.Categories {
			kept := fields[:0]
			for _, f := range fields {
				if f.Sensitivity == vault.TierPersonal {
					omitted++
					continue
				}
				kept = append(kept, f)
			}
			if len(kept) == 0 {
				delete(ctx.Categories, cat)
			} else {
				ctx.Categories[cat] = kept
			}
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	// A failed write (full disk, closed pipe) must not look like a good export.
	if err := enc.Encode(ctx); err != nil {
		fatal("writing export: %v", err)
	}
	switch {
	case omitted > 0:
		fmt.Fprintf(os.Stderr, "%d personal field(s) left out; add --include-personal to include them (unencrypted).\n", omitted)
	case includePersonal:
		fmt.Fprintln(os.Stderr, "Warning: output includes personal secrets unencrypted — don't save it anywhere synced or shared.")
	}
}
