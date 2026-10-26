package main

import (
	"fmt"
	"os"
	"strconv"
)

const retentionUsage = `usage:
  pvault retention                Show the retention settings
  pvault retention set <years>    Keep people's data this many years after their ruolo.data_fine;
                                  'pvault expiring' then lists who is past it
  pvault retention unset          Remove the setting
  pvault retention scans <days>   Keep a client's ID and procura scans this many days after the
                                  last pratica naming them closes (0: right away); 'pvault expiring'
                                  then lists them
  pvault retention scans unset    Remove that setting
There are no defaults: check the periods that apply to your business first.`

func cmdRetention() {
	args := positionalArgs(os.Args[2:], retentionUsage, 0, 2)
	switch {
	case len(args) == 0:
		needGrant("*:ruolo.data_fine", "read") // the settings are about these; any grant may read them
		r, err := fetchRetention()
		if err != nil {
			fatal("%v", err)
		}
		if r.Years == 0 {
			fmt.Println("No retention period set. Set one with '" + pvCmd("retention set <years>") + "'.")
		} else {
			fmt.Printf("People's data is kept %d years after their ruolo.data_fine.\n", r.Years)
		}
		if r.ScanDays == nil {
			fmt.Println("No period set for clients' ID scans. Set one with '" + pvCmd("retention scans <days>") + "'.")
		} else {
			fmt.Printf("Clients' ID and procura scans are kept %d days after the last pratica naming them closes.\n", *r.ScanDays)
		}
	case len(args) == 2 && args[0] == "set":
		years, err := strconv.Atoi(args[1])
		if err != nil || years < 1 {
			fatal("years must be a whole number from 1\n%s", retentionUsage)
		}
		setRetention(map[string]int{"years": years})
		fmt.Printf("Retention set to %d years after ruolo.data_fine. '%s' lists who is past it.\n", years, pvCmd("expiring"))
	case len(args) == 1 && args[0] == "unset":
		setRetention(map[string]int{"years": 0})
		fmt.Println("Retention period removed.")
	case len(args) == 2 && args[0] == "scans" && args[1] == "unset":
		setRetention(map[string]int{"scan_days": -1})
		fmt.Println("Period for clients' ID scans removed.")
	case len(args) == 2 && args[0] == "scans":
		days, err := strconv.Atoi(args[1])
		if err != nil || days < 0 {
			fatal("days must be a whole number from 0\n%s", retentionUsage)
		}
		setRetention(map[string]int{"scan_days": days})
		fmt.Printf("Clients' ID and procura scans kept %d days after their last pratica closes. '%s' lists them.\n", days, pvCmd("expiring"))
	default:
		fatal(retentionUsage)
	}
}

// setRetention changes a setting; only your own session may.
func setRetention(body map[string]int) {
	needGrant("", "")
	resp, err := apiRequest("PUT", "/vault/retention", body)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if err := apiResult(resp, nil); err != nil {
		fatal("%v", err)
	}
}

// retention is the vault's retention settings: Years is 0 and ScanDays nil
// when not set.
type retention struct {
	Years    int  `json:"years"`
	ScanDays *int `json:"scan_days"`
}

// fetchRetention returns the retention settings.
func fetchRetention() (retention, error) {
	resp, err := apiRequest("GET", "/vault/retention", nil)
	if err != nil {
		return retention{}, fmt.Errorf("request failed: %w", err)
	}
	var out retention
	if err := apiResult(resp, &out); err != nil {
		return retention{}, err
	}
	return out, nil
}
