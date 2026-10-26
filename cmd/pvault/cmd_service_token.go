package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

func cmdCreateServiceToken() {
	const usage = "usage: pvault create-service-token <consumer> --scope <patterns> [--access read|append|write] [--ttl duration] [--max-sensitivity tier]\n" +
		"  example: pvault create-service-token tari-agent --scope \"identity.*,residenza.*\" --ttl 2h\n" +
		"  --scope is required; pass \"*\" explicitly for full access (you and your family members).\n" +
		"    category.* and category.field are yours only; leo:* is everything about family member leo,\n" +
		"    leo:cie.* one of leo's categories, and *:identity.* that category for you and every person.\n" +
		"  --ttl defaults to 24h (max 2160h = 90 days).\n" +
		"  --access defaults to read: the agent can only read. append also lets it add new fields and\n" +
		"    documents (e.g. archive a receipt) but never overwrite or delete; write allows everything.\n" +
		"  --max-sensitivity defaults to sensitive; use critical only for agents that must read critical fields.\n" +
		"  --keychain <service> (macOS) saves the token in your Keychain under that service name instead of\n" +
		"    printing it, e.g. --keychain pvault-mcp for the MCP server's VAULT_TOKEN_COMMAND."
	if len(os.Args) < 3 {
		fatal(usage)
	}

	consumer := os.Args[2]
	scope := ""
	ttl := "24h"
	maxSensitivity := ""
	access := ""
	keychain := ""

	if err := parseFlags(os.Args[3:], map[string]*string{
		"--scope": &scope, "--ttl": &ttl, "--max-sensitivity": &maxSensitivity,
		"--access": &access, "--keychain": &keychain,
	}); err != nil {
		fatal("%v\n%s", err, usage)
	}
	if scope == "" {
		fatal(usage)
	}
	if keychain != "" {
		if err := checkKeychain(keychain); err != nil {
			fatal("%v", err)
		}
	}

	resp, err := apiRequest("POST", "/vault/tokens/service", map[string]string{
		"consumer":        consumer,
		"scope":           scope,
		"ttl":             ttl,
		"max_sensitivity": maxSensitivity,
		"access":          access,
	})
	if err != nil {
		fatal("request failed: %v", err)
	}

	var result struct {
		Token          string `json:"token"`
		MaxSensitivity string `json:"max_sensitivity"`
		Access         string `json:"access"`
		ExpiresAt      string `json:"expires_at"`
	}
	if err := apiResult(resp, &result); err != nil {
		fatal("%v", err)
	}

	stored := false
	previous := ""
	var keychainErr error
	if keychain != "" {
		// Read the old token first: once replaced it's no longer reachable
		// through the Keychain, so it should stop working too.
		previous, _ = readKeychain(keychain)
		if keychainErr = storeInKeychain(keychain, result.Token); keychainErr == nil {
			stored = true
		}
	}

	fmt.Printf("Service token created for %q\n", consumer)
	if stored {
		fmt.Printf("Keychain:        saved as service %q\n", keychain)
		if previous != "" && previous != result.Token {
			fmt.Printf("Previous token:  %s\n", revokeReplacedToken(previous))
		}
	} else {
		fmt.Printf("Token:           %s\n", result.Token)
	}
	fmt.Printf("Scope:           %s\n", scope)
	fmt.Printf("Max sensitivity: %s\n", result.MaxSensitivity)
	fmt.Printf("Access:          %s\n", result.Access)
	fmt.Printf("Expires:         %s\n", result.ExpiresAt)
	if !stored {
		fmt.Println("\nSave this token — it cannot be displayed again.")
	}
	if keychainErr != nil {
		// The token exists and is printed above, but whatever reads the
		// Keychain won't see it: fail so callers don't assume it worked.
		fatal("Keychain save failed: %v", keychainErr)
	}
}

// revokeReplacedToken revokes the token a --keychain save just replaced and
// describes the outcome for the summary.
func revokeReplacedToken(previous string) string {
	resp, err := apiRequest("DELETE", "/vault/tokens/service/"+tokenRef(previous), nil)
	if err != nil {
		return fmt.Sprintf("NOT revoked (%v); check `pvault list-service-tokens`", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return "already expired or revoked"
	}
	if err := apiResult(resp, nil); err != nil {
		return fmt.Sprintf("NOT revoked (%v); check `pvault list-service-tokens`", err)
	}
	return "revoked"
}

func cmdListServiceTokens() {
	positionalArgs(os.Args[2:], "usage: pvault list-service-tokens", 0, 0)
	resp, err := apiRequest("GET", "/vault/tokens/service", nil)
	if err != nil {
		fatal("request failed: %v", err)
	}

	var tokens []struct {
		TokenPrefix    string `json:"token_prefix"`
		Consumer       string `json:"consumer"`
		Scope          string `json:"scope"`
		MaxSensitivity string `json:"max_sensitivity"`
		Access         string `json:"access"`
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
	ref := positionalArgs(os.Args[2:], "usage: pvault revoke-service-token <token-or-prefix>\n  prefix: the token_prefix shown by list-service-tokens (at least 8 characters)", 1, 1)[0]
	// A full token is sent as its hash, so the raw token never goes in a URL.
	if len(ref) == 64 {
		ref = tokenRef(ref)
	}

	resp, err := apiRequest("DELETE", "/vault/tokens/service/"+url.PathEscape(ref), nil)
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
