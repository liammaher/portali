package filesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// Drive reads Google Drive with the metadata-only scope: names, places, sizes,
// dates and md5, never content. It lists every non-trashed file the user can
// see, folders included (they are the paths), and builds each path from the
// first parent.
type Drive struct {
	api  *apiClient
	base string
}

// segment makes a Drive name one path segment: Drive allows "/" in a name.
func segment(name string) string { return cleanName(name) }

const (
	driveFolder   = "application/vnd.google-apps.folder"
	driveShortcut = "application/vnd.google-apps.shortcut"

	// sharedWithMe is where files and folders the user was given, and whose
	// parents they can't see, are placed.
	sharedWithMe = "/Shared with me"

	maxDrivePages = 100000
)

type driveFile struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MimeType     string   `json:"mimeType"`
	Size         string   `json:"size"` // a string in the API, absent for Google's own formats
	ModifiedTime string   `json:"modifiedTime"`
	MD5          string   `json:"md5Checksum"`
	Parents      []string `json:"parents"`
	WebViewLink  string   `json:"webViewLink"`
}

func (d *Drive) Scan(ctx context.Context) *Scan {
	s := &Scan{}
	rootID, err := d.rootID(ctx)
	if err != nil {
		s.fail(err)
		return s
	}
	files := map[string]driveFile{}
	var order []string
	pageToken := ""
	for pages := 0; ; pages++ {
		if pages >= maxDrivePages {
			s.fail(errors.New("Google's list of files has no end"))
			return s
		}
		q := url.Values{
			"q":        {"trashed = false"},
			"pageSize": {"1000"},
			"fields":   {"nextPageToken,files(id,name,mimeType,size,modifiedTime,md5Checksum,parents,webViewLink)"},
			"spaces":   {"drive"},
		}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		body, err := d.api.get(ctx, d.base+"/files?"+q.Encode())
		if err != nil {
			s.fail(err)
			return s
		}
		var page struct {
			Files         []driveFile `json:"files"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			s.fail(errors.New("Google's list of files was unreadable"))
			return s
		}
		for _, f := range page.Files {
			if _, seen := files[f.ID]; !seen {
				order = append(order, f.ID)
			}
			files[f.ID] = f
		}
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	// A folder's path is its first parent's plus its name. The user's root
	// folder has none; one whose parent isn't visible (shared with the user)
	// starts under "Shared with me".
	var pathOf func(id string, depth int) (string, bool)
	pathOf = func(id string, depth int) (string, bool) {
		if id == rootID {
			return "", true
		}
		f, ok := files[id]
		if !ok {
			return sharedWithMe, true
		}
		if depth > 256 {
			return "", false
		}
		if len(f.Parents) == 0 {
			return sharedWithMe + "/" + segment(f.Name), true
		}
		parent, ok := pathOf(f.Parents[0], depth+1)
		if !ok {
			return "", false
		}
		return parent + "/" + segment(f.Name), true
	}
	for _, id := range order {
		f := files[id]
		if f.MimeType == driveFolder || f.MimeType == driveShortcut || f.Name == "" {
			continue
		}
		parent := sharedWithMe
		if len(f.Parents) > 0 {
			var ok bool
			if parent, ok = pathOf(f.Parents[0], 0); !ok {
				s.fail(fmt.Errorf("%q sits in a folder loop: the list of files can't be trusted", f.Name))
				continue
			}
		}
		o := vault.FileObservation{
			Key: f.ID, Path: parent + "/" + segment(f.Name), WebURL: f.WebViewLink, Name: segment(f.Name),
			MIME: f.MimeType, Mtime: f.ModifiedTime,
		}
		if n, err := strconv.ParseInt(f.Size, 10, 64); err == nil {
			o.Size = n
		}
		if f.MD5 != "" {
			o.ProviderHash = "md5:" + f.MD5
		}
		s.add(o)
	}
	return s
}

// rootID is the ID of the user's My Drive folder, which files name as their
// parent without the listing containing it.
func (d *Drive) rootID(ctx context.Context) (string, error) {
	body, err := d.api.get(ctx, d.base+"/files/root?fields=id")
	if err != nil {
		return "", err
	}
	var r struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &r) != nil || r.ID == "" {
		return "", errors.New("Google didn't say which folder is My Drive")
	}
	return r.ID, nil
}

func (d *Drive) Who(ctx context.Context) string {
	body, err := d.api.get(ctx, d.base+"/about?fields=user(emailAddress)")
	if err != nil {
		return ""
	}
	var a struct {
		User struct {
			Email string `json:"emailAddress"`
		} `json:"user"`
	}
	if json.Unmarshal(body, &a) != nil {
		return ""
	}
	return a.User.Email
}
