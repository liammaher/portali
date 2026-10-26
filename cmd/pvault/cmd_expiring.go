package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdExpiring() {
	days := 90
	if args := positionalArgs(os.Args[2:], "usage: pvault expiring [days]", 0, 1); len(args) == 1 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 {
			fatal("usage: pvault expiring [days]")
		}
		days = n
	}

	needGrant("*", "read")
	resp, err := apiRequest("GET", "/vault/expiring?days="+strconv.Itoa(days), nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var items []vault.ExpiryInfo
	if err := apiResult(resp, &items); err != nil {
		fatal("%v", err)
	}
	defer retentionHint()
	if len(items) == 0 {
		fmt.Printf("Nothing expired or expiring in the next %d days.\n", days)
		return
	}
	width := 20
	for _, it := range items {
		width = max(width, len(expiryLabel(it)))
	}
	for _, it := range items {
		when := fmt.Sprintf("in %d days", it.DaysLeft)
		switch {
		case it.Kind == vault.ExpiryPraticheClosed:
			when = "their pratiche are closed: delete the scans unless still needed"
		case it.Kind == vault.ExpiryRetention && it.Expired:
			when = fmt.Sprintf("retention ended %d days ago: delete their data unless still needed", -it.DaysLeft)
		case it.Kind == vault.ExpiryRetention:
			when = fmt.Sprintf("retention ends in %d days", it.DaysLeft)
		case it.Expired:
			when = fmt.Sprintf("EXPIRED %d days ago", -it.DaysLeft)
		case it.DaysLeft == 0:
			when = "expires TODAY"
		}
		fmt.Printf("%-*s %s  %s\n", width, expiryLabel(it), it.ExpiresOn, when)
	}
}

// expiryLabel names the document and, for another person's, whose it is:
// "cie (leo)"; "data of rossi" for a retention entry, "cie scans (rossi)"
// for ID scans whose pratiche are closed.
func expiryLabel(it vault.ExpiryInfo) string {
	switch {
	case it.Kind == vault.ExpiryRetention:
		return "data of " + it.Person
	case it.Kind == vault.ExpiryPraticheClosed:
		return it.Document + " scans (" + it.Person + ")"
	case it.Person != "":
		return it.Document + " (" + it.Person + ")"
	}
	return it.Document
}

// retentionHint says, once, that a business vault without a retention period
// doesn't check people whose relationship ended.
func retentionHint() {
	if kind, err := serverKind(); err != nil || kind != vault.KindImpresa {
		return
	}
	if years, err := fetchRetention(); err == nil && years == 0 {
		fmt.Printf("No retention period set, so people whose ruolo.data_fine has passed aren't checked: see '%s'.\n", pvCmd("retention"))
	}
}
