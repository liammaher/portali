package filesync

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// Dropbox reads a Dropbox account with the metadata scope: a recursive listing
// of the whole account, paged by cursor. Dropbox gives each entry's full path,
// so no tree is needed.
type Dropbox struct {
	api  *apiClient
	base string
}

type dropboxEntry struct {
	Tag            string `json:".tag"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	PathDisplay    string `json:"path_display"`
	Size           int64  `json:"size"`
	ServerModified string `json:"server_modified"`
	ContentHash    string `json:"content_hash"`
}

type dropboxPage struct {
	Entries []dropboxEntry `json:"entries"`
	Cursor  string         `json:"cursor"`
	HasMore bool           `json:"has_more"`
}

const maxDropboxPages = 100000

func (d *Dropbox) Scan(ctx context.Context) *Scan {
	s := &Scan{}
	body, err := d.api.postJSON(ctx, d.base+"/files/list_folder", map[string]any{
		"path": "", "recursive": true, "include_deleted": false,
		"include_non_downloadable_files": true, "limit": 2000,
	})
	for pages := 0; ; pages++ {
		if err != nil {
			s.fail(err)
			return s
		}
		if pages >= maxDropboxPages {
			s.fail(errors.New("Dropbox's list of files has no end"))
			return s
		}
		var page dropboxPage
		if err := json.Unmarshal(body, &page); err != nil {
			s.fail(errors.New("Dropbox's list of files was unreadable"))
			return s
		}
		for _, e := range page.Entries {
			if e.Tag != "file" || e.ID == "" || e.Name == "" {
				continue
			}
			o := vault.FileObservation{
				Key: e.ID, Path: e.PathDisplay, Name: e.Name, Size: e.Size, Mtime: e.ServerModified,
				WebURL: dropboxWebURL(e.PathDisplay),
			}
			if e.ContentHash != "" {
				o.ProviderHash = "dropbox:" + e.ContentHash
			}
			s.add(o)
		}
		if !page.HasMore {
			return s
		}
		body, err = d.api.postJSON(ctx, d.base+"/files/list_folder/continue", map[string]string{"cursor": page.Cursor})
	}
}

// dropboxWebURL is where the user's own Dropbox shows a path: not a link to
// hand to anyone (it asks the viewer to sign in), so the vault keeps it.
func dropboxWebURL(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "https://www.dropbox.com/home/" + strings.Join(parts, "/")
}

func (d *Dropbox) Who(ctx context.Context) string {
	body, err := d.api.postJSON(ctx, d.base+"/users/get_current_account", nil)
	if err != nil {
		return ""
	}
	var a struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(body, &a) != nil {
		return ""
	}
	return a.Email
}
