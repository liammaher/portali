package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unicode"

	"golang.org/x/term"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

const signUsage = `usage:
  pvault sign list                       Signing requests waiting for you
  pvault sign review <id> [--out file]   Open the signed PDF; if you confirm, approve it and save it
  pvault sign reject <id>                Refuse a request

Store your handwritten signature once with: pvault doc add firma_autografa signature.png`

func cmdSign() {
	if len(os.Args) < 3 {
		fatal(signUsage)
	}
	switch os.Args[2] {
	case "list":
		cmdSignList()
	case "review":
		cmdSignReview()
	case "reject":
		cmdSignReject()
	default:
		fatal(signUsage)
	}
}

func cmdSignList() {
	positionalArgs(os.Args[3:], signUsage, 0, 0)
	needGrant(vault.SigningScopeID, "read")
	resp, err := apiRequest("GET", "/vault/signing", nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var list []vault.SigningInfo
	if err := apiResult(resp, &list); err != nil {
		fatal("%v", err)
	}
	if len(list) == 0 {
		fmt.Println("No signing requests.")
		return
	}
	for _, s := range list {
		fmt.Printf("%s  %-9s %-30s %d pages  from %s, %s (expires %s)\n",
			s.ID, s.Status, plainText(s.Name, 60), s.Pages, plainText(s.Requester, 40), plainText(s.Purpose, 100), s.ExpiresAt.Local().Format("15:04"))
	}
}

func cmdSignReject() {
	args := positionalArgs(os.Args[3:], signUsage, 1, 1)
	needGrant(vault.SigningScopeID, "write")
	resp, err := apiRequest("DELETE", "/vault/signing/"+url.PathEscape(args[0]), nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if err := apiResult(resp, nil); err != nil {
		fatal("%v", err)
	}
	fmt.Println("Refused. Nothing was signed.")
}

func cmdSignReview() {
	if len(os.Args) < 4 {
		fatal(signUsage)
	}
	id, out := os.Args[3], ""
	if err := parseFlags(os.Args[4:], map[string]*string{"--out": &out}); err != nil {
		fatal("%v\n%s", err, signUsage)
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		fatal("signing is approved in person: run this in an interactive terminal")
	}
	r := signReview{id: id, out: out, in: os.Stdin, w: os.Stdout, grant: signGrant, view: openFile}
	if err := r.run(); err != nil {
		fatal("%v", err)
	}
}

// signSummary is what the vault tells before anything is released.
type signSummary struct {
	ID        string `json:"id"`
	Requester string `json:"requester"`
	Name      string `json:"name"`
	Pages     int    `json:"pages"`
	Status    string `json:"status"`
	Date      string `json:"date"`
	Hash8     string `json:"hash8"`
}

// signReview is `pvault sign review`: one command that carries the human act
// from a grant to the saved file. Its parts are fields so tests can stand in
// for the terminal and the viewer.
type signReview struct {
	id, out string
	in      io.Reader
	w       io.Writer
	grant   func(s signSummary) (token string, err error)
	view    func(path string)
}

func (r signReview) run() error {
	if !vault.ValidSigningID(r.id) {
		return fmt.Errorf("%q is not a signing request ID", r.id)
	}
	sum, err := signingSummary(r.id)
	if err != nil {
		return err
	}
	if sum.Status != vault.SigningPending {
		return fmt.Errorf("that request is %s, not waiting for you", sum.Status)
	}
	if !reHash8.MatchString(sum.Hash8) {
		return errors.New("unexpected answer from the vault server")
	}
	out := r.out
	if out == "" {
		out = defaultSignedName(sum.Name)
	}
	// Make the result's place now, not after the approval: a folder that
	// doesn't exist, a name that is too long or a file already there is
	// found before anyone is asked for a password.
	place, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists: choose another with --out", out)
	}
	if err != nil {
		return fmt.Errorf("can't save to %s: %w", out, err)
	}
	c := &signCleanup{place: place, out: out}
	defer c.run()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(stop)
	go func() {
		if _, ok := <-stop; ok {
			c.run()
			os.Exit(130)
		}
	}()

	token, err := r.grant(*sum)
	if err != nil {
		return err
	}
	file, sha, err := fetchCandidate(r.id, token)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(sha, sum.Hash8) {
		return errors.New("the vault's file is not the one it announced: nothing was signed")
	}
	dir, err := os.MkdirTemp("", "pvault-sign-")
	if err != nil {
		return err
	}
	c.setDir(dir)
	if err := vault.RestrictDir(dir); err != nil {
		return err
	}
	review := filepath.Join(dir, "review.pdf")
	if err := os.WriteFile(review, file, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(r.w, "%s: %d pages, signed %s as it will go out.\nOpening %s\nsha256 %s\n", plainText(sum.Name, 60), sum.Pages, sum.Date, review, sha)
	r.view(review)

	fmt.Fprintf(r.w, "Type the first 8 characters of the hash (%s) to sign it, or just press Enter to refuse: ", sum.Hash8)
	line, _ := bufio.NewReader(r.in).ReadString('\n')
	if strings.TrimSpace(line) != sum.Hash8 {
		return errors.New("not signed")
	}

	resp, err := doRequest("POST", "/vault/signing/"+r.id+"/approve", map[string]string{"sha256": sha}, token)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	if err := apiResult(resp, nil); err != nil {
		return err
	}
	// Approved: the vault no longer holds the file. If saving fails, the
	// review copy is all there is, so it stays.
	c.keepDir()
	if _, err := place.Write(file); err != nil {
		return fmt.Errorf("approved, but saving %s failed (%v): the signed file is %s", out, err, review)
	}
	if err := place.Close(); err != nil {
		return fmt.Errorf("approved, but saving %s failed (%v): the signed file is %s", out, err, review)
	}
	c.keepOutput()
	os.RemoveAll(dir)
	fmt.Fprintf(r.w, "Signed. Saved %s\n", out)
	return nil
}

var reHash8 = regexp.MustCompile(`^[0-9a-f]{8}$`)

// signCleanup removes what a review leaves behind when it ends without a
// signed file: the review copy of the form (which holds personal data) and
// the empty file made for the result. It runs once, from the command's end or
// from a signal.
type signCleanup struct {
	mu         sync.Mutex
	done       bool
	place      *os.File
	out, dir   string
	dirKept    bool
	outputKept bool
}

func (c *signCleanup) setDir(dir string) { c.mu.Lock(); c.dir = dir; c.mu.Unlock() }
func (c *signCleanup) keepDir()          { c.mu.Lock(); c.dirKept = true; c.mu.Unlock() }
func (c *signCleanup) keepOutput()       { c.mu.Lock(); c.outputKept = true; c.mu.Unlock() }

func (c *signCleanup) run() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.done = true
	c.place.Close()
	if !c.outputKept {
		os.Remove(c.out)
	}
	if c.dir != "" && !c.dirKept {
		// On Windows a viewer that still has the PDF open keeps it there.
		if err := os.RemoveAll(c.dir); err != nil {
			fmt.Fprintf(os.Stderr, "Couldn't remove %s, which holds the signed PDF (%v): delete it once the PDF is closed.\n", c.dir, err)
		}
	}
}

// plainText is what an agent-chosen name or purpose looks like on a terminal
// or in a file name: letters, digits, spaces and . _ - ( ) only, others as ?,
// cut to max characters.
func plainText(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == max {
			b.WriteString("…")
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" ._-()", r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
		n++
	}
	return b.String()
}

// defaultSignedName is "<form>-firmato.pdf" in the current folder, from a
// name the agent chose: made plain and short enough for any file system.
func defaultSignedName(name string) string {
	stem := strings.ReplaceAll(plainText(strings.TrimSuffix(name, filepath.Ext(name)), 100), "?", "_")
	stem = strings.TrimLeft(strings.TrimSpace(stem), ".-")
	if stem == "" {
		stem = "documento"
	}
	return stem + "-firmato.pdf"
}

// signingSummary asks the vault, over its verified socket, what a request is.
func signingSummary(id string) (*signSummary, error) {
	if err := requireUnlockedServer(); err != nil {
		return nil, err
	}
	if err := checkVaultServer(); err != nil {
		return nil, err
	}
	resp, err := credentialRequest("GET", "/vault/signing/"+url.PathEscape(id)+"/summary", nil)
	if err != nil {
		return nil, err
	}
	var sum signSummary
	if err := apiResult(resp, &sum); err != nil {
		return nil, err
	}
	return &sum, nil
}

// fetchCandidate downloads the signed PDF with the sign grant and returns it
// with its own sha256, which the caller compares with what the vault said.
func fetchCandidate(id, token string) ([]byte, string, error) {
	resp, err := doRequest("GET", "/vault/signing/"+url.PathEscape(id)+"/candidate", nil, token)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", apiResult(resp, nil)
	}
	file, err := io.ReadAll(io.LimitReader(resp.Body, vault.MaxSigningPDF*2))
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(file)
	sha := hex.EncodeToString(sum[:])
	if announced := resp.Header.Get("X-Signing-Sha256"); announced != sha {
		return nil, "", errors.New("the file arrived changed: nothing was signed")
	}
	return file, sha, nil
}

// signGrant gets the user's approval for one review: a fingerprint worded
// from the request, or the typed password, exchanged over the socket for a
// grant that can only read and approve that request.
func signGrant(s signSummary) (string, error) {
	interactive := term.IsTerminal(int(syscall.Stdin))
	if touchIDEnrolled() {
		token, err := runTouchID("", "sign", "--id", s.ID)
		if err == nil && token != "" {
			return token, nil
		}
		if err == nil {
			err = errors.New("Touch ID helper returned no token")
		}
		if !interactive {
			return "", err
		}
		fmt.Fprintf(os.Stderr, "%v — enter your password instead.\n", err)
	}
	if !interactive {
		return "", errors.New("signing needs your profile password, which can only be entered in an interactive terminal")
	}
	fmt.Fprintf(os.Stderr, "Review %q (%d pages, hash %s), filed by %s. Your password lets this command open and, if you confirm, sign it.\n",
		plainText(s.Name, 60), s.Pages, s.Hash8, plainText(s.Requester, 40))
	pw, err := promptPassword("Profile password: ")
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	sk, err := readSecretKey()
	if err != nil {
		return "", err
	}
	resp, err := credentialRequest("POST", "/vault/session", map[string]string{
		"password": pw, "secret_key": sk, "sign": s.ID,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Token string `json:"token"`
		Sign  string `json:"sign"`
	}
	if err := apiResult(resp, &result); err != nil {
		return "", err
	}
	// An older server ignores sign and grants full access: more than the
	// user approved, so it goes unused.
	if result.Sign != s.ID {
		return "", fmt.Errorf("the running vault server doesn't support signing — restart it with this version ('%s', then '%s')", pvCmd("lock"), pvCmd("unlock"))
	}
	return result.Token, nil
}

// openFile shows path in the default viewer, if there is one.
func openFile(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "windows":
		shellOpen(path)
		return
	default:
		return
	}
	cmd.Start()
}
