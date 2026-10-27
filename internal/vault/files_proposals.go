package vault

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// A file an agent found through its own connector (Claude's Google Drive, say)
// can be proposed to the index. The agent gives what the connector told it;
// nothing enters the index until the user accepts, because everything in a
// proposal comes through a model and from whoever named the file. An accepted
// file lives in a source with no folder and no sign-in of its own
// (ViaConnector): nothing syncs it, so it is never marked gone, and it is only
// as fresh as the day it was accepted.

// ProposalProviders are the providers a proposal can name.
var ProposalProviders = []string{"gdrive", "onedrive", "sharepoint", "dropbox"}

// FileProposalInput is what an agent proposes: the file as its connector
// described it, and optionally whom to link it to.
type FileProposalInput struct {
	Provider     string `json:"provider"`
	Key          string `json:"key"` // the provider's own id for the file
	Name         string `json:"name"`
	Path         string `json:"path,omitempty"`
	WebURL       string `json:"web_url,omitempty"`
	MIME         string `json:"mime,omitempty"`
	Size         int64  `json:"size,omitempty"`
	Mtime        string `json:"mtime,omitempty"`
	ProviderHash string `json:"provider_hash,omitempty"`
	Holder       string `json:"for,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Pratica      string `json:"pratica,omitempty"`
	Role         string `json:"role,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// FileProposalInfo is a pending proposal as listed.
type FileProposalInfo struct {
	ID         string    `json:"id"`
	ProposedBy string    `json:"proposed_by"`
	CreatedAt  time.Time `json:"created_at"`
	FileProposalInput
}

func validProposalProvider(p string) bool {
	for _, q := range ProposalProviders {
		if p == q {
			return true
		}
	}
	return false
}

// proposalHostOK reports a web address on the provider's own sites: a proposal
// is untrusted, and the user may open its address from `pvault files open`, so
// it can't point anywhere else.
func proposalHostOK(provider, host string) bool {
	host = strings.ToLower(host)
	switch provider {
	case "gdrive":
		return host == "drive.google.com" || host == "docs.google.com"
	case "onedrive":
		return host == "onedrive.live.com" || strings.HasSuffix(host, ".sharepoint.com")
	case "sharepoint":
		return strings.HasSuffix(host, ".sharepoint.com") || strings.HasSuffix(host, ".sharepoint.us")
	case "dropbox":
		return host == "www.dropbox.com" || host == "dropbox.com"
	}
	return false
}

func providerTitle(provider string) string {
	return map[string]string{"gdrive": "Google Drive", "onedrive": "OneDrive", "sharepoint": "SharePoint", "dropbox": "Dropbox"}[provider]
}

// observation is the proposal as the index would take it, cleaned. A web
// address that is a sharing link is dropped; one on another site is an error.
func (in *FileProposalInput) observation() (FileObservation, error) {
	o := FileObservation{Key: in.Key, Name: in.Name, Path: in.Path, WebURL: in.WebURL, MIME: in.MIME,
		Size: in.Size, Mtime: in.Mtime, ProviderHash: in.ProviderHash}
	if o.WebURL != "" {
		u, err := url.Parse(o.WebURL)
		if err != nil || u.Scheme != "https" || !proposalHostOK(in.Provider, u.Hostname()) {
			return o, &ValidationError{Field: "web_url", Msg: "must be a page of " + providerTitle(in.Provider) + " itself"}
		}
		if CheckFileURL(o.WebURL) != nil {
			o.WebURL = "" // a sharing link: the file is kept, the link is not
		}
	}
	if o.Path == "" {
		o.Path = "/" + o.Name
	}
	if err := o.Check(); err != nil {
		return o, err
	}
	return o, nil
}

func (in *FileProposalInput) hasLink() bool {
	return in.Holder != "" || in.Pratica != "" || in.Kind != ""
}

func (in *FileProposalInput) link() FileLinkInput {
	return FileLinkInput{Holder: in.Holder, Kind: in.Kind, Pratica: in.Pratica, Role: in.Role}
}

// ProposeFile queues a file for the user to accept. A caller with a filter (a
// service token's) may only propose files for the holder it can reach, and
// only pratiche it can see. The same file proposed again is the same proposal.
func (v *Vault) ProposeFile(allow FieldFilter, in FileProposalInput, proposedBy string) (*FileProposalInfo, error) {
	if !validProposalProvider(in.Provider) {
		return nil, &ValidationError{Field: "provider", Msg: "use one of " + strings.Join(ProposalProviders, ", ")}
	}
	o, err := in.observation()
	if err != nil {
		return nil, err
	}
	in.Key, in.Name, in.Path, in.WebURL, in.MIME, in.Size, in.Mtime, in.ProviderHash = o.Key, o.Name, o.Path, o.WebURL, o.MIME, o.Size, o.Mtime, o.ProviderHash
	if in.Reason, err = cleanFileText("reason", in.Reason, maxFileLinkText); err != nil {
		return nil, err
	}
	if in.hasLink() {
		li := in.link()
		if err := li.clean(); err != nil {
			return nil, err
		}
		in.Kind, in.Role = li.Kind, li.Role
	}

	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	var out *FileProposalInfo
	err = v.withFileKey(func(key []byte) error {
		if allow != nil && !allow(FilesScopeID(in.Holder), "sensitive") {
			return &ValidationError{Field: "for", Msg: "your access doesn't cover " + FilesScopeID(in.Holder)}
		}
		if in.hasLink() {
			if err := v.checkFileTarget(allow, in.Holder, in.Kind, in.Pratica); err != nil {
				return err
			}
		}
		pending, err := v.db.ListFileProposals()
		if err != nil {
			return err
		}
		for _, p := range pending {
			var existing FileProposalInput
			if err := openFileBlob(key, "fileproposal", p.ID, p.Blob, &existing); err != nil {
				return err
			}
			if existing.Provider == in.Provider && existing.Key == in.Key {
				out = &FileProposalInfo{ID: p.ID, ProposedBy: p.ProposedBy, CreatedAt: p.CreatedAt, FileProposalInput: existing}
				return nil // already waiting for the user
			}
		}
		if len(pending) >= maxPendingSuggestions {
			return &ValidationError{Field: "proposals", Msg: "too many proposals are waiting for the user: wait until they have decided on some"}
		}
		id, err := newFileID()
		if err != nil {
			return err
		}
		blob, err := sealFileBlob(key, "fileproposal", id, in)
		if err != nil {
			return err
		}
		now := time.Now()
		if err := v.db.CreateFileProposal(store.FileProposal{ID: id, Provider: in.Provider, Holder: in.Holder,
			ProposedBy: proposedBy, Blob: blob, CreatedAt: now}); err != nil {
			return err
		}
		out = &FileProposalInfo{ID: id, ProposedBy: proposedBy, CreatedAt: now, FileProposalInput: in}
		return nil
	})
	if err != nil {
		return nil, err
	}
	v.auditFile(FilesScopeID(in.Holder), "file_propose", in.Provider)
	v.markChanged()
	return out, nil
}

// FileProposals lists the pending proposals allow may see.
func (v *Vault) FileProposals(allow FieldFilter) ([]FileProposalInfo, error) {
	var out []FileProposalInfo
	err := v.withFileKey(func(key []byte) error {
		rows, err := v.db.ListFileProposals()
		if err != nil {
			return err
		}
		for _, p := range rows {
			if !holderAllowed(allow, p.Holder, "") {
				continue
			}
			var in FileProposalInput
			if err := openFileBlob(key, "fileproposal", p.ID, p.Blob, &in); err != nil {
				return err
			}
			out = append(out, FileProposalInfo{ID: p.ID, ProposedBy: p.ProposedBy, CreatedAt: p.CreatedAt, FileProposalInput: in})
		}
		return nil
	})
	return out, err
}

// connectorSource returns the source holding a provider's accepted proposals,
// making it the first time.
func (v *Vault) connectorSource(provider string) (string, error) {
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	var id string
	err := v.withFileKey(func(key []byte) error {
		srcs, err := v.db.ListFileSources()
		if err != nil {
			return err
		}
		for _, s := range srcs {
			if s.Provider != provider {
				continue
			}
			info, err := openFileSource(key, s)
			if err != nil {
				return err
			}
			if info.Via == ViaConnector {
				id = s.ID
				return nil
			}
		}
		cfg := FileSourceConfig{Label: providerTitle(provider) + " (found through Claude)", Via: ViaConnector}
		if err := CheckFileSourceConfig(provider, &cfg); err != nil {
			return err
		}
		src, err := v.addFileSource(provider, cfg)
		if err != nil {
			return err
		}
		id = src.ID
		return nil
	})
	return id, err
}

// fileIDByKey finds a file of a source by its key at the source.
func (v *Vault) fileIDByKey(sourceID, fileKey string) (string, error) {
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	var id string
	err := v.withFileKey(func(key []byte) error {
		idx, err := v.loadFileIndex(key, sourceID)
		if err != nil {
			return err
		}
		if of := idx.byKey[fileKey]; of != nil {
			id = of.row.ID
		}
		return nil
	})
	return id, err
}

// ResolveFileProposal accepts or rejects a proposal. Accepting puts the file
// in the index (and links it, if the proposal asked for that and the link is
// still valid) and returns it; rejecting forgets the proposal. The user
// decides: allow limits it to the holders a grant they approved reaches.
func (v *Vault) ResolveFileProposal(allow FieldFilter, id string, accept bool) (*FileInfo, error) {
	var prop *FileProposalInfo
	var holder string
	err := v.withFileKey(func(key []byte) error {
		rows, err := v.db.ListFileProposals()
		if err != nil {
			return err
		}
		for _, p := range rows {
			if p.ID != id || !holderAllowed(allow, p.Holder, "") {
				continue
			}
			var in FileProposalInput
			if err := openFileBlob(key, "fileproposal", p.ID, p.Blob, &in); err != nil {
				return err
			}
			prop, holder = &FileProposalInfo{ID: p.ID, ProposedBy: p.ProposedBy, CreatedAt: p.CreatedAt, FileProposalInput: in}, p.Holder
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if prop == nil {
		return nil, ErrFileNotFound
	}

	var info *FileInfo
	if accept {
		o, err := prop.observation()
		if err != nil {
			return nil, err
		}
		srcID, err := v.connectorSource(prop.Provider)
		if err != nil {
			return nil, err
		}
		if _, err := v.UpsertFiles(srcID, []FileObservation{o}, false); err != nil {
			return nil, err
		}
		fileID, err := v.fileIDByKey(srcID, o.Key)
		if err != nil || fileID == "" {
			if err == nil {
				err = fmt.Errorf("the file was not added")
			}
			return nil, err
		}
		if prop.hasLink() {
			if _, err := v.LinkFile(fileID, prop.link(), FileOriginSuggestion); err != nil {
				return nil, fmt.Errorf("the file is in the index, but linking it failed: %w", err)
			}
		}
		if info, err = v.GetFileInfo(nil, fileID); err != nil {
			return nil, err
		}
	}

	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	if _, err := v.db.DeleteFileProposal(id); err != nil {
		return nil, err
	}
	action := "file_proposal_reject"
	if accept {
		action = "file_proposal_accept"
	}
	v.auditFile(FilesScopeID(holder), action, prop.Provider)
	v.markChanged()
	return info, nil
}
