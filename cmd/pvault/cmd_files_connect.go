package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/lovincyrus/personal-vault/internal/filesync"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

// scanTimeout bounds one read of an account through its API.
const scanTimeout = 45 * time.Minute

// Where sign-ins are kept, the providers' addresses and how the browser is
// opened: the real ones, replaced only by tests.
var (
	secretStore      filesync.SecretStore = filesync.KeychainStore{}
	connectEndpoints *filesync.Endpoints
	browserOpener    = openInBrowser
)

// scanSource reads a source: a folder on this computer, or an account through
// its API. It says why when it can't, and ok is then false. where names what
// was read, for messages.
func scanSource(src vault.FileSourceInfo) (scan *filesync.Scan, where string, ok bool) {
	if src.Root != "" {
		root, err := filesync.ResolveRoot(src.Root)
		if err != nil {
			fmt.Printf("can't read %s (%v). Is the drive connected? Nothing was marked gone.\n", src.Root, err)
			return nil, "", false
		}
		return filesync.Walk(root), root, true
	}
	prov, err := filesync.Open(filesync.Spec{
		Provider: src.Provider, ClientID: src.ClientID, Tenant: src.Tenant, DriveID: src.DriveID,
	}, secretStore, filesync.ServiceName(src.ID), nil, connectEndpoints)
	if err != nil {
		var auth *filesync.AuthError
		if errors.As(err, &auth) {
			// Only a sign-in the provider or the Keychain no longer has is cured
			// by signing in again, and that keeps the source and its links.
			fmt.Printf("can't sign in (%v).\n  Sign in again: '%s'. Nothing was marked gone.\n", err, pvCmd("files source reconnect "+src.ID))
		} else {
			fmt.Printf("can't read the saved sign-in (%v): try again; nothing was changed.\n", err)
		}
		return nil, "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()
	return prov.Scan(ctx), "the account", true
}

func openInBrowser(addr string) {
	// Only a provider's https sign-in page: the address came from a template
	// with the user's own client ID, not from anything read off the network.
	if u, err := url.Parse(addr); err != nil || u.Scheme != "https" || runtime.GOOS != "darwin" {
		return
	}
	exec.Command("/usr/bin/open", addr).Run()
}

func readClientSecret(provider string) string {
	if provider != "gdrive" {
		return ""
	}
	if s := strings.TrimSpace(os.Getenv("PVAULT_GDRIVE_CLIENT_SECRET")); s != "" {
		return s
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		fatal("Google's desktop clients need their client secret: set PVAULT_GDRIVE_CLIENT_SECRET, or run this in a terminal to type it")
	}
	fmt.Fprint(os.Stderr, "Client secret of your Google OAuth client (typed hidden): ")
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		fatal("no client secret given")
	}
	return strings.TrimSpace(string(b))
}

// signIn sends the user to the provider in their browser and returns the
// sign-in they approve. It waits up to five minutes for that, no more: what
// follows has its own time.
func signIn(spec filesync.Spec) (*filesync.Flow, *filesync.Token) {
	flow, err := filesync.NewFlow(spec, connectEndpoints)
	if err != nil {
		fatal("%v", err)
	}
	auth, err := flow.StartAuthorization()
	if err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Signing in to %s in your browser. pvault asks only to READ file names, places and dates: %s.\n", flow.Name, strings.Join(flow.Scopes, ", "))
	fmt.Printf("If your browser doesn't open, open this address:\n  %s\n", auth.URL)
	browserOpener(auth.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tok, err := auth.Wait(ctx)
	if err != nil {
		fatal("%v", err)
	}
	return flow, tok
}

// connectFlags are what `source connect` and `source reconnect` take.
type connectFlags struct {
	clientID, label, site, library, tenant, port string
}

func (f *connectFlags) values() map[string]*string {
	return map[string]*string{
		"--client-id": &f.clientID, "--label": &f.label, "--site": &f.site, "--library": &f.library, "--tenant": &f.tenant, "--port": &f.port,
	}
}

func portOf(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1024 || n > 65535 {
		fatal("--port is a number from 1024 to 65535")
	}
	return n
}

func needKeychain() {
	if _, keychain := secretStore.(filesync.KeychainStore); keychain && runtime.GOOS != "darwin" {
		fatal("reading an account through its API keeps its sign-in in the macOS Keychain: this needs a Mac")
	}
}

func cmdFilesSourceConnect() {
	if len(os.Args) < 5 {
		fatal(filesUsage)
	}
	provider := os.Args[4]
	var f connectFlags
	if err := parseFlags(os.Args[5:], f.values()); err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	known := false
	for _, p := range filesync.ProviderNames {
		known = known || p == provider
	}
	switch {
	case !known:
		fatal("%q: use one of %s\n%s", provider, strings.Join(filesync.ProviderNames, ", "), filesUsage)
	case f.clientID == "":
		fatal("--client-id is needed: the ID of an OAuth client you registered with the provider (docs/usage.md says how)")
	case provider == "sharepoint" && f.site == "":
		fatal("--site is needed for sharepoint: the site's address, e.g. https://acme.sharepoint.com/sites/Studio")
	case provider != "sharepoint" && (f.site != "" || f.library != ""):
		fatal("--site and --library are for sharepoint")
	case provider != "onedrive" && provider != "sharepoint" && f.tenant != "":
		fatal("--tenant is for Microsoft accounts")
	}
	needKeychain()
	// What the vault will check when the source is added, checked now: not
	// after the user has been through a sign-in.
	probe := vault.FileSourceConfig{Label: "x", ClientID: f.clientID, Tenant: f.tenant, Site: f.site}
	if err := vault.CheckFileSourceConfig(provider, &probe); err != nil {
		fatal("%v", err)
	}
	if f.label != "" {
		if err := vault.CheckFileSourceConfig(provider, &vault.FileSourceConfig{Label: f.label, ClientID: f.clientID}); err != nil {
			fatal("%v", err)
		}
	}
	spec := filesync.Spec{Provider: provider, ClientID: f.clientID, Tenant: f.tenant, ClientSecret: readClientSecret(provider), Port: portOf(f.port)}

	flow, tok := signIn(spec)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	prov, driveID := openAfterSignIn(ctx, flow, spec, tok, f.site, f.library, &f.label)

	who := prov.Who(ctx)
	if f.label == "" {
		f.label = strings.TrimSpace(map[string]string{"onedrive": "OneDrive", "sharepoint": "SharePoint", "gdrive": "Google Drive", "dropbox": "Dropbox"}[provider] + " " + who)
	}
	cfg := map[string]string{
		"provider": provider, "client_id": f.clientID, "tenant": f.tenant, "site": f.site,
		"drive_id": driveID, "account": who, "label": f.label,
	}
	needGrant("files.*", "write")
	resp, err := apiRequest("POST", "/vault/files/sources", cfg)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var src vault.FileSourceInfo
	if err := apiResult(resp, &src); err != nil {
		fatal("%v", err)
	}
	// The sign-in is kept only in the Keychain. If it can't be, the source
	// would be one nobody can read: take it back, and say if that fails too.
	if err := filesync.SaveToken(secretStore, filesync.ServiceName(src.ID), tok); err != nil {
		if r, derr := apiRequest("DELETE", "/vault/files/sources/"+url.PathEscape(src.ID), nil); derr != nil {
			fmt.Fprintf(os.Stderr, "Could not take the source back either (%v): remove it with '%s'.\n", derr, pvCmd("files source remove "+src.ID))
		} else {
			r.Body.Close()
		}
		fatal("could not save the sign-in in the Keychain: %v", err)
	}
	fmt.Printf("Connected %s as %s.\nRun 'pvault files sync' to index it.\n", src.Label, src.ID)
}

// openAfterSignIn opens the provider with a sign-in just made and, for
// Microsoft, finds the drive to read (the user's OneDrive, or a SharePoint
// library). It returns "" for the others.
func openAfterSignIn(ctx context.Context, flow *filesync.Flow, spec filesync.Spec, tok *filesync.Token, site, library string, label *string) (filesync.Provider, string) {
	tokens := filesync.NewTokenSourceFor(flow, &filesync.MemoryStore{}, "connecting", tok)
	prov, err := filesync.OpenWith(spec, tokens, nil, connectEndpoints)
	if err != nil {
		fatal("%v", err)
	}
	g, ok := prov.(*filesync.Graph)
	if !ok {
		return prov, ""
	}
	if spec.Provider == "sharepoint" {
		id, name, err := g.Library(ctx, site, library)
		if err != nil {
			fatal("%v", err)
		}
		if *label == "" {
			*label = name
		}
		return prov, id
	}
	id, err := g.UserDrive(ctx)
	if err != nil {
		fatal("%v", err)
	}
	return prov, id
}

// cmdFilesSourceReconnect signs in again for a source whose sign-in is gone
// (revoked, expired, or not in this computer's Keychain): the source, its
// index and its links stay, and only the saved sign-in is replaced, once it is
// shown to be the same account.
func cmdFilesSourceReconnect() {
	var rest []string
	if len(os.Args) > 4 {
		rest = os.Args[4:]
	}
	rest, bools, err := splitBoolFlags(rest, "--same-account")
	if err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	args := positionalArgsUntilFlag(rest)
	if len(args) != 1 {
		fatal(filesUsage)
	}
	var port string
	if err := parseFlags(rest[len(args):], map[string]*string{"--port": &port}); err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	needKeychain()
	needGrant("files.*", "read")
	src := findSource(listFileSources(), args[0])
	if src.ClientID == "" {
		fatal("%s has no sign-in to renew: it is read from a folder, or holds files an agent found through its own connector", src.Label)
	}
	spec := filesync.Spec{Provider: src.Provider, ClientID: src.ClientID, Tenant: src.Tenant, DriveID: src.DriveID,
		ClientSecret: readClientSecret(src.Provider), Port: portOf(port)}

	flow, tok := signIn(spec)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	label := ""
	prov, driveID := openAfterSignIn(ctx, flow, spec, tok, src.Site, "", &label)

	if err := sameAccount(src, prov.Who(ctx), driveID, bools["--same-account"]); err != nil {
		fatal("%v", err)
	}
	if err := filesync.SaveToken(secretStore, filesync.ServiceName(src.ID), tok); err != nil {
		fatal("could not save the sign-in in the Keychain: %v", err)
	}
	fmt.Printf("Signed in again for %s. Its index and links are as they were; 'pvault files sync' brings them up to date.\n", src.Label)
}

// positionalArgsUntilFlag returns the leading arguments that aren't options.
func positionalArgsUntilFlag(args []string) []string {
	n := 0
	for n < len(args) && !strings.HasPrefix(args[n], "-") {
		n++
	}
	return args[:n]
}

// sameAccount refuses a sign-in that isn't the source's own: signing in as
// someone else would make the next sync mark the first account's files gone.
//
// When either side can't say whose account it is, that can't be checked: it
// goes ahead only if the user says (--same-account) they are sure.
func sameAccount(src vault.FileSourceInfo, who, driveID string, sure bool) error {
	if (src.Account == "" || who == "") && !sure {
		return fmt.Errorf("can't tell whether that sign-in is the same account as this source's: nothing was changed (add --same-account if it is)")
	}
	if src.Account != "" && who != "" && !strings.EqualFold(who, src.Account) {
		return fmt.Errorf("that sign-in is %s, but this source is %s: nothing was changed (sign in as %s)", who, src.Account, src.Account)
	}
	if src.Provider == "onedrive" && src.DriveID != "" && driveID != src.DriveID {
		return fmt.Errorf("that sign-in has another OneDrive than this source: nothing was changed")
	}
	return nil
}
