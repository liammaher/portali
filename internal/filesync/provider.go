package filesync

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// Provider reads one account's files through its API: names, places, sizes,
// dates and the provider's own hash, never the content, and nothing is
// written, moved or deleted (the sign-in only asks for read-only access).
type Provider interface {
	// Scan lists every file of the source. Anything that goes wrong lands in
	// Scan.Errors, which makes the scan incomplete: its caller then marks
	// nothing gone.
	Scan(ctx context.Context) *Scan
	// Who names the signed-in account, best effort ("" when it can't).
	Who(ctx context.Context) string
}

// cleanName makes a provider's name something the index holds: no slash (Drive
// allows one in a name), control or direction-override characters, at most 255
// bytes.
func cleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/':
			return '∕'
		case r == '\\':
			return '＼'
		case unicode.IsControl(r), r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F,
			r == 0x061C, r == 0x2028, r == 0x2029:
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	for len(name) > 255 {
		_, n := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-n]
	}
	return name
}

// cleanPath cleans each segment of a path.
func cleanPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = cleanName(p)
	}
	return strings.Join(parts, "/")
}

// add records one observation. A web address the vault won't keep (a sharing
// link) is dropped, not the file. A file the index still can't hold is
// reported as kept: it exists, so a sync must not mark it gone.
func (s *Scan) add(o vault.FileObservation) {
	if len(s.Files) >= MaxFiles {
		s.Truncated = true
		return
	}
	o.Name, o.Path = cleanName(o.Name), cleanPath(o.Path)
	if err := o.Check(); err != nil && o.WebURL != "" {
		o.WebURL = ""
		err = o.Check()
		if err == nil {
			s.Files = append(s.Files, o)
			return
		}
	} else if err == nil {
		s.Files = append(s.Files, o)
		return
	}
	s.Skipped++
	// Only a key the vault accepts can be reported; one it doesn't would make
	// it refuse the whole batch.
	if keep := (vault.FileObservation{Key: o.Key, Keep: true}); keep.Check() == nil {
		s.Files = append(s.Files, keep)
	}
}

// fail records an error that makes the scan incomplete.
func (s *Scan) fail(err error) { s.Errors = append(s.Errors, err) }

// Spec is what it takes to read an account: which provider, which OAuth
// client the user registered, and where in the account.
type Spec struct {
	Provider     string // onedrive, sharepoint, gdrive, dropbox
	ClientID     string
	ClientSecret string // Google's desktop clients; empty for the others
	Tenant       string // Microsoft: common (default), organizations, consumers or a tenant
	DriveID      string // Microsoft: the OneDrive or SharePoint library; empty is the user's OneDrive
	Port         int    // loopback port for signing in (0 for the provider's default)
}

// Endpoints are a provider's addresses, replaceable in tests.
type Endpoints struct {
	AuthURL, TokenURL, API string
}

// DefaultPort is the loopback port a sign-in listens on: register
// http://localhost:53682/ as the redirect address of the OAuth client.
const DefaultPort = 53682

// ProviderNames are the providers read through their APIs.
var ProviderNames = []string{"onedrive", "sharepoint", "gdrive", "dropbox"}

// NewFlow is the sign-in flow of a provider for this client.
func NewFlow(spec Spec, ep *Endpoints) (*Flow, error) {
	if spec.ClientID == "" {
		return nil, fmt.Errorf("an OAuth client ID is needed: register one with the provider (docs/usage.md says how)")
	}
	port := spec.Port
	if port == 0 {
		port = DefaultPort
	}
	f := &Flow{ClientID: spec.ClientID, ClientSecret: spec.ClientSecret, Port: port}
	switch spec.Provider {
	case "onedrive", "sharepoint":
		tenant := spec.Tenant
		if tenant == "" {
			tenant = "common"
		}
		f.Name = "Microsoft"
		f.AuthURL = "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0/authorize"
		f.TokenURL = "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0/token"
		// Read-only. A user's own OneDrive needs Files.Read; SharePoint libraries
		// (and files shared with the user) need the .All scopes. offline_access
		// is what gives a refresh token.
		// User.Read says whose account it is, which reconnecting checks.
		f.Scopes = []string{"Files.Read", "User.Read", "offline_access"}
		if spec.Provider == "sharepoint" {
			f.Scopes = []string{"Files.Read.All", "Sites.Read.All", "User.Read", "offline_access"}
		}
		f.AuthParams = url.Values{"response_mode": {"query"}}
	case "gdrive":
		f.Name = "Google"
		f.AuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
		f.TokenURL = "https://oauth2.googleapis.com/token"
		f.Scopes = []string{"https://www.googleapis.com/auth/drive.metadata.readonly"} // names and places, no content
		f.AuthParams = url.Values{"access_type": {"offline"}, "prompt": {"consent"}}
		f.RedirectHost = "127.0.0.1"
	case "dropbox":
		f.Name = "Dropbox"
		f.AuthURL = "https://www.dropbox.com/oauth2/authorize"
		f.TokenURL = "https://api.dropboxapi.com/oauth2/token"
		f.Scopes = []string{"files.metadata.read", "account_info.read"}
		f.AuthParams = url.Values{"token_access_type": {"offline"}}
	default:
		return nil, fmt.Errorf("%q is not read through an API: use one of %v", spec.Provider, ProviderNames)
	}
	if ep != nil {
		if ep.AuthURL != "" {
			f.AuthURL = ep.AuthURL
		}
		if ep.TokenURL != "" {
			f.TokenURL = ep.TokenURL
		}
	}
	return f, nil
}

// apiBase is a provider's API address.
func apiBase(provider string, ep *Endpoints) string {
	if ep != nil && ep.API != "" {
		return ep.API
	}
	switch provider {
	case "onedrive", "sharepoint":
		return "https://graph.microsoft.com/v1.0"
	case "gdrive":
		return "https://www.googleapis.com/drive/v3"
	}
	return "https://api.dropboxapi.com/2"
}

// Open returns the provider for a source whose sign-in is saved under service.
func Open(spec Spec, store SecretStore, service string, hc *http.Client, ep *Endpoints) (Provider, error) {
	flow, err := NewFlow(spec, ep)
	if err != nil {
		return nil, err
	}
	tokens, err := NewTokenSource(flow, store, service)
	if err != nil {
		return nil, err
	}
	return OpenWith(spec, tokens, hc, ep)
}

// OpenWith is Open with the tokens already in hand (right after signing in).
func OpenWith(spec Spec, tokens *TokenSource, hc *http.Client, ep *Endpoints) (Provider, error) {
	api, err := newAPIClient(hc, tokens, apiBase(spec.Provider, ep))
	if err != nil {
		return nil, err
	}
	base := apiBase(spec.Provider, ep)
	switch spec.Provider {
	case "onedrive", "sharepoint":
		return &Graph{api: api, base: base, driveID: spec.DriveID}, nil
	case "gdrive":
		return &Drive{api: api, base: base}, nil
	case "dropbox":
		return &Dropbox{api: api, base: base}, nil
	}
	return nil, fmt.Errorf("%q is not read through an API", spec.Provider)
}
