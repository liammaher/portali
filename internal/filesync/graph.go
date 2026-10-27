package filesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// Graph reads a OneDrive or a SharePoint document library through Microsoft
// Graph. It enumerates the whole drive with the delta function (the only
// enumeration that doesn't miss files changed meanwhile) and builds each
// file's path from its parents, because delta never says a path.
type Graph struct {
	api     *apiClient
	base    string
	driveID string // empty: the signed-in user's OneDrive
}

type graphItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModifiedDateTime"`
	WebURL       string `json:"webUrl"`
	File         *struct {
		MimeType string `json:"mimeType"`
		Hashes   struct {
			QuickXor string `json:"quickXorHash"`
			Sha1     string `json:"sha1Hash"`
		} `json:"hashes"`
	} `json:"file"`
	Folder          *struct{} `json:"folder"`
	Deleted         *struct{} `json:"deleted"`
	Package         *struct{} `json:"package"` // a OneNote notebook: a folder of the app's, not a file
	Root            *struct{} `json:"root"`
	ParentReference struct {
		ID string `json:"id"`
	} `json:"parentReference"`
}

type graphPage struct {
	Value     []graphItem `json:"value"`
	NextLink  string      `json:"@odata.nextLink"`
	DeltaLink string      `json:"@odata.deltaLink"`
}

func (g *Graph) drive() string {
	if g.driveID == "" {
		return "/me/drive"
	}
	return "/drives/" + url.PathEscape(g.driveID)
}

const graphSelect = "id,name,size,lastModifiedDateTime,webUrl,file,folder,deleted,package,root,parentReference"

// maxGraphPages stops a feed that never ends.
const maxGraphPages = 200000

func (g *Graph) Scan(ctx context.Context) *Scan {
	s := &Scan{}
	items := map[string]graphItem{} // by ID: the last time an item appears is its state
	var order []string
	next := g.base + g.drive() + "/root/delta?$top=200&$select=" + graphSelect
	for pages := 0; next != ""; pages++ {
		if pages >= maxGraphPages {
			s.fail(errors.New("Microsoft's list of files has no end"))
			return s
		}
		if !g.api.sameHost(next) {
			s.fail(fmt.Errorf("Microsoft sent a next page on another address (%s): not following it", hostOf(next)))
			return s
		}
		body, err := g.api.get(ctx, next)
		if err != nil {
			s.fail(err)
			return s
		}
		var page graphPage
		if err := json.Unmarshal(body, &page); err != nil {
			s.fail(errors.New("Microsoft's list of files was unreadable"))
			return s
		}
		for _, it := range page.Value {
			if it.ID == "" {
				continue
			}
			if _, seen := items[it.ID]; !seen {
				order = append(order, it.ID)
			}
			items[it.ID] = it
		}
		if page.NextLink == "" && page.DeltaLink == "" {
			// A finished listing says so; anything else may be cut short.
			s.fail(errors.New("Microsoft's list of files ended without saying it was complete"))
			return s
		}
		next = page.NextLink
	}

	// Paths: a folder's is its parent's plus its name. The root has none. A
	// parent the feed never named (once the root is known) is not guessed at:
	// its files go under a folder that says so.
	rootID := ""
	for id, it := range items {
		if it.Root != nil {
			rootID = id
		}
	}
	var pathOf func(id string, depth int) (string, bool)
	pathOf = func(id string, depth int) (string, bool) {
		it, ok := items[id]
		if !ok {
			if rootID == "" || id == rootID {
				return "", true // the root, which delta names only as a parent
			}
			return "/(unknown folder)", true
		}
		if it.Root != nil {
			return "", true
		}
		if depth > 256 {
			return "", false
		}
		parent, ok := pathOf(it.ParentReference.ID, depth+1)
		if !ok {
			return "", false
		}
		return parent + "/" + cleanName(it.Name), true
	}
	for _, id := range order {
		it := items[id]
		if it.Deleted != nil || it.File == nil || it.Folder != nil || it.Package != nil || it.Name == "" {
			continue
		}
		parent, ok := pathOf(it.ParentReference.ID, 0)
		if !ok {
			s.fail(fmt.Errorf("%q sits in a folder loop: the list of files can't be trusted", it.Name))
			continue
		}
		o := vault.FileObservation{
			Key: it.ID, Path: parent + "/" + it.Name, WebURL: it.WebURL, Name: it.Name,
			MIME: it.File.MimeType, Size: it.Size, Mtime: it.LastModified,
		}
		switch {
		case it.File.Hashes.QuickXor != "":
			o.ProviderHash = "quickxor:" + it.File.Hashes.QuickXor
		case it.File.Hashes.Sha1 != "":
			o.ProviderHash = "sha1:" + it.File.Hashes.Sha1
		}
		s.add(o)
	}
	return s
}

func (g *Graph) Who(ctx context.Context) string {
	body, err := g.api.get(ctx, g.base+"/me?$select=userPrincipalName,mail")
	if err != nil {
		return ""
	}
	var me struct {
		Mail string `json:"mail"`
		UPN  string `json:"userPrincipalName"`
	}
	if json.Unmarshal(body, &me) != nil {
		return ""
	}
	if me.Mail != "" {
		return me.Mail
	}
	return me.UPN
}

// UserDrive returns the ID of the signed-in user's OneDrive.
func (g *Graph) UserDrive(ctx context.Context) (id string, err error) {
	body, err := g.api.get(ctx, g.base+"/me/drive?$select=id,driveType")
	if err != nil {
		return "", err
	}
	var d struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &d) != nil || d.ID == "" {
		return "", errors.New("Microsoft didn't say which OneDrive is this user's")
	}
	return d.ID, nil
}

// Library finds a document library of a SharePoint site by name ("" takes the
// site's first document library, the default one) and returns its drive ID and
// name. siteURL is the site's address in a browser.
func (g *Graph) Library(ctx context.Context, siteURL, library string) (driveID, name string, err error) {
	u, err := url.Parse(strings.TrimSpace(siteURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", "", errors.New("the site must be its https address, e.g. https://acme.sharepoint.com/sites/Studio")
	}
	sitePath := strings.TrimRight(u.EscapedPath(), "/")
	ref := "/sites/" + u.Host
	if sitePath != "" {
		ref += ":" + sitePath
	}
	body, err := g.api.get(ctx, g.base+ref+"?$select=id")
	if err != nil {
		return "", "", fmt.Errorf("finding the site: %w", err)
	}
	var site struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &site) != nil || site.ID == "" {
		return "", "", errors.New("Microsoft didn't find that site")
	}
	body, err = g.api.get(ctx, g.base+"/sites/"+strings.ReplaceAll(url.PathEscape(site.ID), "%2C", ",")+"/drives?$select=id,name,driveType")
	if err != nil {
		return "", "", fmt.Errorf("listing the site's libraries: %w", err)
	}
	var drives struct {
		Value []struct {
			ID, Name, DriveType string
		} `json:"value"`
	}
	if json.Unmarshal(body, &drives) != nil {
		return "", "", errors.New("Microsoft's list of libraries was unreadable")
	}
	var names []string
	for _, d := range drives.Value {
		if d.DriveType != "" && d.DriveType != "documentLibrary" {
			continue
		}
		names = append(names, d.Name)
		if library == "" || strings.EqualFold(d.Name, library) {
			return d.ID, d.Name, nil
		}
	}
	if len(names) == 0 {
		return "", "", errors.New("that site has no document library you can read")
	}
	return "", "", fmt.Errorf("no library %q on that site: it has %s", library, strings.Join(names, ", "))
}
