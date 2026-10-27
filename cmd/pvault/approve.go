package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
)

// An approver gets this command a grant without the typed password by asking
// the user in person: Touch ID on a Mac (the pvault-touchid helper), Windows
// Hello on Windows (the vault server asks itself). Either way the prompt
// names exactly the grant, worded from it, and this command only ever
// receives the token.
type approver interface {
	name() string      // as the user knows it
	enableCmd() string // the pvault command that turns it on
	// enrolled reports whether it is on for this vault; checkVaultServer
	// has run.
	enrolled() bool
	grant() (string, error) // covering cliGrant
	sign(id string) (string, error)
}

// currentApprover is this system's (a variable for tests).
var currentApprover = func() approver {
	if runtime.GOOS == "windows" {
		return helloApprover{}
	}
	return touchIDApprover{}
}

type touchIDApprover struct{}

func (touchIDApprover) name() string           { return "Touch ID" }
func (touchIDApprover) enableCmd() string      { return "touchid enable" }
func (touchIDApprover) enrolled() bool         { return touchIDEnrolled() }
func (touchIDApprover) grant() (string, error) { return touchIDGrant() }

func (touchIDApprover) sign(id string) (string, error) {
	token, err := runTouchID("", "sign", "--id", id)
	if err == nil && token == "" {
		err = errors.New("Touch ID helper returned no token")
	}
	return token, err
}

// helloApprover asks the vault server for a grant it approves with Windows
// Hello (POST /vault/session with approve "hello", over the private channel).
type helloApprover struct{}

var errHelloDenied = errors.New("Windows Hello request was not approved")

func (helloApprover) name() string      { return "Windows Hello" }
func (helloApprover) enableCmd() string { return "hello enable" }
func (helloApprover) enrolled() bool    { return verifiedApprover == "hello" }

func (helloApprover) grant() (string, error) {
	body := map[string]any{"approve": "hello", "window": promptWindow()}
	if cliGrant.scope != "" {
		body["scope"] = cliGrant.scope
		if cliGrant.access != "" {
			body["access"] = cliGrant.access
		}
	}
	result, err := helloRequest(body)
	if err != nil {
		return "", err
	}
	// A server that ignored scope or access would grant more than the user
	// approved, so its token goes unused.
	if cliGrant.scope != "" {
		want := cliGrant.access
		if want == "" {
			want = "read"
		}
		if result["scope"] != cliGrant.scope || result["access"] != want {
			return "", errors.New("the vault server granted something other than what was approved — update it ('" + pvCmd("lock") + "', then '" + pvCmd("unlock") + "')")
		}
	}
	return result["token"], nil
}

func (helloApprover) sign(id string) (string, error) {
	result, err := helloRequest(map[string]any{"approve": "hello", "window": promptWindow(), "sign": id})
	if err != nil {
		return "", err
	}
	if result["sign"] != id {
		return "", errors.New("the vault server granted something other than this signing review")
	}
	return result["token"], nil
}

// helloRequest sends an approval request and waits while the user answers.
func helloRequest(body map[string]any) (map[string]string, error) {
	resp, err := approvalRequest("POST", "/vault/session", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var refusal struct {
			Error      string `json:"error"`
			Constraint string `json:"constraint"`
		}
		json.Unmarshal(data, &refusal)
		switch refusal.Constraint {
		case "not_approved":
			return nil, errHelloDenied
		case "approver_off":
			return nil, fmt.Errorf("Windows Hello is off for this vault — run '%s'", pvCmd("hello enable"))
		}
		if refusal.Error != "" {
			return nil, errors.New(refusal.Error)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil || result["token"] == "" {
		return nil, errors.New("the vault server returned no token")
	}
	return result, nil
}
