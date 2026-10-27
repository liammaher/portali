// Package filesync reads the places files live and reports them to the vault
// as observations. It only looks: nothing here writes, moves or deletes a
// file, and a file's bytes are never read (an online-only placeholder would
// be downloaded by opening it).
package filesync

import (
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// MaxFiles bounds one walk, so a root pointed at a whole disk by mistake
// stops instead of filling the index.
const MaxFiles = 500_000

// Scan is what a walk of a local root found.
type Scan struct {
	Files []vault.FileObservation
	// Placeholders counts files that are online-only: indexed by name, size
	// and date, never opened.
	Placeholders int
	// Skipped counts what the index leaves out: hidden files, links, lock
	// files and names the vault would refuse.
	Skipped int
	// Errors are folders or files that couldn't be read. A scan with errors
	// is incomplete, so the caller must not mark anything gone from it.
	Errors []error
	// Truncated is set when MaxFiles stopped the walk.
	Truncated bool
}

// Complete reports a scan that saw everything under the root.
func (s *Scan) Complete() bool { return len(s.Errors) == 0 && !s.Truncated }

// ResolveRoot returns the real path of a folder to read.
func ResolveRoot(root string) (string, error) {
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		root = abs
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s is not a folder", root)
	}
	return real, nil
}

// skipName is the files an index shouldn't hold: hidden ones (also the
// placeholders some sync apps keep under a dot), Office lock files and the
// system's folder droppings.
func skipName(name string) bool {
	switch {
	case strings.HasPrefix(name, "."), strings.HasPrefix(name, "~$"):
		return true
	}
	switch strings.ToLower(name) {
	case "thumbs.db", "desktop.ini", "icon\r":
		return true
	}
	return false
}

// Walk lists the regular files below root (already resolved). It doesn't
// follow links, skips hidden entries, and reads no file content.
func Walk(root string) *Scan {
	s := &Scan{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			s.Errors = append(s.Errors, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == root {
			return nil
		}
		name := d.Name()
		if skipName(name) {
			s.Skipped++
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			s.Skipped++ // a link can lead out of the root
			return nil
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			s.Skipped++
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			s.Errors = append(s.Errors, err)
			return nil
		}
		rel = "/" + filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			s.Errors = append(s.Errors, err)
			return nil
		}
		o := vault.FileObservation{
			Key:   rel,
			Path:  rel,
			Name:  name,
			MIME:  mime.TypeByExtension(strings.ToLower(path.Ext(name))),
			Size:  info.Size(),
			Mtime: info.ModTime().UTC().Format(time.RFC3339),
		}
		// The vault's own check, so a name it would refuse is left out here
		// instead of failing the batch it is in.
		if err := o.Check(); err != nil {
			s.Skipped++
			return nil
		}
		if len(s.Files) >= MaxFiles {
			s.Truncated = true
			return fs.SkipAll
		}
		if IsPlaceholder(info) {
			s.Placeholders++
		}
		s.Files = append(s.Files, o)
		return nil
	})
	if err != nil {
		s.Errors = append(s.Errors, err)
	}
	return s
}

// DetectProvider names the cloud service whose sync folder root is, and the
// account part of its name, from where the desktop apps put it: macOS keeps
// them under ~/Library/CloudStorage as OneDrive-<account>,
// GoogleDrive-<email> and Dropbox; older apps use ~/Dropbox, ~/OneDrive and
// ~/Google Drive. ok is false for any other folder (a folder that merely
// starts with such a name is not a sync folder).
func DetectProvider(root string) (provider, account string, ok bool) {
	parts := strings.Split(filepath.ToSlash(root), "/")
	for i, p := range parts {
		if p == "CloudStorage" && i+1 < len(parts) {
			return providerOfCloudStorage(parts[i+1])
		}
	}
	switch last := parts[len(parts)-1]; strings.ToLower(last) {
	case "dropbox":
		return "dropbox", last, true
	case "onedrive":
		return "onedrive", last, true
	case "google drive":
		return "gdrive", last, true
	default:
		if strings.HasPrefix(strings.ToLower(last), "onedrive - ") {
			return "onedrive", last, true
		}
	}
	return "", "", false
}

func providerOfCloudStorage(name string) (provider, account string, ok bool) {
	lower := strings.ToLower(name)
	switch {
	case lower == "onedrive" || strings.HasPrefix(lower, "onedrive-"):
		return "onedrive", name, true
	case strings.HasPrefix(lower, "sharepoint-"):
		return "sharepoint", name, true
	case strings.HasPrefix(lower, "googledrive-"):
		return "gdrive", name, true
	case lower == "dropbox" || strings.HasPrefix(lower, "dropbox-"):
		return "dropbox", name, true
	}
	return "", "", false
}
