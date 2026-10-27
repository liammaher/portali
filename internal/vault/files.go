package vault

import (
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lovincyrus/personal-vault/internal/crypto"
	"github.com/lovincyrus/personal-vault/internal/store"
)

// The file index: where the user's files are (a folder on disk, OneDrive,
// SharePoint, Google Drive, Dropbox) and whom they are linked to. It holds
// metadata and links only; a file's bytes and text never enter the vault, and
// the vault never goes online (the sync client reads the sources and writes
// the index through the API).
//
// Names, paths, URLs, sizes and hashes are encrypted, each blob bound to its
// row ID. The plain columns are what a scope check needs: the holder's slug,
// the link kind, the pratica's slug (both already plain elsewhere) and tiers.
// Search runs over the decrypted blobs in memory, so there is no plaintext
// index. Nothing is deleted on its own: a file that left its source is
// tombstoned, and removing a source, a person or old tombstones is explicit.

const (
	FileKindPerson   = "person"   // holder "" is the owner, else a person
	FileKindClient   = "client"   // holder is a client of the business
	FileKindBusiness = "business" // the business itself (holder "")
	FileKindPratica  = "pratica"  // a pratica of the owner's

	FileOriginManual     = "manual"
	FileOriginRule       = "rule"
	FileOriginSuggestion = "suggestion"
)

// FileProviders are the places files can be read from.
var FileProviders = []string{"local", "onedrive", "sharepoint", "gdrive", "dropbox"}

// ErrFileNotFound is returned for a file, link, rule, source or suggestion
// that doesn't exist or that the caller can't see.
var ErrFileNotFound = errors.New("not found")

const (
	maxFilePath      = 2048
	maxFileKey       = 1024
	maxFileLinkText  = 500
	maxFileRole      = 64
	defaultFileLimit = 200
	maxFileLimit     = 1000

	// maxPendingSuggestions bounds what an agent can queue for the user.
	maxPendingSuggestions = 200
)

// FilesScopeID is the ID token scopes are checked against for files held by
// holder: "files.index" for the owner and the business, "leo:files.index" for
// a person. "files.*" and "leo:files.*" grant them.
func FilesScopeID(holder string) string {
	if holder == "" {
		return "files.index"
	}
	return holder + ":files.index"
}

// DefaultFileTier is the tier of a file: sensitive (a name can carry a
// client's), or personal when the name or path holds a login-secret word
// (the same words as IsCredentialField), so an agent never sees it.
func DefaultFileTier(name, filePath string) string {
	for _, text := range []string{name + " " + filePath, name} {
		if IsCredentialField("files." + fileWordsID(text)) {
			return TierPersonal
		}
	}
	return "sensitive"
}

var reNonWord = regexp.MustCompile(`[^a-z0-9]+`)

// fileWordsID turns text into one field-name-like word string. It never cuts
// the text short: a credential word at the end of a long path counts.
func fileWordsID(s string) string {
	return strings.Trim(reNonWord.ReplaceAllString(strings.ToLower(foldText(s)), "_"), "_")
}

var accentFold = strings.NewReplacer("à", "a", "á", "a", "è", "e", "é", "e", "ì", "i", "í", "i", "ò", "o", "ó", "o", "ù", "u", "ú", "u")

// foldText lower-cases and drops Italian accents, for matching.
func foldText(s string) string { return accentFold.Replace(strings.ToLower(s)) }

func maxTier(a, b string) string {
	if tierRank[b] > tierRank[a] {
		return b
	}
	return a
}

// FileLocation is one place a file is: its key at the source (the provider's
// item ID, or the path below a local root), its path below the source's root
// and, for an online file, the provider's page for it.
type FileLocation struct {
	Source string `json:"source"`
	Key    string `json:"key"`
	Path   string `json:"path,omitempty"`
	WebURL string `json:"web_url,omitempty"`
}

// FileMeta is what the encrypted blob of a file holds.
type FileMeta struct {
	Name         string         `json:"name"`
	Locations    []FileLocation `json:"locations"`
	MIME         string         `json:"mime,omitempty"`
	Size         int64          `json:"size,omitempty"`
	Mtime        string         `json:"mtime,omitempty"`
	ProviderHash string         `json:"provider_hash,omitempty"`
	SHA256       string         `json:"sha256,omitempty"`
}

// FileInfo is a file with the links the caller may see.
type FileInfo struct {
	ID        string    `json:"id"`
	SourceID  string    `json:"source_id"`
	Tier      string    `json:"tier"`
	Gone      bool      `json:"gone,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	FileMeta
	Links []FileLinkInfo `json:"links"`
}

// FileLinkInfo is one link of a file.
type FileLinkInfo struct {
	ID          string    `json:"id"`
	FileID      string    `json:"file_id"`
	Holder      string    `json:"holder"`
	Kind        string    `json:"kind"`
	PraticaSlug string    `json:"pratica,omitempty"`
	Origin      string    `json:"origin"`
	Tier        string    `json:"tier"`
	Role        string    `json:"role,omitempty"`
	Note        string    `json:"note,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// FileSourceConfig is what a source's encrypted blob holds.
type FileSourceConfig struct {
	Label    string `json:"label"`
	Root     string `json:"root,omitempty"`    // a folder on this computer; none for an account read through the provider's API
	Account  string `json:"account,omitempty"` // the account or site, for display
	Cursor   string `json:"cursor,omitempty"`  // the provider's change feed position
	LastSync string `json:"last_sync,omitempty"`
	// An account read through the provider's API (no Root): the OAuth client
	// the user registered, which library or drive, and where to find it. The
	// tokens are never here: they are in the macOS Keychain.
	ClientID string `json:"client_id,omitempty"`
	Tenant   string `json:"tenant,omitempty"`   // Microsoft: common, organizations, or a tenant ID
	DriveID  string `json:"drive_id,omitempty"` // Microsoft: the OneDrive or SharePoint library
	Site     string `json:"site,omitempty"`     // SharePoint: the site's address, for display
	Via      string `json:"via,omitempty"`      // ViaConnector for a source an agent feeds
}

// reClientID is what an OAuth client ID may look like: the three providers' are
// letters, digits and a few separators (a Microsoft one is a GUID, Google's ends
// in .apps.googleusercontent.com).
var reClientID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~@-]{0,199}$`)

// reTenant is a Microsoft tenant: common, organizations, consumers, a GUID or a domain.
var reTenant = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,99}$`)

// reDriveID is a Microsoft Graph drive ID (b!... base64url).
var reDriveID = regexp.MustCompile(`^[A-Za-z0-9!_.=-]{1,200}$`)

// FileSourceInfo is a source as listed.
type FileSourceInfo struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	FileSourceConfig
	Files int `json:"files"`
}

type fileLinkBlob struct {
	Role string `json:"role,omitempty"`
	Note string `json:"note,omitempty"`
}

type fileRuleBlob struct {
	Path string `json:"path"`
}

type fileSuggestionBlob struct {
	Reason string `json:"reason,omitempty"`
}

// fileKey derives the subkey every file-index blob is encrypted under. The
// "files/" info has a slash, which no category name can have, so it can't
// collide with a category's key.
func (v *Vault) fileKey(vaultKey []byte) ([]byte, error) {
	return crypto.DeriveSubkey(vaultKey, v.salt, "files/index")
}

func fileAAD(kind, id string) []byte { return []byte(kind + ":" + id) }

func sealFileBlob(key []byte, kind, id string, x any) (string, error) {
	plain, err := json.Marshal(x)
	if err != nil {
		return "", err
	}
	ct, err := crypto.EncryptWithAAD(key, plain, fileAAD(kind, id))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

func openFileBlob(key []byte, kind, id, blob string, x any) error {
	ct, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return fmt.Errorf("decode %s %s: %w", kind, id, err)
	}
	plain, err := crypto.DecryptWithAAD(key, ct, fileAAD(kind, id))
	if err != nil {
		return fmt.Errorf("decrypt %s %s: %w", kind, id, err)
	}
	return json.Unmarshal(plain, x)
}

func newFileID() (string, error) {
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// withFileKey runs fn with the file subkey, cleared after.
func (v *Vault) withFileKey(fn func(key []byte) error) error {
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return err
	}
	defer clear(vaultKey)
	key, err := v.fileKey(vaultKey)
	if err != nil {
		return err
	}
	defer clear(key)
	return fn(key)
}

func (v *Vault) auditFile(scope, action, purpose string) {
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: scope, Action: action, Purpose: purpose})
}

// --- validation -------------------------------------------------------------

// hasUnsafe reports control characters and the bidi overrides that make a name
// read differently from what it is (a file "gpj.exe" shown as "exe.jpg").
func hasUnsafe(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0x200E || r == 0x200F {
			return true
		}
	}
	return false
}

// CleanFilePath normalizes a path below a source's root to "/a/b/c".
func CleanFilePath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.TrimSpace(p) == "" {
		return "", nil
	}
	if len(p) > maxFilePath || !utf8.ValidString(p) || hasUnsafe(p) {
		return "", &ValidationError{Field: "path", Msg: "path is too long or has control characters"}
	}
	return path.Clean("/" + p), nil
}

// CheckFileURL refuses a URL that is not a plain https page for a file. A
// sharing link is a bearer credential (whoever has it can open the file), so
// it is never stored, whatever the tier.
func CheckFileURL(raw string) error {
	if raw == "" {
		return nil
	}
	bad := func(msg string) error { return &ValidationError{Field: "web_url", Msg: msg} }
	if len(raw) > maxFilePath || hasUnsafe(raw) {
		return bad("URL is too long or has control characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return bad("only a plain https URL is kept")
	}
	host, p := strings.ToLower(u.Hostname()), u.Path
	switch {
	case host == "1drv.ms", host == "dl.dropboxusercontent.com", host == "drive.usercontent.google.com":
		return bad("sharing and download links are never stored: they give anyone who has them the file")
	case strings.HasPrefix(p, "/:"), strings.Contains(p, "/sharing"), strings.Contains(p, "/_layouts/15/guestaccess"):
		return bad("sharing links are never stored: they give anyone who has them the file")
	case strings.HasSuffix(host, "dropbox.com") && (strings.HasPrefix(p, "/s/") || strings.HasPrefix(p, "/scl/") || strings.HasPrefix(p, "/sh/")):
		return bad("sharing links are never stored: they give anyone who has them the file")
	}
	for k, vals := range u.Query() {
		switch strings.ToLower(k) {
		case "usp":
			// Drive's own page for a file carries usp=drivesdk; usp=sharing (or
			// drive_link) is a link to hand to others.
			if len(vals) != 1 || vals[0] != "drivesdk" {
				return bad("a sharing link is never stored")
			}
		case "e", "key", "rlkey", "token", "access_token", "sig", "sv", "share", "download", "authkey", "resourcekey":
			return bad("a URL with a key or token in it is never stored")
		}
	}
	return nil
}

func cleanFileKey(k string) (string, error) {
	if k == "" || len(k) > maxFileKey || !utf8.ValidString(k) || hasUnsafe(k) {
		return "", &ValidationError{Field: "key", Msg: "every file needs a key (the provider's item ID or its path), up to 1024 characters"}
	}
	return k, nil
}

func validFileProvider(p string) bool {
	for _, q := range FileProviders {
		if p == q {
			return true
		}
	}
	return false
}

// checkFileTarget validates whom a link, rule or suggestion is for and
// returns the kind, which a person's slug alone does not give.
func (v *Vault) checkFileTarget(allow FieldFilter, holder, kind, pratica string) error {
	bad := func(msg string) error { return &ValidationError{Field: "for", Msg: msg} }
	switch kind {
	case FileKindPerson:
		if holder == "" && v.kind == KindImpresa {
			return bad("in a business vault, link to the business with kind business")
		}
	case FileKindClient:
		if holder == "" {
			return bad("a client is a person: name their slug")
		}
	case FileKindBusiness:
		if holder != "" || v.kind != KindImpresa {
			return bad("the business is the owner of a business vault: no person, and only in a business vault")
		}
	case FileKindPratica:
		if holder != "" {
			return bad("a pratica is the owner's: link the file to the person separately")
		}
		if !rePraticaSlug.MatchString(pratica) {
			return bad("pratica needs a slug")
		}
		f, err := v.db.GetField("pratiche." + pratica + "_stato")
		if err != nil {
			return err
		}
		// A caller that can't read the pratica is told what it is told about
		// a missing one, so it can't probe for slugs.
		if f == nil || (allow != nil && !allow(f.ID, f.Sensitivity)) {
			return bad("there is no pratica \"" + pratica + "\" that you can see")
		}
		return nil
	default:
		return bad("kind must be person, client, business or pratica")
	}
	if pratica != "" {
		return bad("only a pratica link names a pratica")
	}
	if holder == "" {
		return nil
	}
	if !ValidPersonSlug(holder) {
		return &ValidationError{Field: "for", Msg: errPersonSlug(holder).Error()}
	}
	exists, err := v.db.PersonExists(holder)
	if err != nil {
		return err
	}
	if !exists {
		return bad("there is no person \"" + holder + "\" in the vault: add them first with 'pvault person add'")
	}
	return nil
}

func cleanFileText(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > max || !utf8.ValidString(s) || hasUnsafe(s) {
		return "", &ValidationError{Field: field, Msg: fmt.Sprintf("up to %d characters, no control characters", max)}
	}
	return s, nil
}

// --- sources ---------------------------------------------------------------

// CheckFileSourceConfig cleans a source's settings in place and refuses what a
// source can't have. A client can run it before sending the user off to sign in.
func CheckFileSourceConfig(provider string, cfg *FileSourceConfig) error {
	if !validFileProvider(provider) {
		return &ValidationError{Field: "provider", Msg: "use one of " + strings.Join(FileProviders, ", ")}
	}
	var err error
	if cfg.Label, err = cleanFileText("label", cfg.Label, 80); err != nil || cfg.Label == "" {
		if err == nil {
			err = &ValidationError{Field: "label", Msg: "a source needs a label"}
		}
		return err
	}
	for _, f := range []struct {
		name string
		p    *string
		max  int
	}{{"root", &cfg.Root, maxFilePath}, {"account", &cfg.Account, 200}, {"client_id", &cfg.ClientID, 200},
		{"drive_id", &cfg.DriveID, 200}, {"tenant", &cfg.Tenant, 100}, {"site", &cfg.Site, 400}, {"via", &cfg.Via, 20}} {
		if *f.p, err = cleanFileText(f.name, *f.p, f.max); err != nil {
			return err
		}
	}
	switch {
	case cfg.Via != "" && cfg.Via != ViaConnector:
		return &ValidationError{Field: "via", Msg: "not a way of reading a source"}
	case cfg.Via != "" && (cfg.Root != "" || cfg.ClientID != "" || provider == "local"):
		return &ValidationError{Field: "via", Msg: "a source fed by an agent has no folder and no OAuth client"}
	case provider == "local" && cfg.Root == "":
		return &ValidationError{Field: "root", Msg: "a local source needs the folder to read"}
	case cfg.Root == "" && cfg.ClientID == "" && cfg.Via == "":
		return &ValidationError{Field: "client_id", Msg: "a source needs a folder to read, or an OAuth client to read the account through"}
	case cfg.Root != "" && cfg.ClientID != "":
		return &ValidationError{Field: "client_id", Msg: "a source is read from a folder or through the provider's API, not both"}
	case cfg.ClientID != "" && provider == "local":
		return &ValidationError{Field: "provider", Msg: "a folder on this computer isn't read through an API"}
	case cfg.ClientID != "" && !reClientID.MatchString(cfg.ClientID):
		return &ValidationError{Field: "client_id", Msg: "not an OAuth client ID"}
	case cfg.Tenant != "" && !reTenant.MatchString(cfg.Tenant):
		return &ValidationError{Field: "tenant", Msg: "common, organizations, consumers, a tenant ID or its domain"}
	case cfg.DriveID != "" && !reDriveID.MatchString(cfg.DriveID):
		return &ValidationError{Field: "drive_id", Msg: "not a drive ID"}
	}
	cfg.Cursor, cfg.LastSync = "", ""
	return nil
}

// ViaConnector marks a source with no folder and no sign-in of its own: its
// files are ones an agent found through its own connector and the user
// accepted. Nothing syncs it, so nothing in it is ever marked gone.
const ViaConnector = "connector"

// AddFileSource registers a place to read files from. The session adds sources.
func (v *Vault) AddFileSource(provider string, cfg FileSourceConfig) (*FileSourceInfo, error) {
	if err := CheckFileSourceConfig(provider, &cfg); err != nil {
		return nil, err
	}
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	return v.addFileSource(provider, cfg)
}

func (v *Vault) addFileSource(provider string, cfg FileSourceConfig) (*FileSourceInfo, error) {
	id, err := newFileID()
	if err != nil {
		return nil, err
	}
	err = v.withFileKey(func(key []byte) error {
		blob, err := sealFileBlob(key, "filesource", id, cfg)
		if err != nil {
			return err
		}
		now := time.Now()
		return v.db.CreateFileSource(store.FileSource{ID: id, Provider: provider, Blob: blob, CreatedAt: now, UpdatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	v.auditFile(FilesScopeID(""), "file_source_add", provider)
	v.markChanged()
	return &FileSourceInfo{ID: id, Provider: provider, FileSourceConfig: cfg}, nil
}

// FileSources lists the sources with how many files each has that allow may
// see (allow nil: all).
func (v *Vault) FileSources(allow FieldFilter) ([]FileSourceInfo, error) {
	var out []FileSourceInfo
	err := v.withFileKey(func(key []byte) error {
		srcs, err := v.db.ListFileSources()
		if err != nil {
			return err
		}
		var counts map[string]int
		if allow == nil {
			counts, err = v.db.CountFilesBySource()
		} else {
			counts, err = v.countVisibleFiles(allow)
		}
		if err != nil {
			return err
		}
		for _, s := range srcs {
			info, err := openFileSource(key, s)
			if err != nil {
				return err
			}
			info.Files = counts[s.ID]
			out = append(out, *info)
		}
		return nil
	})
	return out, err
}

// countVisibleFiles counts, per source, the files allow may see.
func (v *Vault) countVisibleFiles(allow FieldFilter) (map[string]int, error) {
	files, err := v.db.ListFiles("")
	if err != nil {
		return nil, err
	}
	links, err := v.db.ListFileLinks("")
	if err != nil {
		return nil, err
	}
	linksOf := map[string][]store.FileLink{}
	for _, l := range links {
		linksOf[l.FileID] = append(linksOf[l.FileID], l)
	}
	counts := map[string]int{}
	for _, f := range files {
		if _, ok := visibleFileLinks(allow, f, linksOf[f.ID]); ok {
			counts[f.SourceID]++
		}
	}
	return counts, nil
}

func openFileSource(key []byte, s store.FileSource) (*FileSourceInfo, error) {
	info := &FileSourceInfo{ID: s.ID, Provider: s.Provider}
	if err := openFileBlob(key, "filesource", s.ID, s.Blob, &info.FileSourceConfig); err != nil {
		return nil, err
	}
	return info, nil
}

// SetFileSourceState records a sync's cursor and time on its source.
func (v *Vault) SetFileSourceState(id, cursor string) error {
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	return v.withFileKey(func(key []byte) error {
		s, err := v.db.GetFileSource(id)
		if err != nil {
			return err
		}
		if s == nil {
			return ErrFileNotFound
		}
		info, err := openFileSource(key, *s)
		if err != nil {
			return err
		}
		if len(cursor) > 8192 || !utf8.ValidString(cursor) || hasUnsafe(cursor) {
			return &ValidationError{Field: "cursor", Msg: "up to 8192 characters, no control characters"}
		}
		info.Cursor, info.LastSync = cursor, time.Now().UTC().Format(time.RFC3339)
		blob, err := sealFileBlob(key, "filesource", id, info.FileSourceConfig)
		if err != nil {
			return err
		}
		if err := v.db.UpdateFileSource(id, blob, time.Now()); err != nil {
			return err
		}
		v.markChanged()
		return nil
	})
}

// ForgetFileSource erases a source with its files, links, rules and
// suggestions. Nothing at the source itself is touched. It returns how many
// files went.
func (v *Vault) ForgetFileSource(id string) (int64, error) {
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	if err := v.checkUnlocked(); err != nil {
		return 0, err
	}
	s, err := v.db.GetFileSource(id)
	if err != nil {
		return 0, err
	}
	if s == nil {
		return 0, ErrFileNotFound
	}
	n, err := v.db.DeleteFileSource(id)
	if err != nil {
		return 0, err
	}
	v.dropFileSyncs()
	v.auditFile(FilesScopeID(""), "files_forget", fmt.Sprintf("%d files", n))
	v.markChanged()
	return n, nil
}

// --- sync -----------------------------------------------------------------

// FileObservation is what a sync client saw of one file at a source.
type FileObservation struct {
	Key          string `json:"key"` // the provider's item ID, or the path below a local root
	Path         string `json:"path,omitempty"`
	WebURL       string `json:"web_url,omitempty"`
	Name         string `json:"name"`
	MIME         string `json:"mime,omitempty"`
	Size         int64  `json:"size,omitempty"`
	Mtime        string `json:"mtime,omitempty"`
	ProviderHash string `json:"provider_hash,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Deleted      bool   `json:"deleted,omitempty"` // a change feed's "gone"
	// Keep says the file exists but the client can't describe it in a way the
	// index accepts: it only counts as seen, so the sync's finish doesn't mark
	// a file gone that is still there.
	Keep bool `json:"keep,omitempty"`
}

// FileSyncResult counts what a sync changed.
type FileSyncResult struct {
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Gone      int `json:"gone"`
	Linked    int `json:"linked"` // links the source's rules made
}

type openedFile struct {
	row   store.File
	meta  FileMeta
	index int // position of the source's location in meta.Locations, -1 if none
}

// Check cleans an observation in place and refuses what the index can't
// hold. A sync client runs it on each file first, so one odd name is left
// out instead of making the vault refuse a whole batch.
func (o *FileObservation) Check() error {
	var err error
	if o.Key, err = cleanFileKey(o.Key); err != nil {
		return err
	}
	if o.Deleted || o.Keep {
		return nil
	}
	if strings.ContainsAny(o.Name, `/\`) || hasUnsafe(o.Name) {
		return &ValidationError{Field: "name", Msg: "filename has a slash, control or direction-override character"}
	}
	if o.Name, err = cleanDocumentName(o.Name); err != nil {
		return err
	}
	if o.Path, err = CleanFilePath(o.Path); err != nil {
		return err
	}
	if err := CheckFileURL(o.WebURL); err != nil {
		return err
	}
	if o.MIME, err = cleanFileText("mime", o.MIME, 127); err != nil {
		return err
	}
	if o.Mtime, err = cleanFileText("mtime", o.Mtime, 40); err != nil {
		return err
	}
	if o.ProviderHash, err = cleanFileText("provider_hash", o.ProviderHash, 200); err != nil {
		return err
	}
	if o.SHA256, err = cleanFileText("sha256", o.SHA256, 64); err != nil {
		return err
	}
	o.Size = max(o.Size, 0)
	return nil
}

// UpsertFiles records what a sync saw at a source. If full is set, the
// observations are the whole source and any file not among them is
// tombstoned; a change feed sends full=false with Deleted entries instead.
// A file is matched by its key at the source. Tombstoned files return when
// seen again.
func (v *Vault) UpsertFiles(sourceID string, obs []FileObservation, full bool) (*FileSyncResult, error) {
	return v.upsertFiles(nil, sourceID, obs, full, "", false)
}

// UpsertFilesIf is UpsertFiles for a caller with a filter (a service token's):
// it leaves alone the files the caller couldn't see, so a sync token can't
// rewrite or tombstone a person's files, or learn from the counts that its
// guess at one was right.
func (v *Vault) UpsertFilesIf(allow FieldFilter, sourceID string, obs []FileObservation, full bool) (*FileSyncResult, error) {
	return v.upsertFiles(allow, sourceID, obs, full, "", false)
}

// UpsertFileBatch is UpsertFiles for one batch of a longer sync: it remembers
// the keys it saw under syncID (in memory only), and FinishFileSync
// tombstones the rest. A request can't carry a whole large source, so a sync
// of one is many batches and a finish.
//
// The first batch says begin. A later batch of a sync the vault doesn't
// remember (it locked, restarted, pruned or began another sync of the source)
// is refused with ErrSyncExpired: carrying on would let finish mark every
// file named only by the lost batches gone.
func (v *Vault) UpsertFileBatch(sourceID, syncID string, begin bool, obs []FileObservation) (*FileSyncResult, error) {
	return v.UpsertFileBatchIf(nil, sourceID, syncID, begin, obs)
}

// UpsertFileBatchIf is UpsertFileBatch for a caller with a filter.
func (v *Vault) UpsertFileBatchIf(allow FieldFilter, sourceID, syncID string, begin bool, obs []FileObservation) (*FileSyncResult, error) {
	if !reSyncID.MatchString(syncID) {
		return nil, &ValidationError{Field: "sync_id", Msg: "a sync needs an id of 8 to 64 letters, digits or hyphens"}
	}
	return v.upsertFiles(allow, sourceID, obs, false, syncID, begin)
}

var reSyncID = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// maxSyncKeys bounds the keys remembered for one running sync.
const maxSyncKeys = 1_000_000

// fileIndex is a source's files, decrypted.
type fileIndex struct {
	byKey map[string]*openedFile
	all   []*openedFile
}

// fileSync is a running sync of a source: the keys its batches named and the
// source's decrypted files, so each batch doesn't decrypt them all again.
// It is dropped when the vault locks (or is found to belong to an earlier
// session), when the source's rows are pruned or forgotten, and by a newer
// sync of the same source.
type fileSync struct {
	id      string
	session *Session
	keys    map[string]bool
	idx     *fileIndex
	last    time.Time // of the latest batch
}

// syncIdle is how long a sync may sit between batches before the vault
// forgets it (and the decrypted names it holds).
const syncIdle = 30 * time.Minute

// reapFileSyncs forgets syncs idle for syncIdle. filesMu is held.
func (v *Vault) reapFileSyncs(now time.Time) {
	for id, t := range v.fileSyncs {
		if now.Sub(t.last) > syncIdle {
			delete(v.fileSyncs, id)
		}
	}
}

// guardMassGone refuses to mark most of a source gone at once: a drive that
// is not mounted, a signed-out cloud folder or a partial listing looks just
// like that. With force the user has seen the number and said go ahead.
func guardMassGone(live, gone int, force bool) error {
	switch {
	case gone == 0:
		return nil
	case gone == live && live >= 5 && !force:
		return &ValidationError{Field: "sync", Msg: fmt.Sprintf("this would mark all %d files of the source gone: it is probably not mounted, or signed out. Nothing was marked gone; if the files really are gone, confirm it", live)}
	case live >= 20 && gone*2 > live && !force:
		return &ValidationError{Field: "sync", Msg: fmt.Sprintf("this would mark %d of %d files gone: the source may be only partly there. Nothing was marked gone; if they really are gone, confirm it", gone, live)}
	}
	return nil
}

func (v *Vault) currentSession() *Session {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.session
}

// dropFileSyncs forgets every running sync. filesMu is held.
func (v *Vault) dropFileSyncs() { v.fileSyncs = nil }

// dropFileSyncsIfIdle clears what the running syncs remember at lock time. It
// never waits: the locker holds v.mu, which a file operation holding filesMu
// may be waiting for. If it can't clear them now, the next operation does
// (a sync from an earlier session is dropped on use).
func (v *Vault) dropFileSyncsIfIdle() {
	if v.filesMu.TryLock() {
		v.fileSyncs = nil
		v.filesMu.Unlock()
	}
}

func (v *Vault) loadFileIndex(key []byte, sourceID string) (*fileIndex, error) {
	rows, err := v.db.ListFiles(sourceID)
	if err != nil {
		return nil, err
	}
	idx := &fileIndex{byKey: map[string]*openedFile{}, all: make([]*openedFile, 0, len(rows))}
	for _, row := range rows {
		of := &openedFile{row: row, index: -1}
		if err := openFileBlob(key, "file", row.ID, row.Blob, &of.meta); err != nil {
			return nil, err
		}
		for i, l := range of.meta.Locations {
			if l.Source == sourceID {
				idx.byKey[l.Key] = of
				of.index = i
			}
		}
		idx.all = append(idx.all, of)
	}
	return idx, nil
}

// runningSync returns the sync syncID of a source. A begin starts it (ending
// an older sync of the source); anything else needs it to be running in this
// session.
func (v *Vault) runningSync(sourceID, syncID string, begin bool) (*fileSync, error) {
	session := v.currentSession()
	if t := v.fileSyncs[sourceID]; t != nil && t.session != session {
		delete(v.fileSyncs, sourceID)
	}
	if begin {
		if v.fileSyncs == nil {
			v.fileSyncs = map[string]*fileSync{}
		}
		t := &fileSync{id: syncID, session: session, keys: map[string]bool{}, last: time.Now()}
		v.fileSyncs[sourceID] = t
		return t, nil
	}
	if t := v.fileSyncs[sourceID]; t != nil && t.id == syncID {
		t.last = time.Now()
		return t, nil
	}
	return nil, ErrSyncExpired
}

// fileVisibility returns whether a cached file is one allow may see (allow
// nil: all). It reads the links and scoped marks afresh, since the user can
// change them while a sync runs.
func (v *Vault) fileVisibility(allow FieldFilter) (func(*openedFile) bool, error) {
	if allow == nil {
		return func(*openedFile) bool { return true }, nil
	}
	links, err := v.db.ListFileLinks("")
	if err != nil {
		return nil, err
	}
	linksOf := map[string][]store.FileLink{}
	for _, l := range links {
		linksOf[l.FileID] = append(linksOf[l.FileID], l)
	}
	scoped, err := v.db.ScopedFileIDs()
	if err != nil {
		return nil, err
	}
	return func(of *openedFile) bool {
		row := of.row
		row.Scoped = scoped[row.ID]
		_, ok := visibleFileLinks(allow, row, linksOf[row.ID])
		return ok
	}, nil
}

// tombstone marks a file gone, in the database first.
func (v *Vault) tombstone(of *openedFile, now time.Time) error {
	row := of.row
	row.GoneAt, row.UpdatedAt = now, now
	if err := v.db.UpdateFile(row); err != nil {
		return err
	}
	of.row = row
	return nil
}

func (v *Vault) upsertFiles(allow FieldFilter, sourceID string, obs []FileObservation, full bool, syncID string, begin bool) (*FileSyncResult, error) {
	for i := range obs {
		if err := obs[i].Check(); err != nil {
			return nil, err
		}
	}

	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()

	res := &FileSyncResult{}
	err := v.withFileKey(func(key []byte) error {
		src, err := v.db.GetFileSource(sourceID)
		if err != nil {
			return err
		}
		if src == nil {
			return ErrFileNotFound
		}
		v.reapFileSyncs(time.Now())
		var track *fileSync
		var idx *fileIndex
		if syncID == "" {
			// A write outside a running sync would leave its cache stale.
			delete(v.fileSyncs, sourceID)
		}
		if syncID != "" {
			if track, err = v.runningSync(sourceID, syncID, begin); err != nil {
				return err
			}
			if track.idx == nil {
				if track.idx, err = v.loadFileIndex(key, sourceID); err != nil {
					return err
				}
			}
			idx = track.idx
		} else if idx, err = v.loadFileIndex(key, sourceID); err != nil {
			return err
		}
		rules, err := v.openFileRules(key, sourceID)
		if err != nil {
			return err
		}
		visible, err := v.fileVisibility(allow)
		if err != nil {
			return err
		}

		seen := map[*openedFile]bool{}
		now := time.Now()
		for _, o := range obs {
			of := idx.byKey[o.Key]
			if of != nil && !visible(of) {
				// Not the caller's to see: left as it is, and uncounted.
				if track != nil {
					track.keys[o.Key] = true
				}
				continue
			}
			if o.Keep {
				if track != nil {
					track.keys[o.Key] = true
				}
				if of != nil {
					seen[of] = true
				}
				continue
			}
			if o.Deleted {
				if of != nil && of.row.GoneAt.IsZero() {
					if err := v.tombstone(of, now); err != nil {
						return err
					}
					res.Gone++
				}
				continue
			}
			if track != nil {
				if len(track.keys) >= maxSyncKeys {
					return &ValidationError{Field: "files", Msg: "this sync is larger than a million files"}
				}
				track.keys[o.Key] = true
			}
			loc := FileLocation{Source: sourceID, Key: o.Key, Path: o.Path, WebURL: o.WebURL}
			if of == nil {
				meta := FileMeta{Name: o.Name, Locations: []FileLocation{loc}, MIME: o.MIME, Size: o.Size,
					Mtime: o.Mtime, ProviderHash: o.ProviderHash, SHA256: o.SHA256}
				id, err := newFileID()
				if err != nil {
					return err
				}
				blob, err := sealFileBlob(key, "file", id, meta)
				if err != nil {
					return err
				}
				row := store.File{ID: id, SourceID: sourceID, Tier: DefaultFileTier(o.Name, o.Path), Blob: blob, CreatedAt: now, UpdatedAt: now}
				if err := v.db.CreateFile(row); err != nil {
					return err
				}
				of = &openedFile{row: row, meta: meta, index: 0}
				idx.byKey[o.Key] = of
				idx.all = append(idx.all, of)
				res.Added++
				n, err := v.applyFileRules(key, rules, of, now)
				if err != nil {
					return err
				}
				res.Linked += n
				seen[of] = true
				continue
			}
			if seen[of] {
				continue // the same key twice in one batch: the first says it all
			}
			seen[of] = true
			meta := of.meta
			meta.Locations = append([]FileLocation(nil), of.meta.Locations...)
			moved := meta.Locations[of.index].Path != o.Path
			meta.Locations[of.index] = loc
			meta.Name, meta.MIME, meta.Size, meta.Mtime = o.Name, o.MIME, o.Size, o.Mtime
			meta.ProviderHash = o.ProviderHash
			// A hash the client didn't compute this time (a placeholder it
			// must not read) keeps the last one.
			if o.SHA256 != "" {
				meta.SHA256 = o.SHA256
			}
			tier := maxTier(of.row.Tier, DefaultFileTier(o.Name, o.Path))
			if reflect.DeepEqual(meta, of.meta) && tier == of.row.Tier && of.row.GoneAt.IsZero() {
				res.Unchanged++
				continue
			}
			blob, err := sealFileBlob(key, "file", of.row.ID, meta)
			if err != nil {
				return err
			}
			row := of.row
			row.Blob, row.Tier, row.GoneAt, row.UpdatedAt = blob, tier, time.Time{}, now
			if err := v.db.UpdateFile(row); err != nil {
				return err
			}
			of.row, of.meta = row, meta
			res.Updated++
			// Only a move into a ruled folder links the file: a link the user
			// removed must not come back each time the file is saved.
			if moved {
				n, err := v.applyFileRules(key, rules, of, now)
				if err != nil {
					return err
				}
				res.Linked += n
			}
		}
		if full {
			live := 0
			var gone []*openedFile
			for _, of := range idx.all {
				if !of.row.GoneAt.IsZero() || of.index < 0 || !visible(of) {
					continue
				}
				live++
				if !seen[of] {
					gone = append(gone, of)
				}
			}
			if err := guardMassGone(live, len(gone), false); err != nil {
				return err
			}
			for _, of := range gone {
				if err := v.tombstone(of, now); err != nil {
					return err
				}
				res.Gone++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	v.auditFile(FilesScopeID(""), "file_sync", fmt.Sprintf("%d added, %d updated, %d gone, %d linked", res.Added, res.Updated, res.Gone, res.Linked))
	if res.Added+res.Updated+res.Gone+res.Linked > 0 {
		v.markChanged()
	}
	return res, nil
}

// ErrSyncExpired is returned by FinishFileSync for a sync the vault no longer
// remembers (it locked, or another sync of the source began). Nothing is
// tombstoned then: run the sync again.
var ErrSyncExpired = &ValidationError{Field: "sync_id", Msg: "this sync is no longer remembered (the vault locked or another sync started): run it again; nothing was marked gone"}

// FinishFileSync ends the sync syncID of a source: files of the source that
// no batch named, and that allow may see, are tombstoned, never deleted. It
// refuses to mark everything, or most of a large source, gone unless force.
func (v *Vault) FinishFileSync(allow FieldFilter, sourceID, syncID string, force bool) (*FileSyncResult, error) {
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	track := v.fileSyncs[sourceID]
	if track == nil || track.id != syncID || track.session != v.currentSession() {
		return nil, ErrSyncExpired
	}
	res := &FileSyncResult{}
	err := v.withFileKey(func(key []byte) error {
		idx := track.idx
		if idx == nil { // no batch carried a file
			var err error
			if idx, err = v.loadFileIndex(key, sourceID); err != nil {
				return err
			}
		}
		visible, err := v.fileVisibility(allow)
		if err != nil {
			return err
		}
		live := 0
		var gone []*openedFile
		for _, of := range idx.all {
			if of.index < 0 || !of.row.GoneAt.IsZero() || !visible(of) {
				continue
			}
			live++
			if !track.keys[of.meta.Locations[of.index].Key] {
				gone = append(gone, of)
			}
		}
		if err := guardMassGone(live, len(gone), force); err != nil {
			return err
		}
		now := time.Now()
		for _, of := range gone {
			if err := v.tombstone(of, now); err != nil {
				return err
			}
			res.Gone++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	delete(v.fileSyncs, sourceID)
	v.auditFile(FilesScopeID(""), "file_sync_finish", fmt.Sprintf("%d gone", res.Gone))
	if res.Gone > 0 {
		v.markChanged()
	}
	return res, nil
}

// --- links ------------------------------------------------------------------

// FileLinkInput says whom a file is linked to.
type FileLinkInput struct {
	Holder  string `json:"for"`
	Kind    string `json:"kind"`
	Pratica string `json:"pratica,omitempty"`
	Role    string `json:"role,omitempty"`
	Note    string `json:"note,omitempty"`
}

func (in *FileLinkInput) clean() error {
	var err error
	if in.Kind == "" {
		switch {
		case in.Pratica != "":
			in.Kind = FileKindPratica
		default:
			in.Kind = FileKindPerson
		}
	}
	if in.Role, err = cleanFileText("role", in.Role, maxFileRole); err != nil {
		return err
	}
	in.Note, err = cleanFileText("note", in.Note, maxFileLinkText)
	return err
}

// LinkFile links a file to a person, the business, a client or a pratica.
// Linking again returns the existing link. The session (or a grant the user
// approved) links; a service token can only suggest.
func (v *Vault) LinkFile(fileID string, in FileLinkInput, origin string) (*FileLinkInfo, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	var out *FileLinkInfo
	err := v.withFileKey(func(key []byte) error {
		f, err := v.db.GetFile(fileID)
		if err != nil {
			return err
		}
		if f == nil {
			return ErrFileNotFound
		}
		if err := v.checkFileTarget(nil, in.Holder, in.Kind, in.Pratica); err != nil {
			return err
		}
		out, err = v.createFileLink(key, *f, in, origin, time.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	v.auditFile(FilesScopeID(in.Holder), "file_link", in.Kind)
	v.markChanged()
	return out, nil
}

// createFileLink inserts a link, or returns the same one if there is one.
func (v *Vault) createFileLink(key []byte, f store.File, in FileLinkInput, origin string, now time.Time) (*FileLinkInfo, error) {
	links, err := v.db.ListFileLinks(f.ID)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if l.Holder == in.Holder && l.Kind == in.Kind && l.PraticaSlug == in.Pratica {
			info, err := openFileLink(key, l)
			return info, err
		}
	}
	id, err := newFileID()
	if err != nil {
		return nil, err
	}
	blob, err := sealFileBlob(key, "filelink", id, fileLinkBlob{Role: in.Role, Note: in.Note})
	if err != nil {
		return nil, err
	}
	if in.Holder != "" && !f.Scoped {
		// From now on the file is no longer the owner's, even if this link goes.
		if err := v.db.SetFileScoped(f.ID); err != nil {
			return nil, err
		}
	}
	tier := maxTier("sensitive", f.Tier)
	l := store.FileLink{ID: id, FileID: f.ID, Holder: in.Holder, Kind: in.Kind, PraticaSlug: in.Pratica,
		Origin: origin, Tier: tier, Blob: blob, CreatedAt: now}
	if err := v.db.CreateFileLink(l); err != nil {
		return nil, err
	}
	return openFileLink(key, l)
}

func openFileLink(key []byte, l store.FileLink) (*FileLinkInfo, error) {
	var b fileLinkBlob
	if err := openFileBlob(key, "filelink", l.ID, l.Blob, &b); err != nil {
		return nil, err
	}
	return &FileLinkInfo{ID: l.ID, FileID: l.FileID, Holder: l.Holder, Kind: l.Kind, PraticaSlug: l.PraticaSlug,
		Origin: l.Origin, Tier: l.Tier, Role: b.Role, Note: b.Note, CreatedAt: l.CreatedAt}, nil
}

// UnlinkFile removes a link. The file stays in the index.
func (v *Vault) UnlinkFile(allow FieldFilter, linkID string) error {
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	if err := v.checkUnlocked(); err != nil {
		return err
	}
	l, err := v.db.GetFileLink(linkID)
	if err != nil {
		return err
	}
	if l == nil || !holderAllowed(allow, l.Holder, l.Tier) {
		return ErrFileNotFound
	}
	if _, err := v.db.DeleteFileLink(linkID); err != nil {
		return err
	}
	v.auditFile(FilesScopeID(l.Holder), "file_unlink", l.Kind)
	v.markChanged()
	return nil
}

// holderAllowed reports whether allow reaches the files of a holder (allow
// nil: all). The user's grant for one person must not touch another's.
func holderAllowed(allow FieldFilter, holder, tier string) bool {
	return allow == nil || allow(FilesScopeID(holder), maxTier("sensitive", tier))
}

// --- rules ------------------------------------------------------------------

// FileRuleInput links every file below Path of a source.
type FileRuleInput struct {
	Path    string `json:"path"`
	Holder  string `json:"for"`
	Kind    string `json:"kind"`
	Pratica string `json:"pratica,omitempty"`
}

// FileRuleInfo is a rule as listed.
type FileRuleInfo struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	Path     string `json:"path"`
	Holder   string `json:"for"`
	Kind     string `json:"kind"`
	Pratica  string `json:"pratica,omitempty"`
}

type openedRule struct {
	info FileRuleInfo
}

func (v *Vault) openFileRules(key []byte, sourceID string) ([]openedRule, error) {
	rows, err := v.db.ListFileRules(sourceID)
	if err != nil {
		return nil, err
	}
	var out []openedRule
	for _, r := range rows {
		var b fileRuleBlob
		if err := openFileBlob(key, "filerule", r.ID, r.Blob, &b); err != nil {
			return nil, err
		}
		out = append(out, openedRule{FileRuleInfo{ID: r.ID, SourceID: r.SourceID, Path: b.Path, Holder: r.Holder, Kind: r.Kind, Pratica: r.PraticaSlug}})
	}
	return out, nil
}

// pathUnder reports whether p is prefix or below it, by whole path parts.
func pathUnder(p, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	return prefix == "" || p == prefix || strings.HasPrefix(p, prefix+"/")
}

// applyFileRules links f for each rule whose folder holds it. It returns how
// many new links it made.
func (v *Vault) applyFileRules(key []byte, rules []openedRule, of *openedFile, now time.Time) (int, error) {
	made := 0
	for _, r := range rules {
		hit := false
		for _, l := range of.meta.Locations {
			if l.Source == r.info.SourceID && pathUnder(foldText(l.Path), foldText(r.info.Path)) {
				hit = true
			}
		}
		if !hit {
			continue
		}
		// A rule whose person was removed since is skipped, not an error: a
		// sync must not fail on it.
		in := FileLinkInput{Holder: r.info.Holder, Kind: r.info.Kind, Pratica: r.info.Pratica}
		if err := v.checkFileTarget(nil, in.Holder, in.Kind, in.Pratica); err != nil {
			var verr *ValidationError
			if errors.As(err, &verr) {
				continue
			}
			return made, err
		}
		before, err := v.db.ListFileLinks(of.row.ID)
		if err != nil {
			return made, err
		}
		if _, err := v.createFileLink(key, of.row, in, FileOriginRule, now); err != nil {
			return made, err
		}
		after, err := v.db.ListFileLinks(of.row.ID)
		if err != nil {
			return made, err
		}
		made += len(after) - len(before)
	}
	return made, nil
}

// AddFileRule links every file of a source below a folder, now and as they
// are indexed. It returns the rule and how many existing files it linked.
func (v *Vault) AddFileRule(sourceID string, in FileRuleInput) (*FileRuleInfo, int, error) {
	p, err := CleanFilePath(strings.TrimSpace(in.Path))
	if err != nil {
		return nil, 0, err
	}
	if p == "" {
		return nil, 0, &ValidationError{Field: "path", Msg: "a rule needs the folder it covers (\"/\" for the whole source)"}
	}
	li := FileLinkInput{Holder: in.Holder, Kind: in.Kind, Pratica: in.Pratica}
	if err := li.clean(); err != nil {
		return nil, 0, err
	}
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	var info *FileRuleInfo
	linked := 0
	err = v.withFileKey(func(key []byte) error {
		src, err := v.db.GetFileSource(sourceID)
		if err != nil {
			return err
		}
		if src == nil {
			return ErrFileNotFound
		}
		if err := v.checkFileTarget(nil, li.Holder, li.Kind, li.Pratica); err != nil {
			return err
		}
		id, err := newFileID()
		if err != nil {
			return err
		}
		blob, err := sealFileBlob(key, "filerule", id, fileRuleBlob{Path: p})
		if err != nil {
			return err
		}
		now := time.Now()
		if err := v.db.CreateFileRule(store.FileRule{ID: id, SourceID: sourceID, Holder: li.Holder, Kind: li.Kind,
			PraticaSlug: li.Pratica, Blob: blob, CreatedAt: now}); err != nil {
			return err
		}
		info = &FileRuleInfo{ID: id, SourceID: sourceID, Path: p, Holder: li.Holder, Kind: li.Kind, Pratica: li.Pratica}
		rows, err := v.db.ListFiles(sourceID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			of := &openedFile{row: row}
			if err := openFileBlob(key, "file", row.ID, row.Blob, &of.meta); err != nil {
				return err
			}
			n, err := v.applyFileRules(key, []openedRule{{*info}}, of, now)
			if err != nil {
				return err
			}
			linked += n
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	v.auditFile(FilesScopeID(in.Holder), "file_rule_add", fmt.Sprintf("%d linked", linked))
	v.markChanged()
	return info, linked, nil
}

// FileRules lists the rules of a source ("" for all).
func (v *Vault) FileRules(allow FieldFilter, sourceID string) ([]FileRuleInfo, error) {
	var out []FileRuleInfo
	err := v.withFileKey(func(key []byte) error {
		rules, err := v.openFileRules(key, sourceID)
		if err != nil {
			return err
		}
		for _, r := range rules {
			if holderAllowed(allow, r.info.Holder, "") {
				out = append(out, r.info)
			}
		}
		return nil
	})
	return out, err
}

// RemoveFileRule deletes a rule. The links it made stay: unlink them.
func (v *Vault) RemoveFileRule(allow FieldFilter, id string) error {
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	if err := v.checkUnlocked(); err != nil {
		return err
	}
	rules, err := v.db.ListFileRules("")
	if err != nil {
		return err
	}
	found := false
	for _, r := range rules {
		if r.ID == id && holderAllowed(allow, r.Holder, "") {
			found = true
		}
	}
	if !found {
		return ErrFileNotFound
	}
	if _, err := v.db.DeleteFileRule(id); err != nil {
		return err
	}
	v.auditFile(FilesScopeID(""), "file_rule_remove", "")
	v.markChanged()
	return nil
}

// --- suggestions ------------------------------------------------------------

// FileSuggestionInfo is a link an agent proposed.
type FileSuggestionInfo struct {
	ID         string    `json:"id"`
	FileID     string    `json:"file_id"`
	Holder     string    `json:"for"`
	Kind       string    `json:"kind"`
	Pratica    string    `json:"pratica,omitempty"`
	ProposedBy string    `json:"proposed_by"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// SuggestFileLink records a link for the user to accept. A caller with a
// filter (a service token's) can suggest only for files and holders it can see.
func (v *Vault) SuggestFileLink(allow FieldFilter, fileID string, in FileLinkInput, proposedBy, reason string) (*FileSuggestionInfo, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	reason, err := cleanFileText("reason", reason, maxFileLinkText)
	if err != nil {
		return nil, err
	}
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	var out *FileSuggestionInfo
	err = v.withFileKey(func(key []byte) error {
		f, err := v.db.GetFile(fileID)
		if err != nil {
			return err
		}
		if f == nil {
			return ErrFileNotFound
		}
		links, err := v.db.ListFileLinks(fileID)
		if err != nil {
			return err
		}
		if _, ok := visibleFileLinks(allow, *f, links); !ok {
			return ErrFileNotFound
		}
		if allow != nil && !allow(FilesScopeID(in.Holder), maxTier("sensitive", f.Tier)) {
			return &ValidationError{Field: "for", Msg: "your access doesn't cover " + FilesScopeID(in.Holder)}
		}
		if err := v.checkFileTarget(allow, in.Holder, in.Kind, in.Pratica); err != nil {
			return err
		}
		pending, err := v.db.ListFileSuggestions()
		if err != nil {
			return err
		}
		for _, p := range pending {
			if p.FileID == fileID && p.Holder == in.Holder && p.Kind == in.Kind && p.PraticaSlug == in.Pratica {
				var b fileSuggestionBlob
				if err := openFileBlob(key, "filesuggestion", p.ID, p.Blob, &b); err != nil {
					return err
				}
				out = &FileSuggestionInfo{ID: p.ID, FileID: p.FileID, Holder: p.Holder, Kind: p.Kind, Pratica: p.PraticaSlug,
					ProposedBy: p.ProposedBy, Reason: b.Reason, CreatedAt: p.CreatedAt}
				return nil // already waiting for the user
			}
		}
		if len(pending) >= maxPendingSuggestions {
			return &ValidationError{Field: "suggestions", Msg: "too many suggestions are waiting for the user: wait until they have decided on some"}
		}
		id, err := newFileID()
		if err != nil {
			return err
		}
		blob, err := sealFileBlob(key, "filesuggestion", id, fileSuggestionBlob{Reason: reason})
		if err != nil {
			return err
		}
		now := time.Now()
		if err := v.db.CreateFileSuggestion(store.FileSuggestion{ID: id, FileID: fileID, Holder: in.Holder, Kind: in.Kind,
			PraticaSlug: in.Pratica, ProposedBy: proposedBy, Blob: blob, CreatedAt: now}); err != nil {
			return err
		}
		out = &FileSuggestionInfo{ID: id, FileID: fileID, Holder: in.Holder, Kind: in.Kind, Pratica: in.Pratica,
			ProposedBy: proposedBy, Reason: reason, CreatedAt: now}
		return nil
	})
	if err != nil {
		return nil, err
	}
	v.auditFile(FilesScopeID(in.Holder), "file_suggest", in.Kind)
	v.markChanged()
	return out, nil
}

// FileSuggestions lists the pending suggestions.
func (v *Vault) FileSuggestions(allow FieldFilter) ([]FileSuggestionInfo, error) {
	var out []FileSuggestionInfo
	err := v.withFileKey(func(key []byte) error {
		rows, err := v.db.ListFileSuggestions()
		if err != nil {
			return err
		}
		for _, s := range rows {
			if !holderAllowed(allow, s.Holder, "") {
				continue
			}
			var b fileSuggestionBlob
			if err := openFileBlob(key, "filesuggestion", s.ID, s.Blob, &b); err != nil {
				return err
			}
			out = append(out, FileSuggestionInfo{ID: s.ID, FileID: s.FileID, Holder: s.Holder, Kind: s.Kind, Pratica: s.PraticaSlug,
				ProposedBy: s.ProposedBy, Reason: b.Reason, CreatedAt: s.CreatedAt})
		}
		return nil
	})
	return out, err
}

// ResolveFileSuggestion accepts (a normal link, origin suggestion) or rejects
// a suggestion. Either way it is gone from the list.
func (v *Vault) ResolveFileSuggestion(allow FieldFilter, id string, accept bool) (*FileLinkInfo, error) {
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	var link *FileLinkInfo
	err := v.withFileKey(func(key []byte) error {
		sugs, err := v.db.ListFileSuggestions()
		if err != nil {
			return err
		}
		var s *store.FileSuggestion
		for i := range sugs {
			if sugs[i].ID == id {
				s = &sugs[i]
			}
		}
		if s == nil || !holderAllowed(allow, s.Holder, "") {
			return ErrFileNotFound
		}
		if accept {
			f, err := v.db.GetFile(s.FileID)
			if err != nil {
				return err
			}
			if f == nil {
				return ErrFileNotFound
			}
			var b fileSuggestionBlob
			if err := openFileBlob(key, "filesuggestion", s.ID, s.Blob, &b); err != nil {
				return err
			}
			in := FileLinkInput{Holder: s.Holder, Kind: s.Kind, Pratica: s.PraticaSlug, Note: b.Reason}
			if err := v.checkFileTarget(nil, in.Holder, in.Kind, in.Pratica); err != nil {
				return err
			}
			if link, err = v.createFileLink(key, *f, in, FileOriginSuggestion, time.Now()); err != nil {
				return err
			}
		}
		_, err = v.db.DeleteFileSuggestion(id)
		return err
	})
	if err != nil {
		return nil, err
	}
	action := "file_suggestion_reject"
	if accept {
		action = "file_suggestion_accept"
	}
	v.auditFile(FilesScopeID(""), action, "")
	v.markChanged()
	return link, nil
}

// --- reading ----------------------------------------------------------------

// AnyHolder stands for "every person" in a scope check: only a scope naming
// all people (*:files.*, *) allows its files.index.
const AnyHolder = "zz-any"

// visibleFileLinks returns the links allow may see and whether the file is
// visible at all. A file that has had no person's link is the owner's; one
// with links is visible through any link whose holder the caller reaches; one
// whose person links are all gone (unlinked, or the person removed) is
// visible only to a caller reaching every person. allow nil (the session)
// sees everything.
func visibleFileLinks(allow FieldFilter, f store.File, links []store.FileLink) ([]store.FileLink, bool) {
	if allow == nil {
		return links, true
	}
	var seen []store.FileLink
	for _, l := range links {
		if allow(FilesScopeID(l.Holder), maxTier(f.Tier, l.Tier)) {
			seen = append(seen, l)
		}
	}
	if len(links) == 0 {
		if f.Scoped {
			return nil, allow(FilesScopeID(AnyHolder), f.Tier)
		}
		return nil, allow(FilesScopeID(""), f.Tier)
	}
	return seen, len(seen) > 0
}

// FileQuery filters SearchFiles. Every set field must match.
type FileQuery struct {
	Text        string  // every word must be in the name or a path (accents and case ignored)
	Holder      *string // linked to this holder ("" is the owner or business)
	Pratica     string  // linked to this pratica
	SourceID    string
	Provider    string
	Unlinked    bool // no link at all
	IncludeGone bool
	Limit       int
}

func (v *Vault) fileInfo(key []byte, f store.File, links []store.FileLink) (*FileInfo, error) {
	info := &FileInfo{ID: f.ID, SourceID: f.SourceID, Tier: f.Tier, Gone: !f.GoneAt.IsZero(), UpdatedAt: f.UpdatedAt, Links: []FileLinkInfo{}}
	if err := openFileBlob(key, "file", f.ID, f.Blob, &info.FileMeta); err != nil {
		return nil, err
	}
	for _, l := range links {
		li, err := openFileLink(key, l)
		if err != nil {
			return nil, err
		}
		info.Links = append(info.Links, *li)
	}
	return info, nil
}

// SearchFiles lists the files allow may see that match q, by name. It returns
// the matches (at most q.Limit) and how many there were. The audit row holds
// the count, never the text searched for.
func (v *Vault) SearchFiles(allow FieldFilter, q FileQuery) ([]FileInfo, int, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultFileLimit
	}
	limit = min(limit, maxFileLimit)
	words := strings.Fields(foldText(q.Text))

	var out []FileInfo
	total := 0
	err := v.withFileKey(func(key []byte) error {
		files, err := v.db.ListFiles(q.SourceID)
		if err != nil {
			return err
		}
		allLinks, err := v.db.ListFileLinks("")
		if err != nil {
			return err
		}
		linksOf := map[string][]store.FileLink{}
		for _, l := range allLinks {
			linksOf[l.FileID] = append(linksOf[l.FileID], l)
		}
		var providers map[string]string
		if q.Provider != "" {
			srcs, err := v.db.ListFileSources()
			if err != nil {
				return err
			}
			providers = map[string]string{}
			for _, s := range srcs {
				providers[s.ID] = s.Provider
			}
		}
		for _, f := range files {
			if (!f.GoneAt.IsZero() && !q.IncludeGone) || (q.Provider != "" && providers[f.SourceID] != q.Provider) {
				continue
			}
			links := linksOf[f.ID]
			if q.Unlinked && len(links) > 0 {
				continue
			}
			shown, ok := visibleFileLinks(allow, f, links)
			if !ok || !fileLinksMatch(shown, q) {
				continue
			}
			info, err := v.fileInfo(key, f, shown)
			if err != nil {
				return err
			}
			if !fileTextMatches(info, words) {
				continue
			}
			out = append(out, *info)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := foldText(out[i].Name), foldText(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	total = len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	v.auditFile(FilesScopeID(""), "file_search", fmt.Sprintf("%d hits", total))
	return out, total, nil
}

func fileLinksMatch(links []store.FileLink, q FileQuery) bool {
	if q.Holder == nil && q.Pratica == "" {
		return true
	}
	for _, l := range links {
		if q.Holder != nil && l.Holder != *q.Holder {
			continue
		}
		if q.Pratica != "" && !(l.Kind == FileKindPratica && l.PraticaSlug == q.Pratica) {
			continue
		}
		return true
	}
	return false
}

func fileTextMatches(f *FileInfo, words []string) bool {
	if len(words) == 0 {
		return true
	}
	var b strings.Builder
	b.WriteString(foldText(f.Name))
	for _, l := range f.Locations {
		b.WriteByte(' ')
		b.WriteString(foldText(l.Path))
	}
	for _, l := range f.Links {
		b.WriteByte(' ')
		b.WriteString(foldText(l.Role))
	}
	hay := b.String()
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// GetFileInfo returns one file with the links allow may see, or
// ErrFileNotFound for a file that doesn't exist or isn't the caller's to see.
func (v *Vault) GetFileInfo(allow FieldFilter, id string) (*FileInfo, error) {
	var out *FileInfo
	err := v.withFileKey(func(key []byte) error {
		f, err := v.db.GetFile(id)
		if err != nil {
			return err
		}
		if f == nil {
			return ErrFileNotFound
		}
		links, err := v.db.ListFileLinks(id)
		if err != nil {
			return err
		}
		shown, ok := visibleFileLinks(allow, *f, links)
		if !ok {
			return ErrFileNotFound
		}
		out, err = v.fileInfo(key, *f, shown)
		return err
	})
	if err != nil {
		return nil, err
	}
	v.auditFile(FilesScopeID(""), "file_read", "")
	return out, nil
}

// PruneFiles deletes the tombstoned files that have no link, and returns how
// many. A tombstoned file with a link stays: the link is the user's record.
func (v *Vault) PruneFiles() (int, error) {
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	if err := v.checkUnlocked(); err != nil {
		return 0, err
	}
	files, err := v.db.ListFiles("")
	if err != nil {
		return 0, err
	}
	links, err := v.db.ListFileLinks("")
	if err != nil {
		return 0, err
	}
	linked := map[string]bool{}
	for _, l := range links {
		linked[l.FileID] = true
	}
	var drop []string
	for _, f := range files {
		if !f.GoneAt.IsZero() && !linked[f.ID] {
			drop = append(drop, f.ID)
		}
	}
	if len(drop) == 0 {
		return 0, nil
	}
	if err := v.db.DeleteFiles(drop); err != nil {
		return 0, err
	}
	v.dropFileSyncs()
	v.auditFile(FilesScopeID(""), "files_prune", fmt.Sprintf("%d files", len(drop)))
	v.markChanged()
	return len(drop), nil
}
