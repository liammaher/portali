package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/filesync"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

// A fake Dropbox: its token endpoint (which checks the PKCE verifier against
// the challenge the browser was sent) and the two list calls.
type fakeDropbox struct {
	*httptest.Server
	mu        sync.Mutex
	challenge string
	files     []map[string]any
	refreshes int
	badToken  bool   // answers every API call with 401
	email     string // whose account it is
}

func newFakeDropbox(t *testing.T) *fakeDropbox {
	t.Helper()
	d := &fakeDropbox{}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth2/token":
			r.ParseForm()
			switch r.PostForm.Get("grant_type") {
			case "authorization_code":
				sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
				if r.PostForm.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != d.challenge {
					w.WriteHeader(400)
					w.Write([]byte(`{"error":"invalid_grant"}`))
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"access_token": "dbx-access-1", "refresh_token": "dbx-refresh-1", "expires_in": 14400})
			case "refresh_token":
				d.refreshes++
				json.NewEncoder(w).Encode(map[string]any{"access_token": "dbx-access-2", "expires_in": 14400})
			}
		case "/2/users/get_current_account":
			email := d.email
			if email == "" {
				email = "liam@example.it"
			}
			json.NewEncoder(w).Encode(map[string]string{"email": email})
		case "/2/files/list_folder":
			if d.badToken || r.Header.Get("Authorization") == "" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"entries": d.files, "cursor": "c", "has_more": false})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.Close)
	return d
}

// signInAs plays the browser: it takes the address pvault prints and follows
// the provider's redirect back to the loopback listener.
func (d *fakeDropbox) signInAs(t *testing.T) func(string) {
	return func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Error(err)
			return
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("scope") != "files.metadata.read" || q.Get("token_access_type") != "offline" || q.Get("client_id") != "test-client" {
			t.Errorf("sign-in request = %v", q)
		}
		d.mu.Lock()
		d.challenge = q.Get("code_challenge")
		d.mu.Unlock()
		go func() {
			cb := q.Get("redirect_uri") + "?" + url.Values{"code": {"the-code"}, "state": {q.Get("state")}}.Encode()
			if resp, err := http.Get(cb); err == nil {
				resp.Body.Close()
			}
		}()
	}
}

// An account is connected through a (fake) browser, indexed, searched and
// forgotten, with its sign-in in the store and nowhere else.
func TestFiles_ConnectAnAccountEndToEnd(t *testing.T) {
	v, _ := touchIDVault(t, map[string]string{"leo:famiglia.relazione": "figlio"})
	_ = v
	dbx := newFakeDropbox(t)
	dbx.files = []map[string]any{
		{".tag": "file", "id": "id:1", "name": "contratto.pdf", "path_display": "/Casa/contratto.pdf", "size": 10, "server_modified": "2026-10-01T10:00:00Z", "content_hash": "h1"},
		{".tag": "file", "id": "id:2", "name": "scuola.pdf", "path_display": "/Leo/scuola.pdf", "size": 10, "server_modified": "2026-10-01T10:00:00Z"},
	}
	store := &filesync.MemoryStore{}
	oldStore, oldEP, oldBrowser := secretStore, connectEndpoints, browserOpener
	secretStore = store
	connectEndpoints = &filesync.Endpoints{AuthURL: dbx.URL + "/oauth2/authorize", TokenURL: dbx.URL + "/oauth2/token", API: dbx.URL + "/2"}
	browserOpener = dbx.signInAs(t)
	t.Cleanup(func() { secretStore, connectEndpoints, browserOpener = oldStore, oldEP, oldBrowser })

	out := runCmd(t, cmdFiles, "files", "source", "connect", "dropbox", "--client-id", "test-client", "--port", freePort(t))
	if !strings.Contains(out, "READ file names") || !strings.Contains(out, "Connected Dropbox liam@example.it") {
		t.Fatalf("connect:\n%s", out)
	}
	if strings.Contains(out, "dbx-access") || strings.Contains(out, "dbx-refresh") {
		t.Fatalf("a token was printed:\n%s", out)
	}
	if len(store.M) != 1 {
		t.Fatalf("saved sign-ins: %v", store.M)
	}
	for svc, raw := range store.M {
		if !strings.HasPrefix(svc, "pvault-files-") || !strings.Contains(raw, "dbx-refresh-1") {
			t.Fatalf("saved %q = %q", svc, raw)
		}
	}
	// What the vault holds about the source has no token.
	srcs, err := v.FileSources(nil)
	if err != nil || len(srcs) != 1 || srcs[0].ClientID != "test-client" || srcs[0].Root != "" || srcs[0].Provider != "dropbox" {
		t.Fatalf("source = %+v, %v", srcs, err)
	}
	if raw, _ := json.Marshal(srcs[0]); strings.Contains(string(raw), "refresh") || strings.Contains(string(raw), "dbx-") {
		t.Fatalf("the source carries a secret: %s", raw)
	}

	if out := runCmd(t, cmdFiles, "files", "source", "list"); !strings.Contains(out, "through its API: liam@example.it") {
		t.Fatalf("source list:\n%s", out)
	}
	out = runCmd(t, cmdFiles, "files", "sync")
	if !strings.Contains(out, "2 files") || !strings.Contains(out, "2 new") {
		t.Fatalf("sync:\n%s", out)
	}
	out = runCmd(t, cmdFiles, "files", "search", "scuola")
	if !strings.Contains(out, "scuola.pdf") || !strings.Contains(out, "/Leo/scuola.pdf") {
		t.Fatalf("search:\n%s", out)
	}
	// Dropbox's own page for a file is kept as its address.
	if out := runCmd(t, cmdFiles, "files", "show", fileID(t, out, "scuola.pdf")); !strings.Contains(out, "https://www.dropbox.com/home/Leo/scuola.pdf") {
		t.Fatalf("show:\n%s", out)
	}

	// A file that is gone from the account is marked gone; the others stay.
	dbx.files = dbx.files[:1]
	if out := runCmd(t, cmdFiles, "files", "sync"); !strings.Contains(out, "1 gone") {
		t.Fatalf("second sync:\n%s", out)
	}

	// An account that refuses the saved sign-in marks nothing gone and says to connect again.
	dbx.badToken = true
	out = runCmd(t, cmdFiles, "files", "sync")
	if !strings.Contains(out, "nothing was marked gone") || !strings.Contains(out, "connect the source again") {
		t.Fatalf("a refused sign-in:\n%s", out)
	}
	dbx.badToken = false
	if out := runCmd(t, cmdFiles, "files", "search", "contratto"); !strings.Contains(out, "contratto.pdf") {
		t.Fatalf("index after a refused sync:\n%s", out)
	}

	// Removing the source removes the saved sign-in with it.
	out = runCmd(t, cmdFiles, "files", "source", "remove", "Dropbox liam@example.it")
	if !strings.Contains(out, "removed from the Keychain") || len(store.M) != 0 {
		t.Fatalf("remove:\n%s\nstore: %v", out, store.M)
	}
}

// TestFilesFatalHelper is the child process runFatal starts: fatal() exits, so
// what it prints is read from outside.
func TestFilesFatalHelper(t *testing.T) {
	if os.Getenv("PVAULT_FATAL_HELPER") != "1" {
		t.Skip("only as a child of runFatal")
	}
	os.Args = append([]string{"pvault"}, strings.Split(os.Getenv("PVAULT_FATAL_ARGS"), "\x1f")...)
	cmdFiles()
}

func runFatal(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFilesFatalHelper$")
	cmd.Env = append(os.Environ(), "PVAULT_FATAL_HELPER=1", "PVAULT_FATAL_ARGS="+strings.Join(args, "\x1f"))
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func TestFiles_ConnectChecksItsArguments(t *testing.T) {
	// Each of these exits before anything is asked of a provider.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"files", "source", "connect", "dropbox"}, "--client-id is needed"},
		{[]string{"files", "source", "connect", "box", "--client-id", "x"}, "use one of"},
		{[]string{"files", "source", "connect", "sharepoint", "--client-id", "x"}, "--site is needed"},
		{[]string{"files", "source", "connect", "dropbox", "--client-id", "x", "--site", "https://a.sharepoint.com"}, "for sharepoint"},
		{[]string{"files", "source", "connect", "gdrive", "--client-id", "x", "--tenant", "t"}, "Microsoft"},
	} {
		got := runFatal(t, tc.args...)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%v: %q, want %q", tc.args, got, tc.want)
		}
	}
}

// A sign-in that is gone is renewed without losing the source's links, and
// only as the same account.
func TestFiles_ReconnectKeepsTheSourceAndItsLinks(t *testing.T) {
	touchIDVault(t, nil)
	dbx := newFakeDropbox(t)
	dbx.files = []map[string]any{
		{".tag": "file", "id": "id:1", "name": "contratto.pdf", "path_display": "/Casa/contratto.pdf", "size": 10, "server_modified": "2026-10-01T10:00:00Z"},
	}
	store := &filesync.MemoryStore{}
	oldStore, oldEP, oldBrowser := secretStore, connectEndpoints, browserOpener
	secretStore = store
	connectEndpoints = &filesync.Endpoints{AuthURL: dbx.URL + "/oauth2/authorize", TokenURL: dbx.URL + "/oauth2/token", API: dbx.URL + "/2"}
	browserOpener = dbx.signInAs(t)
	t.Cleanup(func() { secretStore, connectEndpoints, browserOpener = oldStore, oldEP, oldBrowser })

	runCmd(t, cmdFiles, "files", "source", "connect", "dropbox", "--client-id", "test-client", "--port", freePort(t))
	runCmd(t, cmdFiles, "files", "sync")
	id := fileID(t, runCmd(t, cmdFiles, "files", "search", "contratto"), "contratto.pdf")
	runCmd(t, cmdFiles, "files", "link", id, "--for", "io", "--role", "contratto")

	// The saved sign-in is lost (a new Mac, a locked-out Keychain item).
	for k := range store.M {
		delete(store.M, k)
	}
	out := runCmd(t, cmdFiles, "files", "sync")
	if !strings.Contains(out, "can't sign in") || !strings.Contains(out, "files source reconnect") || strings.Contains(out, "source remove") {
		t.Fatalf("sync without a sign-in:\n%s", out)
	}

	runReconnect := func() string {
		return runCmd(t, cmdFiles, "files", "source", "reconnect", "Dropbox liam@example.it", "--port", freePort(t))
	}
	out = runReconnect()
	if !strings.Contains(out, "Signed in again") || len(store.M) != 1 {
		t.Fatalf("reconnect:\n%s\nstore: %v", out, store.M)
	}
	// The index and the link survived, and a sync works again.
	if out := runCmd(t, cmdFiles, "files", "sync"); !strings.Contains(out, "1 unchanged") {
		t.Fatalf("sync after reconnecting:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "show", id); !strings.Contains(out, "linked to io (contratto)") {
		t.Fatalf("the link was lost:\n%s", out)
	}
}

func TestFiles_ReconnectOnlyAsTheSameAccount(t *testing.T) {
	src := vault.FileSourceInfo{Provider: "dropbox", FileSourceConfig: vault.FileSourceConfig{Account: "liam@example.it"}}
	if err := sameAccount(src, "LIAM@example.it", ""); err != nil {
		t.Errorf("the same account in another case: %v", err)
	}
	if err := sameAccount(src, "someone.else@example.it", ""); err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("another account: %v", err)
	}
	if err := sameAccount(src, "", ""); err != nil {
		t.Errorf("a provider that doesn't say who: %v", err)
	}
	od := vault.FileSourceInfo{Provider: "onedrive", FileSourceConfig: vault.FileSourceConfig{Account: "a@b.it", DriveID: "b!mine"}}
	if err := sameAccount(od, "a@b.it", "b!other"); err == nil {
		t.Error("another OneDrive")
	}
	if err := sameAccount(od, "a@b.it", "b!mine"); err != nil {
		t.Errorf("the same OneDrive: %v", err)
	}
}
