package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

// fakeApprover stands in for Windows Hello: it records what it was asked and
// answers as told.
type fakeApprover struct {
	mu        sync.Mutex
	available error
	answer    bool
	err       error
	messages  []string
	windows   []uint64
	hold      chan struct{} // when set, Approve waits for it to close
}

func (f *fakeApprover) Available() error { return f.available }

func (f *fakeApprover) Approve(ctx context.Context, window uint64, message string) (bool, error) {
	f.mu.Lock()
	f.messages = append(f.messages, message)
	f.windows = append(f.windows, window)
	hold := f.hold
	f.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return f.answer, f.err
}

func (f *fakeApprover) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.messages...)
}

// helloEnv is a test vault with Windows Hello turned on and a fake approver.
func helloEnv(t *testing.T) (*testEnv, *fakeApprover) {
	t.Helper()
	env := setup(t)
	f := &fakeApprover{answer: true}
	env.server.SetApprover(f)
	if w := env.call("POST", "/vault/hello", env.token, map[string]bool{"enabled": true}, true); w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body)
	}
	return env, f
}

func helloBody(extra map[string]any) map[string]any {
	body := map[string]any{"approve": "hello", "window": 42}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// The prompt names exactly what is granted, and the grant is exactly that.
func TestApprove_ScopedGrantIsWhatThePromptSaid(t *testing.T) {
	env, f := helloEnv(t)
	if err := env.vault.Set("identity.full_name", "Mario Rossi", ""); err != nil {
		t.Fatal(err)
	}
	if err := env.vault.Set("residenza.cap", "80100", ""); err != nil {
		t.Fatal(err)
	}
	w := env.call("POST", "/vault/session", "", helloBody(map[string]any{"scope": "identity.full_name"}), true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got := decode[map[string]string](t, w)
	if got["scope"] != "identity.full_name" || got["access"] != "read" {
		t.Fatalf("echo: %v", got)
	}
	want := "pvault wants to read identity.full_name from your vault for 2 minutes."
	if msgs := f.asked(); len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("prompt %q, want %q", msgs, want)
	}
	if f.windows[0] != 42 {
		t.Errorf("window %d", f.windows[0])
	}
	if w := env.call("GET", "/vault/fields/identity.full_name", got["token"], nil, false); w.Code != 200 {
		t.Errorf("in scope: %d %s", w.Code, w.Body)
	}
	if w := env.call("GET", "/vault/fields/residenza.cap", got["token"], nil, false); w.Code == 200 {
		t.Error("the grant reached a field the prompt didn't name")
	}
	if w := env.call("GET", "/vault/tokens/service", got["token"], nil, false); w.Code == 200 {
		t.Error("a scoped grant did what only the session may")
	}
	entries, _ := env.vault.AuditLog(5)
	if !auditHas(entries, "scoped_grant", "via: hello") {
		t.Errorf("audit: %+v", entries)
	}
}

func TestApprove_FullAccess(t *testing.T) {
	env, f := helloEnv(t)
	w := env.call("POST", "/vault/session", "", helloBody(nil), true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := "pvault wants to get full access to your vault, including settings and agent tokens, for 2 minutes."
	if msgs := f.asked(); len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("prompt %q", msgs)
	}
	token := decode[map[string]string](t, w)["token"]
	if w := env.call("GET", "/vault/tokens/service", token, nil, false); w.Code != 200 {
		t.Errorf("full access refused: %d %s", w.Code, w.Body)
	}
}

func TestApprove_AppendAndTTLAreWorded(t *testing.T) {
	env, f := helloEnv(t)
	w := env.call("POST", "/vault/session", "", helloBody(map[string]any{"scope": "leo:cie.*", "access": "append", "ttl": "5m"}), true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := "pvault wants to read and add new entries to all of leo's cie fields and documents in your vault for 5 minutes."
	if msgs := f.asked(); msgs[0] != want {
		t.Fatalf("prompt %q", msgs[0])
	}
}

// A refusal grants nothing, is audited, and counts toward the rate limit like
// a wrong password.
func TestApprove_Refused(t *testing.T) {
	env, f := helloEnv(t)
	f.answer = false
	w := env.call("POST", "/vault/session", "", helloBody(map[string]any{"scope": "cie.*"}), true)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "not_approved") || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	entries, _ := env.vault.AuditLog(5)
	if !auditHas(entries, "approval_refused", "via: hello") {
		t.Errorf("audit: %+v", entries)
	}
	for range 9 {
		env.call("POST", "/vault/session", "", helloBody(map[string]any{"scope": "cie.*"}), true)
	}
	if w := env.call("POST", "/vault/session", "", helloBody(map[string]any{"scope": "cie.*"}), true); w.Code != 429 {
		t.Fatalf("after 10 refusals: %d", w.Code)
	}
}

// Nothing is shown to the user for a request that can't be granted as asked.
func TestApprove_RefusedBeforeAsking(t *testing.T) {
	env, f := helloEnv(t)
	cases := []struct {
		name   string
		body   map[string]any
		socket bool
		code   int
	}{
		{"over TCP", helloBody(nil), false, 403},
		{"with a password", helloBody(map[string]any{"password": testPassword}), true, 400},
		{"another approver", map[string]any{"approve": "touchid"}, true, 400},
		{"bad scope", helloBody(map[string]any{"scope": "identity.*\n"}), true, 400},
		{"too many patterns", helloBody(map[string]any{"scope": strings.Repeat("a.b,", 10) + "c.d"}), true, 400},
		{"io prefix", helloBody(map[string]any{"scope": "io:*"}), true, 400},
		{"access without scope", helloBody(map[string]any{"access": "write"}), true, 400},
		{"bad access", helloBody(map[string]any{"scope": "cie.*", "access": "admin"}), true, 400},
		{"scoped ttl too long", helloBody(map[string]any{"scope": "cie.*", "ttl": "11m"}), true, 400},
		{"full ttl too long", helloBody(map[string]any{"ttl": "2h"}), true, 400},
		{"sign with scope", helloBody(map[string]any{"sign": strings.Repeat("a", 32), "scope": "cie.*"}), true, 400},
		{"unknown sign request", helloBody(map[string]any{"sign": strings.Repeat("a", 32)}), true, 404},
	}
	for _, c := range cases {
		if w := env.call("POST", "/vault/session", "", c.body, c.socket); w.Code != c.code {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	if msgs := f.asked(); len(msgs) != 0 {
		t.Fatalf("the user was asked: %q", msgs)
	}
}

func TestApprove_OffUnavailableOrLocked(t *testing.T) {
	env := setup(t)
	f := &fakeApprover{answer: true}
	env.server.SetApprover(f)
	if w := env.call("POST", "/vault/session", "", helloBody(nil), true); w.Code != 412 {
		t.Fatalf("off: %d %s", w.Code, w.Body)
	}
	// Turning it on needs the session, and Windows Hello there to answer.
	f.available = errors.New("Windows Hello isn't set up")
	if w := env.call("POST", "/vault/hello", env.token, map[string]bool{"enabled": true}, true); w.Code != 503 {
		t.Fatalf("enable while unavailable: %d", w.Code)
	}
	f.available = nil
	scoped, _, err := env.vault.CreateScopedGrant(testPassword, "", time.Minute, "*", "write")
	if err != nil {
		t.Fatal(err)
	}
	if w := env.call("POST", "/vault/hello", scoped, map[string]bool{"enabled": true}, true); w.Code != 403 {
		t.Fatalf("enable with a scoped grant: %d", w.Code)
	}
	if w := env.call("POST", "/vault/hello", env.token, map[string]bool{"enabled": true}, true); w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body)
	}
	f.available = errors.New("Windows Hello isn't set up")
	if w := env.call("POST", "/vault/session", "", helloBody(nil), true); w.Code != 503 || !strings.Contains(w.Body.String(), "isn't set up") {
		t.Fatalf("unavailable: %d %s", w.Code, w.Body)
	}
	f.available = nil
	env.vault.Lock()
	if w := env.call("POST", "/vault/session", "", helloBody(nil), true); w.Code == 200 {
		t.Fatal("granted on a locked vault")
	}
	if msgs := f.asked(); len(msgs) != 0 {
		t.Fatalf("the user was asked: %q", msgs)
	}
}

func TestApprove_OnePromptAtATime(t *testing.T) {
	env, f := helloEnv(t)
	f.hold = make(chan struct{})
	done := make(chan int)
	go func() { done <- env.call("POST", "/vault/session", "", helloBody(nil), true).Code }()
	for len(f.asked()) == 0 {
		time.Sleep(time.Millisecond)
	}
	if w := env.call("POST", "/vault/session", "", helloBody(nil), true); w.Code != 409 {
		t.Errorf("second prompt: %d", w.Code)
	}
	close(f.hold)
	if code := <-done; code != 200 {
		t.Errorf("first: %d", code)
	}
}

// A signing review's prompt comes from the vault's own summary of the
// request, and the grant reaches only that request.
func TestApprove_SignReview(t *testing.T) {
	env, agent := signingEnv(t, "firma_autografa.signing", vault.AccessAppend)
	f := &fakeApprover{answer: true}
	env.server.SetApprover(f)
	env.call("POST", "/vault/hello", env.token, map[string]bool{"enabled": true}, true)
	info := decode[vault.SigningInfo](t, env.call("POST", "/vault/signing", agent, signRequestBody(), false))

	w := env.call("POST", "/vault/session", "", helloBody(map[string]any{"sign": info.ID}), true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got := decode[map[string]string](t, w)
	if got["sign"] != info.ID {
		t.Fatalf("echo: %v", got)
	}
	want := "pvault wants to " + vault.SignPrompt("your vault", info.Name, info.Pages, info.SHA256[:8], info.Requester, vault.SignGrantTTL) + "."
	if msgs := f.asked(); len(msgs) != 1 || msgs[0] != want || !strings.Contains(msgs[0], "mod-1.pdf") {
		t.Fatalf("prompt %q, want %q", msgs, want)
	}
	if w := env.call("GET", "/vault/signing/"+info.ID+"/candidate", got["token"], nil, false); w.Code != 200 {
		t.Errorf("candidate: %d %s", w.Code, w.Body)
	}
	if w := env.call("GET", "/vault/fields/identity.full_name", got["token"], nil, false); w.Code == 200 {
		t.Error("a sign grant read a field")
	}
}

// The CLI learns from status whether to ask Windows Hello, over the socket only.
func TestStatus_NamesApproverOverSocket(t *testing.T) {
	env := setup(t)
	env.server.SetApprover(&fakeApprover{})
	if strings.Contains(env.call("GET", "/vault/status", "", nil, true).Body.String(), "approver") {
		t.Fatal("approver named while off")
	}
	env.call("POST", "/vault/hello", env.token, map[string]bool{"enabled": true}, true)
	if !strings.Contains(env.call("GET", "/vault/status", "", nil, true).Body.String(), `"approver":"hello"`) {
		t.Error("not named over the socket")
	}
	if strings.Contains(env.call("GET", "/vault/status", "", nil, false).Body.String(), "approver") {
		t.Error("named over TCP")
	}
	env.call("POST", "/vault/hello", env.token, map[string]bool{"enabled": false}, true)
	if strings.Contains(env.call("GET", "/vault/status", "", nil, true).Body.String(), "approver") {
		t.Error("named after turning it off")
	}
}

func auditHas(entries []store.AuditEntry, action, purpose string) bool {
	for _, e := range entries {
		if e.Action == action && strings.Contains(e.Purpose, purpose) {
			return true
		}
	}
	return false
}

// The switch is pvault hello's, over the private channel: not the onboarding
// page's session over TCP.
func TestHello_SwitchOnlyOverPrivateChannel(t *testing.T) {
	env := setup(t)
	env.server.SetApprover(&fakeApprover{})
	if w := env.call("POST", "/vault/hello", env.token, map[string]bool{"enabled": true}, false); w.Code != 403 {
		t.Fatalf("over TCP: %d %s", w.Code, w.Body)
	}
	if on, _ := env.vault.HelloEnabled(); on {
		t.Fatal("turned on over TCP")
	}
}

// What happens while the prompt is open counts: turning Windows Hello off,
// locking, the caller leaving or the server stopping means no grant, and the
// next prompt isn't kept waiting.
func TestApprove_ChangesWhileThePromptIsOpen(t *testing.T) {
	cases := map[string]func(env *testEnv, cancel context.CancelFunc){
		"turned off": func(env *testEnv, _ context.CancelFunc) {
			if err := env.vault.SetHelloEnabled(false); err != nil {
				t.Fatal(err)
			}
		},
		"locked":         func(env *testEnv, _ context.CancelFunc) { env.vault.Lock() },
		"caller left":    func(_ *testEnv, cancel context.CancelFunc) { cancel() },
		"server stopped": func(env *testEnv, _ context.CancelFunc) { env.server.Stop(context.Background()) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			env, f := helloEnv(t)
			f.hold = make(chan struct{})
			ctx, cancel := context.WithCancel(env.server.stopping)
			defer cancel()
			done := make(chan *httptest.ResponseRecorder)
			go func() {
				var buf bytes.Buffer
				json.NewEncoder(&buf).Encode(helloBody(nil))
				req := newTestRequest("POST", "/vault/session", &buf).WithContext(context.WithValue(ctx, socketKey{}, true))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				env.server.handler.ServeHTTP(w, req)
				done <- w
			}()
			for len(f.asked()) == 0 {
				time.Sleep(time.Millisecond)
			}
			change(env, cancel)
			if name == "turned off" || name == "locked" {
				close(f.hold) // the user approves anyway
			}
			w := <-done
			if w.Code == 200 || strings.Contains(w.Body.String(), "token") {
				t.Fatalf("granted: %d %s", w.Code, w.Body)
			}
			if !env.server.approveMu.TryLock() {
				t.Fatal("the next prompt would have to wait")
			}
			env.server.approveMu.Unlock()
		})
	}
}
