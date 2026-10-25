package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/lovincyrus/personal-vault/internal/vault"
	"golang.org/x/term"
)

const setUsage = `usage: pvault set <id> [value]
  example: pvault set residenza.comune Napoli
  Leave out the value to type it hidden — use this for secrets such as
  spid.password or cie.pin so they never reach your shell history:
    pvault set spid.password`

func cmdSet() {
	if len(os.Args) < 3 {
		fatal(setUsage)
	}
	id := os.Args[2]
	// A well-formed ID first, so the Touch ID prompt names exactly one field.
	if err := vault.ValidateFieldID(id); err != nil {
		fatal("%v", err)
	}
	needGrant(id, "write")

	value, given, err := setValueArg(os.Args[3:])
	if err != nil {
		fatal("%v", err)
	}
	if given {
		// Only real secrets: spid.provider and spid.data_scadenza are personal
		// by default but not secret.
		if vault.IsCredentialField(id) {
			fmt.Fprintf(os.Stderr, "Warning: %s is a secret and the value you typed is now in your shell history.\n"+
				"Next time run 'pvault set %s' without a value to type it hidden.\n", id, id)
		}
	} else {
		// Authenticate first so the password prompt doesn't follow the value prompt.
		if _, err := sessionToken(); err != nil {
			fatal("%v", err)
		}
		v, err := promptValue(id)
		if err != nil {
			fatal("%v", err)
		}
		value = v
	}
	if value == "" {
		fatal("value is empty")
	}

	resp, err := apiRequest("PUT", "/vault/fields/"+id, map[string]string{
		"value": value,
	})
	if err != nil {
		fatal("request failed: %v", err)
	}

	var result struct {
		Status     string `json:"status"`
		Suggestion *struct {
			Canonical   string `json:"canonical"`
			Description string `json:"description"`
		} `json:"suggestion,omitempty"`
	}
	if err := apiResult(resp, &result); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Set %s\n", id)
	if result.Suggestion != nil {
		fmt.Fprintf(os.Stderr, "Hint: the recommended field is %s (%s)\n",
			result.Suggestion.Canonical, result.Suggestion.Description)
	}
}

// promptValue reads a value without echoing it in a terminal, or one line
// from stdin when piped (e.g. from a password manager CLI).
func promptValue(id string) (string, error) {
	if term.IsTerminal(int(syscall.Stdin)) {
		fmt.Fprintf(os.Stderr, "Value for %s (hidden): ", id)
		b, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading value from stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// setValueArg reads the value after the field ID, if one was given. Extra
// arguments are refused rather than joined, which would also swallow a
// mistyped option such as --sensitivity into the value.
func setValueArg(args []string) (value string, given bool, err error) {
	switch {
	case len(args) > 1:
		return "", false, fmt.Errorf("too many arguments: quote a value with spaces, e.g. pvault set residenza.indirizzo \"Via Roma 1\"\n%s", setUsage)
	case len(args) == 1 && strings.HasPrefix(args[0], "--"):
		return "", false, fmt.Errorf("unknown option %q: pvault set takes only <id> [value]; change a tier with pvault set-sensitivity", args[0])
	case len(args) == 1:
		return args[0], true, nil
	}
	return "", false, nil
}
