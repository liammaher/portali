package main

import (
	"context"
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
		fmt.Printf("can't sign in (%v).\n  Connect it again: '%s', then '%s'. Nothing was marked gone.\n", err,
			pvCmd("files source remove "+src.ID), pvCmd("files source connect "+src.Provider+" --client-id ..."))
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

func cmdFilesSourceConnect() {
	if len(os.Args) < 5 {
		fatal(filesUsage)
	}
	provider := os.Args[4]
	var clientID, label, site, library, tenant, port string
	if err := parseFlags(os.Args[5:], map[string]*string{
		"--client-id": &clientID, "--label": &label, "--site": &site, "--library": &library, "--tenant": &tenant, "--port": &port,
	}); err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	known := false
	for _, p := range filesync.ProviderNames {
		known = known || p == provider
	}
	switch {
	case !known:
		fatal("%q: use one of %s\n%s", provider, strings.Join(filesync.ProviderNames, ", "), filesUsage)
	case clientID == "":
		fatal("--client-id is needed: the ID of an OAuth client you registered with the provider (docs/usage.md says how)")
	case provider == "sharepoint" && site == "":
		fatal("--site is needed for sharepoint: the site's address, e.g. https://acme.sharepoint.com/sites/Studio")
	case provider != "sharepoint" && (site != "" || library != ""):
		fatal("--site and --library are for sharepoint")
	case provider != "onedrive" && provider != "sharepoint" && tenant != "":
		fatal("--tenant is for Microsoft accounts")
	}
	if _, keychain := secretStore.(filesync.KeychainStore); keychain && runtime.GOOS != "darwin" {
		fatal("reading an account through its API keeps its sign-in in the macOS Keychain: this needs a Mac")
	}
	spec := filesync.Spec{Provider: provider, ClientID: clientID, Tenant: tenant, ClientSecret: readClientSecret(provider)}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1024 || n > 65535 {
			fatal("--port is a number from 1024 to 65535")
		}
		spec.Port = n
	}

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

	// Find which drive to read and whose it is, with the new sign-in.
	tokens := filesync.NewTokenSourceFor(flow, &filesync.MemoryStore{}, "connecting", tok)
	prov, err := filesync.OpenWith(spec, tokens, nil, connectEndpoints)
	if err != nil {
		fatal("%v", err)
	}
	cfg := map[string]string{"provider": provider, "client_id": clientID, "tenant": tenant, "site": site}
	if g, ok := prov.(*filesync.Graph); ok {
		if provider == "sharepoint" {
			id, name, err := g.Library(ctx, site, library)
			if err != nil {
				fatal("%v", err)
			}
			cfg["drive_id"] = id
			if label == "" {
				label = name
			}
		} else if cfg["drive_id"], err = g.UserDrive(ctx); err != nil {
			fatal("%v", err)
		}
	}
	who := prov.Who(ctx)
	cfg["account"] = who
	if label == "" {
		label = strings.TrimSpace(map[string]string{"onedrive": "OneDrive", "sharepoint": "SharePoint", "gdrive": "Google Drive", "dropbox": "Dropbox"}[provider] + " " + who)
	}
	cfg["label"] = label

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
	// would be one nobody can read: take it back.
	if err := filesync.SaveToken(secretStore, filesync.ServiceName(src.ID), tok); err != nil {
		if r, derr := apiRequest("DELETE", "/vault/files/sources/"+url.PathEscape(src.ID), nil); derr == nil {
			r.Body.Close()
		}
		fatal("could not save the sign-in in the Keychain: %v", err)
	}
	fmt.Printf("Connected %s as %s.\nRun 'pvault files sync' to index it.\n", src.Label, src.ID)
}
