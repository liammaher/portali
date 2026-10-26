package main

import (
	"strings"
	"testing"
)

// pvault list <category> shows a personal field's version, never its value:
// an agent approved to list a category must not get its PIN or password.
func TestListHidesPersonalValues(t *testing.T) {
	touchIDVault(t, map[string]string{
		"spid.password": "S3cretSpid!",
		"cie.pin":       "12345678",
		"cie.numero":    "CA12345AB",
	})
	out := runCmd(t, cmdList, "list", "spid") + runCmd(t, cmdList, "list", "cie")
	for _, secret := range []string{"S3cretSpid!", "12345678"} {
		if strings.Contains(out, secret) {
			t.Errorf("list printed %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "CA12345AB") || !strings.Contains(out, "spid.password") {
		t.Errorf("list should still show standard values and personal fields' names:\n%s", out)
	}
}
