package main

import (
	"fmt"
	"os"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdSetSensitivity() {
	args := positionalArgs(os.Args[2:], "usage: pvault set-sensitivity <id> <tier>\n  tiers: public, standard, sensitive, critical, personal (never readable by agents)", 2, 2)
	id, tier := args[0], args[1]
	if err := vault.ValidateFieldID(id); err != nil {
		fatal("%v", err)
	}

	valid := map[string]bool{"public": true, "standard": true, "sensitive": true, "critical": true, "personal": true}
	if !valid[tier] {
		fatal("invalid tier %q (must be public, standard, sensitive, critical, or personal)", tier)
	}

	resp, err := apiRequest("PUT", "/vault/sensitivity/"+id, map[string]string{
		"tier": tier,
	})
	if err != nil {
		fatal("request failed: %v", err)
	}

	var result map[string]string
	if err := apiResult(resp, &result); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Set %s sensitivity to %s\n", id, tier)
}
