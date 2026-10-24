package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func cmdCreateServiceToken() {
	const usage = "usage: pvault create-service-token <consumer> --scope <patterns> [--ttl duration] [--max-sensitivity tier]\n" +
		"  example: pvault create-service-token tari-agent --scope \"identity.*,residenza.*\" --ttl 2h\n" +
		"  --scope is required; pass \"*\" explicitly for full access.\n" +
		"  --ttl defaults to 24h (max 2160h = 90 days).\n" +
		"  --max-sensitivity defaults to sensitive; use critical only for agents that must read critical fields."
	if len(os.Args) < 3 {
		fatal(usage)
	}

	consumer := os.Args[2]
	scope := ""
	ttl := "24h"
	maxSensitivity := ""

	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--scope":
			if i+1 < len(os.Args) {
				scope = os.Args[i+1]
				i++
			}
		case "--ttl":
			if i+1 < len(os.Args) {
				ttl = os.Args[i+1]
				i++
			}
		case "--max-sensitivity":
			if i+1 < len(os.Args) {
				maxSensitivity = os.Args[i+1]
				i++
			}
		}
	}
	if scope == "" {
		fatal(usage)
	}

	resp, err := apiRequest("POST", "/vault/tokens/service", map[string]string{
		"consumer":        consumer,
		"scope":           scope,
		"ttl":             ttl,
		"max_sensitivity": maxSensitivity,
	})
	if err != nil {
		fatal("request failed: %v", err)
	}

	var result struct {
		Token          string `json:"token"`
		MaxSensitivity string `json:"max_sensitivity"`
		ExpiresAt      string `json:"expires_at"`
	}
	if err := apiResult(resp, &result); err != nil {
		fatal("%v", err)
	}

	fmt.Printf("Service token created for %q\n", consumer)
	fmt.Printf("Token:           %s\n", result.Token)
	fmt.Printf("Scope:           %s\n", scope)
	fmt.Printf("Max sensitivity: %s\n", result.MaxSensitivity)
	fmt.Printf("Expires:         %s\n", result.ExpiresAt)
	fmt.Println("\nSave this token — it cannot be displayed again.")
}

func cmdListServiceTokens() {
	resp, err := apiRequest("GET", "/vault/tokens/service", nil)
	if err != nil {
		fatal("request failed: %v", err)
	}

	var tokens []struct {
		TokenPrefix    string `json:"token_prefix"`
		Consumer       string `json:"consumer"`
		Scope          string `json:"scope"`
		MaxSensitivity string `json:"max_sensitivity"`
		ExpiresAt      string `json:"expires_at"`
		CreatedAt      string `json:"created_at"`
	}
	if err := apiResult(resp, &tokens); err != nil {
		fatal("%v", err)
	}

	if len(tokens) == 0 {
		fmt.Println("No service tokens.")
		return
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(tokens)
}

func cmdRevokeServiceToken() {
	if len(os.Args) < 3 {
		fatal("usage: pvault revoke-service-token <token-or-prefix>\n  prefix: the token_prefix shown by list-service-tokens (at least 8 characters)")
	}
	token := os.Args[2]

	resp, err := apiRequest("DELETE", "/vault/tokens/service/"+token, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}

	var result struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	if err := apiResult(resp, &result); err != nil {
		fatal("%v", err)
	}

	fmt.Printf("Revoked %d token(s).\n", result.Count)
}
