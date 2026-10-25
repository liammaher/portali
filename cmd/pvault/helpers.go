package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func vaultDir() string {
	if d := os.Getenv("VAULT_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pvault")
}

func serverAddr() string {
	if a := os.Getenv("VAULT_ADDR"); a != "" {
		return a
	}
	return "http://127.0.0.1:7200"
}

// legacySessionPath is where older versions stored the session token.
// It is no longer written; any leftover file is removed on serve and lock.
func legacySessionPath() string {
	return filepath.Join(vaultDir(), ".session")
}

func pidPath() string {
	return filepath.Join(vaultDir(), "pvault.pid")
}

func secretKeyPath() string {
	return filepath.Join(vaultDir(), "secret.key")
}

func removeLegacySessionFile() {
	os.Remove(legacySessionPath())
}

func readSecretKey() (string, error) {
	data, err := os.ReadFile(secretKeyPath())
	if err != nil {
		return "", fmt.Errorf("secret key not found at %s", secretKeyPath())
	}
	return strings.TrimSpace(string(data)), nil
}

func writePID(pid int) error {
	return os.WriteFile(pidPath(), []byte(strconv.Itoa(pid)+"\n"), 0600)
}

func readPID() (int, error) {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func removePID() {
	os.Remove(pidPath())
}

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(pw), nil
}

// cliToken caches this process's session grant so multi-step commands
// (e.g. onboard) only ask for the password once. It is never written to disk.
var cliToken string

// requestGrant exchanges the password and secret key for a short-lived
// session-level token from the running vault server, sent only over the
// verified socket.
func requestGrant(password, secretKey, ttl string) (string, error) {
	resp, err := credentialRequest("POST", "/vault/session", map[string]string{
		"password":   password,
		"secret_key": secretKey,
		"ttl":        ttl,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := apiResult(resp, &result); err != nil {
		return "", err
	}
	return result.Token, nil
}

// promptGrant returns a grant for this command. With Touch ID on, a
// fingerprint approves exactly cliGrant; otherwise, or if that fails in a
// terminal, the profile password is typed for a session grant. The password
// can only be typed in an interactive terminal, so an agent running pvault
// through a shell tool can only get what the user approves by fingerprint.
func promptGrant(ttl string) (string, error) {
	// Check the server first so the user isn't asked for a password that
	// can't be used.
	if err := requireUnlockedServer(); err != nil {
		return "", err
	}
	// The password (typed, or sent by the Touch ID helper) only goes to this
	// vault's own server; don't ask for it if that's not who answers.
	if err := checkVaultServer(); err != nil {
		return "", err
	}
	interactive := term.IsTerminal(int(syscall.Stdin))
	if touchIDEnrolled() {
		token, err := touchIDGrant()
		if err == nil {
			return token, nil
		}
		if !interactive {
			return "", err
		}
		fmt.Fprintf(os.Stderr, "%v — enter your password instead.\n", err)
	}
	if !interactive {
		return "", fmt.Errorf("this command needs your profile password, which can only be entered in an interactive terminal (or turn on Touch ID with 'pvault touchid enable')")
	}
	pw, err := promptPassword("Profile password: ")
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	sk, err := readSecretKey()
	if err != nil {
		return "", err
	}
	return requestGrant(pw, sk, ttl)
}

// requireUnlockedServer returns an error unless the vault server is running
// and unlocked.
func requireUnlockedServer() error {
	resp, err := publicRequest("GET", "/vault/status", nil)
	if err != nil {
		return fmt.Errorf("vault server not running — run 'pvault unlock'")
	}
	var status struct {
		Locked bool `json:"locked"`
	}
	if err := apiResult(resp, &status); err != nil {
		return err
	}
	if status.Locked {
		return fmt.Errorf("vault is locked — run 'pvault unlock'")
	}
	return nil
}

// sessionToken returns this process's session grant, prompting once.
func sessionToken() (string, error) {
	if cliToken == "" {
		token, err := promptGrant("2m")
		if err != nil {
			return "", err
		}
		cliToken = token
	}
	return cliToken, nil
}

// apiRequest makes an authenticated HTTP request to the vault server,
// asking for the profile password if this process has no grant yet.
func apiRequest(method, path string, body any) (*http.Response, error) {
	token, err := sessionToken()
	if err != nil {
		return nil, err
	}
	return doRequest(method, path, body, token)
}

// publicRequest makes an unauthenticated HTTP request (status, unlock, session).
func publicRequest(method, path string, body any) (*http.Response, error) {
	return doRequest(method, path, body, "")
}

// apiRawRequest makes an authenticated request with a raw (non-JSON) body,
// e.g. a document upload.
func apiRawRequest(method, path, contentType string, body io.Reader) (*http.Response, error) {
	token, err := sessionToken()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, serverAddr()+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func doRequest(method, path string, body any, token string) (*http.Response, error) {
	return doRequestWith(http.DefaultClient, serverAddr(), method, path, body, token)
}

func doRequestWith(client *http.Client, base, method, path string, body any, token string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequest(method, base+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return client.Do(req)
}

// apiResult decodes a JSON response or returns the error.
func apiResult(resp *http.Response, target any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if target != nil {
		return json.NewDecoder(resp.Body).Decode(target)
	}
	return nil
}

func fatal(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+msg+"\n", args...)
	os.Exit(1)
}

// parseFlags reads "--name value" pairs into flags. An unknown flag, a
// missing value or a stray argument is an error, so a typo like
// --max-sensitvity never silently falls back to a default.
func parseFlags(args []string, flags map[string]*string) error {
	for i := 0; i < len(args); i++ {
		p, ok := flags[args[i]]
		if !ok {
			return fmt.Errorf("unknown option %q", args[i])
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return fmt.Errorf("%s needs a value", args[i])
		}
		*p = args[i+1]
		i++
	}
	return nil
}
