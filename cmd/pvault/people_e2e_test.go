package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestPeople_EndToEnd runs the person commands with Touch ID grants against a
// real server: deleting a person's fields one grant at a time, the last one
// too (the person stays, and the list shows them), a token naming nobody, and
// removal.
func TestPeople_EndToEnd(t *testing.T) {
	v, argsFile := touchIDVault(t, map[string]string{
		"leo:famiglia.relazione":  "figlio",
		"leo:identity.first_name": "Leo",
		"leo:cie.numero":          "CA12345AB",
	})
	addr := os.Getenv("VAULT_ADDR")

	// The regression: deleting one of leo's fields with a one-field grant.
	runCmd(t, cmdDelete, "delete", "leo:cie.numero")
	if f, _ := v.FieldMeta("leo:cie.numero"); f != nil {
		t.Fatal("leo:cie.numero still there")
	}
	// His last rows go too, and he stays a person.
	runCmd(t, cmdDelete, "delete", "leo:identity.first_name")
	runCmd(t, cmdDelete, "delete", "leo:famiglia.relazione")
	if out := runCmd(t, cmdPerson, "person", "list"); !strings.Contains(out, "leo") {
		t.Errorf("person list after leo's last row went:\n%s", out)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--scope leo:cie.numero --access write") {
		t.Errorf("delete didn't ask for a one-field grant:\n%s", args)
	}

	// /ui/fields over real TCP, without auth.
	resp, err := http.Get(addr + "/ui/fields")
	if err != nil {
		t.Fatal(err)
	}
	var holders struct {
		Fields map[string]struct{ Owner, Person bool }
	}
	json.NewDecoder(resp.Body).Decode(&holders)
	resp.Body.Close()
	if resp.StatusCode != 200 || !holders.Fields["famiglia.relazione"].Person || holders.Fields["famiglia.relazione"].Owner {
		t.Errorf("/ui/fields: %d %+v", resp.StatusCode, holders.Fields["famiglia.relazione"])
	}

	// A service token can't name someone who isn't there.
	grant := func() string {
		body, _ := json.Marshal(map[string]string{"password": "test-password", "ttl": "60s"})
		r, err := http.Post(addr+"/vault/session", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out map[string]string
		json.NewDecoder(r.Body).Decode(&out)
		return out["token"]
	}()
	body, _ := json.Marshal(map[string]string{"consumer": "a", "scope": "zed:*", "ttl": "1h"})
	req, _ := http.NewRequest("POST", addr+"/vault/tokens/service", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+grant)
	req.Header.Set("Content-Type", "application/json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Errorf("token for zed: %d", r.StatusCode)
	}

	// Removing leo with no rows left.
	runCmd(t, cmdPerson, "person", "remove", "leo")
	if out := runCmd(t, cmdPerson, "person", "list"); strings.Contains(out, "leo") {
		t.Errorf("leo listed after remove:\n%s", out)
	}
}
