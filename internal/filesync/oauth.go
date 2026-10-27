package filesync

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Token is what a provider's sign-in gives: a short-lived access token and the
// refresh token that gets the next one. Both are secrets: they live in the
// Keychain (tokens.go), never in the vault, its backups or an argument.
type Token struct {
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry,omitempty"`
	// ClientSecret is kept with the tokens (Google asks for it from a desktop
	// app, though it is not confidential there).
	ClientSecret string `json:"client_secret,omitempty"`
}

// Flow is one provider's OAuth 2.0 authorization-code flow with PKCE, for a
// program on the user's own computer: the user signs in in their browser and
// the provider calls back to a loopback address of this process.
type Flow struct {
	Name         string
	AuthURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// AuthParams are added to the authorization request (Microsoft's and
	// Dropbox's offline access, Google's consent prompt).
	AuthParams url.Values
	// RedirectHost is how the provider is told to call back: "localhost" or
	// "127.0.0.1" (Google only takes the second without a registered port).
	RedirectHost string
	// Port is the loopback port to listen on; 0 picks a free one (tests).
	Port int
	// ScopeSep separates the scopes in the request: a space, unless a provider
	// wants another character.
	ScopeSep string
	// HTTP is the client for the token endpoint (default: 30 s timeout).
	HTTP *http.Client
}

func (f *Flow) httpClient() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// AuthError means the provider no longer accepts the saved sign-in (the user
// revoked it, a password changed, a refresh token expired): sign in again.
type AuthError struct{ Msg string }

func (e *AuthError) Error() string { return e.Msg }

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pkceChallenge is the S256 challenge of a code verifier.
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// Authorization is a started sign-in: the address for the user's browser, and
// Wait for the result.
type Authorization struct {
	URL      string
	verifier string
	state    string
	redirect string
	flow     *Flow
	srv      *http.Server
	result   chan callbackResult
}

type callbackResult struct {
	code string
	err  error
}

// StartAuthorization listens on the loopback port and returns the address the
// user opens to sign in. The caller must call Wait (or Close).
func (f *Flow) StartAuthorization() (*Authorization, error) {
	host := f.RedirectHost
	if host == "" {
		host = "localhost"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.Port)))
	if err != nil {
		return nil, fmt.Errorf("can't listen for the sign-in on port %d (is another pvault connect running?): %w", f.Port, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://%s:%d/", host, port)
	verifier, err := randomURLSafe(48)
	if err != nil {
		ln.Close()
		return nil, err
	}
	state, err := randomURLSafe(24)
	if err != nil {
		ln.Close()
		return nil, err
	}

	q := url.Values{}
	for k, v := range f.AuthParams {
		q[k] = v
	}
	q.Set("client_id", f.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	q.Set("code_challenge", pkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	if len(f.Scopes) > 0 {
		sep := f.ScopeSep
		if sep == "" {
			sep = " "
		}
		q.Set("scope", strings.Join(f.Scopes, sep))
	}
	a := &Authorization{
		URL: f.AuthURL + "?" + q.Encode(), verifier: verifier, state: state, redirect: redirect,
		flow: f, result: make(chan callbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.callback)
	a.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go a.srv.Serve(ln)
	return a, nil
}

// callback takes the provider's redirect. Only the one carrying our state
// counts: a page open in the browser could send the user's browser anywhere.
func (a *Authorization) callback(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if got := q.Get("state"); subtle.ConstantTimeCompare([]byte(got), []byte(a.state)) != 1 {
		http.Error(w, "This isn't the sign-in pvault started. Close this tab and try again.", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	var res callbackResult
	switch {
	case q.Get("error") != "":
		msg := q.Get("error")
		if d := q.Get("error_description"); d != "" {
			msg += ": " + d
		}
		res.err = fmt.Errorf("%s refused the sign-in (%s)", a.flow.Name, msg)
		fmt.Fprint(w, "<p>Sign-in refused. You can close this tab.</p>")
	case q.Get("code") == "":
		http.Error(w, "No code in the sign-in answer.", http.StatusBadRequest)
		return
	default:
		res.code = q.Get("code")
		fmt.Fprintf(w, "<p>%s is connected to pvault. You can close this tab.</p>", html.EscapeString(a.flow.Name))
	}
	select {
	case a.result <- res:
	default: // a second callback: the first stands
	}
}

// Close stops listening.
func (a *Authorization) Close() { a.srv.Close() }

// Wait returns the token once the user has signed in, or an error when they
// refuse or ctx ends. It stops the listener either way.
func (a *Authorization) Wait(ctx context.Context) (*Token, error) {
	defer a.Close()
	select {
	case res := <-a.result:
		if res.err != nil {
			return nil, res.err
		}
		return a.flow.exchange(ctx, url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {res.code},
			"redirect_uri":  {a.redirect},
			"code_verifier": {a.verifier},
		})
	case <-ctx.Done():
		return nil, errors.New("sign-in not finished in time")
	}
}

// tokenResponse is what every provider's token endpoint answers.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// exchange posts to the token endpoint with the client's identity added.
func (f *Flow) exchange(ctx context.Context, form url.Values) (*Token, error) {
	form.Set("client_id", f.ClientID)
	if f.ClientSecret != "" {
		form.Set("client_secret", f.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := f.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s sign-in: %w", f.Name, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil && resp.StatusCode < 400 {
		return nil, fmt.Errorf("%s sign-in: unreadable answer", f.Name)
	}
	if resp.StatusCode >= 400 || tr.Error != "" {
		// invalid_grant is what a revoked or expired refresh token gets.
		msg := fmt.Sprintf("%s sign-in refused: %s", f.Name, strings.TrimSpace(tr.Error+" "+tr.ErrorDesc))
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
			return nil, &AuthError{Msg: msg}
		}
		return nil, errors.New(msg)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("%s sign-in: no access token in the answer", f.Name)
	}
	tok := &Token{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, ClientSecret: f.ClientSecret}
	if tr.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return tok, nil
}

// Refresh gets a new access token with the refresh token. Providers that
// rotate refresh tokens (Microsoft) give a new one; the old one stays when the
// answer has none.
func (f *Flow) Refresh(ctx context.Context, old *Token) (*Token, error) {
	if old.RefreshToken == "" {
		return nil, &AuthError{Msg: f.Name + ": no refresh token saved"}
	}
	if f.ClientSecret == "" {
		f.ClientSecret = old.ClientSecret
	}
	tok, err := f.exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {old.RefreshToken},
	})
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = old.RefreshToken
	}
	if tok.ClientSecret == "" {
		tok.ClientSecret = old.ClientSecret
	}
	return tok, nil
}
