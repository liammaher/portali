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
	if len(items) == 0 {
		fmt.Printf("Nothing expired or expiring in the next %d days.\n", days)
		return
	}
	width := 20
	for _, it := range items {
		width = max(width, len(it.Document))
	}
	for _, it := range items {
		when := fmt.Sprintf("in %d days", it.DaysLeft)
		switch {
		case it.Expired:
			when = fmt.Sprintf("EXPIRED %d days ago", -it.DaysLeft)
		case it.DaysLeft == 0:
			when = "expires TODAY"
		}
		fmt.Printf("%-*s %s  %s\n", width, it.Document, it.ExpiresOn, when)
	}
}
