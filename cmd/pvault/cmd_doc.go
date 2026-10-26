package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

const docUsage = `usage:
  pvault doc add <category> <file> [--sensitivity tier]   e.g. pvault doc add permesso_soggiorno ~/Desktop/permesso.pdf
                                                          or pvault doc add leo:cie ~/Desktop/cie-leo.pdf
  pvault doc list [category]
  pvault doc get <id> [--out path]
  pvault doc delete <id>`

func cmdDoc() {
	if len(os.Args) < 3 {
		fatal(docUsage)
	}
	switch os.Args[2] {
	case "add":
		cmdDocAdd()
	case "list":
		cmdDocList()
	case "get":
		cmdDocGet()
	case "delete":
		cmdDocDelete()
	default:
		fatal(docUsage)
	}
}

func cmdDocAdd() {
	if len(os.Args) < 5 {
		fatal(docUsage)
	}
	category, path := os.Args[3], os.Args[4]
	sensitivity := ""
	if err := parseFlags(os.Args[5:], map[string]*string{"--sensitivity": &sensitivity}); err != nil {
		fatal("%v\n%s", err, docUsage)
	}
	checkDocCategory(category)
	needGrant(category+".documents", "append")

	f, err := os.Open(path)
	if err != nil {
		fatal("%v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		fatal("%v", err)
	}
	if st.IsDir() {
		fatal("%s is a directory", path)
	}
	if st.Size() > vault.MaxDocumentSize {
		fatal("%s is larger than %d MB", path, vault.MaxDocumentSize>>20)
	}

	q := url.Values{"category": {category}, "name": {filepath.Base(path)}}
	if sensitivity != "" {
		q.Set("sensitivity", sensitivity)
	}
	resp, err := apiRawRequest("POST", "/vault/documents?"+q.Encode(), detectContentType(f), f)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var doc vault.DocumentInfo
	if err := apiResult(resp, &doc); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Stored %s as %s (%s, %s) [%s]\n", doc.Name, doc.ID, doc.Category, humanSize(doc.Size), doc.Sensitivity)
	fmt.Println("The vault holds an encrypted copy; you can now delete or securely archive the original.")
}

func cmdDocList() {
	path := "/vault/documents"
	needGrant("*", "read")
	if args := positionalArgs(os.Args[3:], docUsage, 0, 1); len(args) == 1 {
		checkDocCategory(args[0])
		path += "?" + url.Values{"category": {args[0]}}.Encode()
		needGrant(args[0]+".documents", "read")
	}
	resp, err := apiRequest("GET", path, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var docs []vault.DocumentInfo
	if err := apiResult(resp, &docs); err != nil {
		fatal("%v", err)
	}
	if len(docs) == 0 {
		fmt.Println("No documents stored.")
		return
	}
	width := 20
	for _, d := range docs {
		width = max(width, len(d.Category))
	}
	for _, d := range docs {
		fmt.Printf("%s  %-*s %-40s %8s  %s  [%s]\n", d.ID, width, d.Category, d.Name, humanSize(d.Size), d.CreatedAt.Format("2006-01-02"), d.Sensitivity)
	}
}

// checkDocCategory fails on a malformed category before any prompt, so the
// Touch ID prompt names a real one.
func checkDocCategory(category string) {
	if !vault.ValidCategoryName(category) {
		fatal("invalid category %q: only letters, digits, underscore and hyphen, with an optional member: prefix (e.g. leo:cie)", category)
	}
}

func cmdDocGet() {
	if len(os.Args) < 4 {
		fatal(docUsage)
	}
	id := os.Args[3]
	out := ""
	if err := parseFlags(os.Args[4:], map[string]*string{"--out": &out}); err != nil {
		fatal("%v\n%s", err, docUsage)
	}

	// The ID doesn't say which category the document is in.
	needGrant("*", "read")
	resp, err := apiRequest("GET", "/vault/documents/"+url.PathEscape(id), nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		fatal("%v", apiResult(resp, nil))
	}
	if out == "" {
		out = filenameFromDisposition(resp.Header.Get("Content-Disposition"))
		if out == "" {
			out = id
		}
	}

	// Decrypted files are written owner-only and never replace existing files.
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			fatal("%s already exists — pass --out <path> to choose another name", out)
		}
		fatal("%v", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(out)
		fatal("writing %s: %v", out, err)
	}
	if err := f.Close(); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Decrypted to %s — this copy is NOT encrypted; delete it when you're done.\n", out)
}

func cmdDocDelete() {
	id := positionalArgs(os.Args[3:], docUsage, 1, 1)[0]
	needGrant("*", "write")
	resp, err := apiRequest("DELETE", "/vault/documents/"+url.PathEscape(id), nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if err := apiResult(resp, nil); err != nil {
		fatal("%v", err)
	}
	fmt.Println("Document deleted.")
}

// detectContentType uses the file extension, falling back to sniffing, and
// leaves the file positioned at the start.
func detectContentType(f *os.File) string {
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(f.Name()))); ct != "" {
		return ct
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	f.Seek(0, io.SeekStart)
	return http.DetectContentType(buf[:n])
}

// filenameFromDisposition extracts a safe base filename from the server's
// Content-Disposition header. Leading dots are stripped so a stored name
// (possibly chosen by an agent) can never create a dotfile such as .zshrc.
func filenameFromDisposition(header string) string {
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	name := strings.TrimLeft(filepath.Base(params["filename"]), ".")
	if name == "" || name == "/" {
		return ""
	}
	return name
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
