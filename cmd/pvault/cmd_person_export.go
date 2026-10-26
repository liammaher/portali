package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// exportedFile is a document's entry in an export's fields.json: its
// metadata and where its file is, relative to the export folder.
type exportedFile struct {
	vault.DocumentInfo
	File string `json:"file"`
}

// cmdPersonExport writes one person's fields and documents to a new folder,
// for their right to data portability (GDPR art. 20).
func cmdPersonExport() {
	const usage = "usage: pvault person export <slug> [--to <folder>] [--include-personal]"
	if len(os.Args) < 4 {
		fatal(usage)
	}
	slug := os.Args[3]
	checkSlug(slug)
	var to string
	includePersonal := false
	var rest []string
	for _, a := range os.Args[4:] {
		if a == "--include-personal" && !includePersonal {
			includePersonal = true
			continue
		}
		rest = append(rest, a)
	}
	if err := parseFlags(rest, map[string]*string{"--to": &to}); err != nil {
		fatal("%v\n%s", err, usage)
	}
	if to == "" {
		to = "."
	}
	dir := filepath.Join(to, slug+"-export-"+time.Now().Format("2006-01-02"))
	// Checked before the prompt, and again by Mkdir, which never reuses one.
	if _, err := os.Lstat(dir); err == nil {
		fatal("%s already exists: move it, or pass --to <folder>", dir)
	}

	needGrant("", "") // only your own session may export a person
	path := "/vault/members/" + slug + "/export"
	if includePersonal {
		path += "?include_personal=1"
	}
	resp, err := apiRequest("GET", path, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var out vault.PersonExport
	if err := apiResult(resp, &out); err != nil {
		fatal("%v", err)
	}
	if err := writePersonExport(dir, &out); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Exported %s to %s: %d fields, %d documents.\n", slug, dir, len(out.Fields), len(out.Documents))
	for _, d := range out.Missing {
		fmt.Printf("Missing: %s (%s), its encrypted file is gone; restore it from a backup to export it.\n", d.Name, d.Category)
	}
	if out.Omitted > 0 {
		fmt.Printf("%d personal item(s) left out; add --include-personal to include them.\n", out.Omitted)
	}
	fmt.Println("Fields of the pratiche that name them are included; files attached to pratiche are not (they aren't kept per person): hand those over from 'pvault doc list pratiche'.")
	fmt.Println("This copy is NOT encrypted: hand it over, then delete it. Don't keep it in a synced folder.")
}

// writePersonExport creates dir (0700, never an existing one) with
// fields.json and the documents under documents/<category>/, all 0600. On
// failure it removes what it wrote.
func writePersonExport(dir string, out *vault.PersonExport) (err error) {
	if err := os.Mkdir(dir, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists: move it, or pass --to <folder>", dir)
		}
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	files := make([]exportedFile, 0, len(out.Documents))
	for _, d := range out.Documents {
		_, category := vault.SplitPerson(d.Category)
		folder := filepath.Join(dir, "documents", category)
		if err := os.MkdirAll(folder, 0700); err != nil {
			return err
		}
		name, err := writeUniqueFile(folder, d.Name, d.ID, d.Data)
		if err != nil {
			return err
		}
		files = append(files, exportedFile{DocumentInfo: d.DocumentInfo, File: "documents/" + category + "/" + name})
	}
	data, err := json.MarshalIndent(struct {
		Person    string            `json:"person"`
		Exported  time.Time         `json:"exported"`
		Fields    []vault.FieldInfo `json:"fields"`
		Documents []exportedFile    `json:"documents"`
		// So the recipient can tell the copy is partial.
		Omitted int                  `json:"omitted,omitempty"`
		Missing []vault.DocumentInfo `json:"missing,omitempty"`
	}{out.Person, out.Exported, out.Fields, files, out.Omitted, out.Missing}, "", "  ")
	if err != nil {
		return err
	}
	return writeNewFile(filepath.Join(dir, "fields.json"), append(data, '\n'))
}

// writeUniqueFile writes data to a new file in folder named after a
// document's stored name, without any path (a name that is nothing but dots
// becomes the document ID). If that name is taken, by an earlier document
// or by one the disk treats as the same (case, Unicode normalization), it
// adds " (2)", " (3)"... before the extension. It returns the name used.
func writeUniqueFile(folder, name, id string, data []byte) (string, error) {
	name = strings.TrimLeft(filepath.Base(filepath.Clean("/"+name)), ".")
	if name == "" || name == "/" {
		name = id
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		err := writeNewFile(filepath.Join(folder, name), data)
		if !errors.Is(err, os.ErrExist) {
			return name, err
		}
		if n > 1000 {
			return "", fmt.Errorf("can't find a free name for %s in %s", name, folder)
		}
		name = stem + " (" + strconv.Itoa(n) + ")" + ext
	}
}

// writeNewFile writes data to a file that must not exist yet (0600).
func writeNewFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
