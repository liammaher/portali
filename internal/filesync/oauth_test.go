package filesync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTokenServer is a provider's token endpoint. It records every request
// and answers like the real ones: a code (with the right PKCE verifier) or a
// refresh token for access tokens, invalid_grant for anything else.
type fakeTokenServer struct {
	*httptest.Server
	mu         sync.Mutex
	forms      []url.Values
	challenge  string // the code_challenge the authorization request carried
	rotate     bool   // answers a refresh with a new refresh token
	refreshErr string // answers a refresh with this OAuth error
	n          int
}

func newFakeTokenServer(t *testing.T) *fakeTokenServer {
	t.Helper()
	f := &fakeTokenServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.forms = append(f.forms, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		deny := func(code string) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": "no"})
		}
		f.n++
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != "the-code" || pkceChallenge(r.PostForm.Get("code_verifier")) != f.challenge {
				deny("invalid_grant")
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 3600})
		case "refresh_token":
			if f.refreshErr != "" {
				deny(f.refreshErr)
				return
			}
			if r.PostForm.Get("refresh_token") == "" {
				deny("invalid_request")
				return
			}
			resp := map[string]any{"access_token": "access-" + string(rune('0'+f.n)), "expires_in": 3600}
			if f.rotate {
				resp["refresh_token"] = "rotated-" + string(rune('0'+f.n))
			}
			json.NewEncoder(w).Encode(resp)
		default:
			deny("unsupported_grant_type")
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeTokenServer) last() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forms[len(f.forms)-1]
}

func testFlow(ts *fakeTokenServer) *Flow {
	return &Flow{
		Name: "Test", AuthURL: "https://idp.example/authorize", TokenURL: ts.URL, ClientID: "client-1",
		Scopes: []string{"read.one", "read.two"}, AuthParams: url.Values{"access_type": {"offline"}}, Port: 0,
	}
}

// browse plays the user's browser: it follows the provider's redirect to the
// loopback address with what the provider would send.
func browse(t *testing.T, authURL string, extra url.Values) *http.Response {
	t.Helper()
	u, _ := url.Parse(authURL)
	redirect, _ := url.Parse(u.Query().Get("redirect_uri"))
	q := url.Values{"state": {u.Query().Get("state")}}
	for k, v := range extra {
		q[k] = v
	}
	redirect.RawQuery = q.Encode()
	resp, err := http.Get(redirect.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestOAuth_AuthorizationCodeWithPKCE(t *testing.T) {
	ts := newFakeTokenServer(t)
	a, err := testFlow(ts).StartAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	q := u.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != "client-1" || q.Get("code_challenge_method") != "S256" ||
		q.Get("scope") != "read.one read.two" || q.Get("access_type") != "offline" || q.Get("code_challenge") == "" || q.Get("state") == "" {
		t.Fatalf("authorization request = %v", q)
	}
	if !strings.HasPrefix(q.Get("redirect_uri"), "http://localhost:") {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	ts.challenge = q.Get("code_challenge")

	// A callback with another state is not ours: refused, and the sign-in goes on.
	bad := url.Values{"state": {"someone-else"}, "code": {"stolen"}}
	redirect := q.Get("redirect_uri") + "?" + bad.Encode()
	resp, err := http.Get(redirect)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback with a foreign state: %v %v", resp, err)
	}
	resp.Body.Close()
	if r := browse(t, a.URL, url.Values{"code": {"the-code"}}); r.StatusCode != 200 {
		t.Fatalf("callback = %d", r.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := a.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "access-1" || tok.RefreshToken != "refresh-1" || tok.Expiry.Before(time.Now().Add(50*time.Minute)) {
		t.Fatalf("token = %+v", tok)
	}
	form := ts.last()
	if form.Get("client_id") != "client-1" || form.Get("redirect_uri") != q.Get("redirect_uri") || form.Get("client_secret") != "" {
		t.Fatalf("token request = %v", form)
	}
}

func TestOAuth_RefusedTimedOutAndSecondCallback(t *testing.T) {
	ts := newFakeTokenServer(t)

	a, _ := testFlow(ts).StartAuthorization()
	browse(t, a.URL, url.Values{"error": {"access_denied"}, "error_description": {"the user said no"}})
	if _, err := a.Wait(context.Background()); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("a refusal = %v", err)
	}

	a, _ = testFlow(ts).StartAuthorization()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := a.Wait(ctx); err == nil || !strings.Contains(err.Error(), "not finished") {
		t.Fatalf("a timeout = %v", err)
	}

	// The port is free again after Wait: a second sign-in can listen on it.
	a, _ = testFlow(ts).StartAuthorization()
	u, _ := url.Parse(a.URL)
	ts.challenge = u.Query().Get("code_challenge")
	browse(t, a.URL, url.Values{"code": {"the-code"}})
	browse(t, a.URL, url.Values{"code": {"another-code"}}) // a second answer is ignored
	if tok, err := a.Wait(context.Background()); err != nil || tok.RefreshToken != "refresh-1" {
		t.Fatalf("after a second callback: %+v %v", tok, err)
	}

	// A wrong code or verifier is refused by the provider.
	a, _ = testFlow(ts).StartAuthorization()
	ts.challenge = "not-this-one"
	browse(t, a.URL, url.Values{"code": {"the-code"}})
	var ae *AuthError
	if _, err := a.Wait(context.Background()); !errors.As(err, &ae) {
		t.Fatalf("a failed exchange = %v, want an AuthError", err)
	}
}

func TestOAuth_PortInUseSaysSo(t *testing.T) {
	ts := newFakeTokenServer(t)
	f := testFlow(ts)
	a, err := f.StartAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	u, _ := url.Parse(a.URL)
	ru, _ := url.Parse(u.Query().Get("redirect_uri"))
	f2 := testFlow(ts)
	f2.Port, _ = parsePort(ru.Port())
	if _, err := f2.StartAuthorization(); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("second listener on the same port = %v", err)
	}
}

func parsePort(s string) (int, error) {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func TestTokenSource_RefreshesAndKeepsWhatItIsGiven(t *testing.T) {
	ts := newFakeTokenServer(t)
	store := &MemoryStore{}
	flow := testFlow(ts)
	svc := ServiceName("src1")
	if err := SaveToken(store, svc, &Token{AccessToken: "old", RefreshToken: "refresh-0", Expiry: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	src, err := NewTokenSource(flow, store, svc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Expired: refreshed. No new refresh token in the answer: the old one stays saved.
	at, err := src.Access(ctx)
	if err != nil || at == "old" {
		t.Fatalf("access = %q, %v", at, err)
	}
	var saved Token
	raw, _ := store.Load(svc)
	json.Unmarshal([]byte(raw), &saved)
	// The refresh token is kept; the access token (kilobytes, from Microsoft) is not.
	if saved.RefreshToken != "refresh-0" || saved.AccessToken != "" || !saved.Expiry.IsZero() {
		t.Fatalf("saved = %+v", saved)
	}
	// Still valid: no second request.
	n := len(ts.forms)
	if again, _ := src.Access(ctx); again != at || len(ts.forms) != n {
		t.Fatal("a valid token was refreshed again")
	}

	// A provider that rotates: the new refresh token is written back before use.
	ts.rotate = true
	src.Invalidate()
	if _, err := src.Access(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _ = store.Load(svc)
	json.Unmarshal([]byte(raw), &saved)
	if !strings.HasPrefix(saved.RefreshToken, "rotated-") {
		t.Fatalf("a rotated refresh token wasn't saved: %+v", saved)
	}

	// Revoked: an AuthError that says to connect again; and no sign-in saved says so too.
	ts.refreshErr = "invalid_grant"
	src.Invalidate()
	var ae *AuthError
	if _, err := src.Access(ctx); !errors.As(err, &ae) {
		t.Fatalf("revoked = %v", err)
	}
	if _, err := NewTokenSource(flow, store, ServiceName("never")); !errors.As(err, &ae) || !strings.Contains(err.Error(), "connect it again") {
		t.Fatalf("nothing saved = %v", err)
	}
	store.Save(ServiceName("junk"), "not json")
	if _, err := NewTokenSource(flow, store, ServiceName("junk")); !errors.As(err, &ae) {
		t.Fatalf("unreadable = %v", err)
	}
}

func TestTokenSource_ClientSecretStaysWithTheTokens(t *testing.T) {
	ts := newFakeTokenServer(t)
	store := &MemoryStore{}
	flow := testFlow(ts)
	SaveToken(store, "s", &Token{RefreshToken: "r", ClientSecret: "desktop-secret"})
	src, _ := NewTokenSource(flow, store, "s")
	if _, err := src.Access(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ts.last().Get("client_secret") != "desktop-secret" {
		t.Fatalf("refresh request = %v", ts.last())
	}
	raw, _ := store.Load("s")
	if !strings.Contains(raw, "desktop-secret") {
		t.Fatal("the client secret was dropped on refresh")
	}
}

func TestNewFlow_ReadOnlyScopesPerProvider(t *testing.T) {
	for provider, want := range map[string][]string{
		"onedrive":   {"Files.Read", "offline_access"},
		"sharepoint": {"Files.Read.All", "Sites.Read.All", "offline_access"},
		"gdrive":     {"https://www.googleapis.com/auth/drive.metadata.readonly"},
		"dropbox":    {"files.metadata.read"},
	} {
		f, err := NewFlow(Spec{Provider: provider, ClientID: "c"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(f.Scopes, " ") != strings.Join(want, " ") {
			t.Errorf("%s scopes = %v, want %v", provider, f.Scopes, want)
		}
		for _, s := range f.Scopes {
			if strings.Contains(strings.ToLower(s), "write") || strings.Contains(strings.ToLower(s), "readwrite") || strings.HasSuffix(s, "FullControl.All") {
				t.Errorf("%s asks for more than reading: %q", provider, s)
			}
		}
		if f.Port != DefaultPort {
			t.Errorf("%s port = %d", provider, f.Port)
		}
	}
	if f, _ := NewFlow(Spec{Provider: "gdrive", ClientID: "c"}, nil); f.RedirectHost != "127.0.0.1" || f.AuthParams.Get("access_type") != "offline" {
		t.Errorf("google flow = %+v", f)
	}
	if f, _ := NewFlow(Spec{Provider: "dropbox", ClientID: "c"}, nil); f.AuthParams.Get("token_access_type") != "offline" {
		t.Errorf("dropbox flow = %+v", f.AuthParams)
	}
	if f, _ := NewFlow(Spec{Provider: "onedrive", ClientID: "c", Tenant: "contoso.com"}, nil); !strings.Contains(f.AuthURL, "/contoso.com/oauth2/v2.0/authorize") {
		t.Errorf("tenant not used: %s", f.AuthURL)
	}
	if _, err := NewFlow(Spec{Provider: "box", ClientID: "c"}, nil); err == nil {
		t.Error("an unknown provider")
	}
	if _, err := NewFlow(Spec{Provider: "dropbox"}, nil); err == nil {
		t.Error("no client ID")
	}
}
