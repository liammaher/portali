package main

import (
	"fmt"
	"os"
	"strconv"
)

const retentionUsage = `usage:
  pvault retention              Show the retention period
  pvault retention set <years>  Keep people's data this many years after their ruolo.data_fine;
                                'pvault expiring' then lists who is past it
  pvault retention unset        Remove the setting
There is no default: check the period that applies to your business first.`

func cmdRetention() {
	args := positionalArgs(os.Args[2:], retentionUsage, 0, 2)
	switch {
	case len(args) == 0:
		needGrant("*", "read")
		years, err := fetchRetention()
		if err != nil {
			fatal("%v", err)
		}
		if years == 0 {
			fmt.Println("No retention period set. Set one with '" + pvCmd("retention set <years>") + "'.")
			return
		}
		fmt.Printf("People's data is kept %d years after their ruolo.data_fine.\n", years)
	case len(args) == 2 && args[0] == "set":
		years, err := strconv.Atoi(args[1])
		if err != nil || years < 1 {
			fatal("years must be a whole number from 1\n%s", retentionUsage)
		}
		setRetention(years)
		fmt.Printf("Retention set to %d years after ruolo.data_fine. '%s' lists who is past it.\n", years, pvCmd("expiring"))
	case len(args) == 1 && args[0] == "unset":
		setRetention(0)
		fmt.Println("Retention period removed.")
	default:
		fatal(retentionUsage)
	}
}

// setRetention changes the setting; only your own session may.
func setRetention(years int) {
	needGrant("", "")
	resp, err := apiRequest("PUT", "/vault/retention", map[string]int{"years": years})
	if err != nil {
		fatal("request failed: %v", err)
	}
	if err := apiResult(resp, nil); err != nil {
		fatal("%v", err)
	}
}

// fetchRetention returns the retention period in years, 0 if not set.
func fetchRetention() (int, error) {
	resp, err := apiRequest("GET", "/vault/retention", nil)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	var out struct {
		Years int `json:"years"`
	}
	if err := apiResult(resp, &out); err != nil {
		return 0, err
	}
	return out.Years, nil
}
