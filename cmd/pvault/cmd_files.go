package main

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/filesync"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

const filesUsage = `usage:
  pvault files source add <folder> [--label name] [--provider local|onedrive|sharepoint|gdrive|dropbox]
                                       Index a folder (a OneDrive/Google Drive/Dropbox sync folder is recognised)
  pvault files source connect onedrive|sharepoint|gdrive|dropbox --client-id <id> [--label name]
                      [--site <https address> --library <name>] [--tenant common] [--port 53682]
                                       Read an account through the provider's API, read-only (docs/usage.md:
                                       register an OAuth client first); sign in in your browser
  pvault files source reconnect <source> [--port 53682]
                                       Sign in again for a source whose sign-in expired or was revoked:
                                       keeps its index and links (same account only)
  pvault files source list
  pvault files source remove <source>  Forget a source's index entries and links (the files are untouched)
  pvault files sync [source] [--confirm-removals]
                                       Read the folders and update the index (nothing is opened or changed);
                                       the flag lets a sync mark most of a source gone
  pvault files search [text] [--for slug|io] [--pratica slug] [--source s] [--provider p]
                      [--unlinked] [--gone] [--limit n]
  pvault files show <file-id>
  pvault files open <file-id> [--print]  Show the file in Finder (never opens it)
  pvault files link <file-id> --for slug|io [--pratica slug] [--kind person|client|business|pratica]
                      [--role name] [--note text]
  pvault files unlink <link-id>
  pvault files rule add <source> --path /Clienti/Rossi --for slug|io [--pratica slug] [--kind k]
  pvault files rule list [source]
  pvault files rule remove <rule-id>
  pvault files proposals [accept|reject <id>]
                                       Files an agent found through its own connector (Claude's Google Drive...)
                                       and proposed for the index: nothing is indexed until you accept
  pvault files suggestions [accept|reject <id>]
                                       Links proposed by agents, for you to decide
  pvault files prune                   Drop index entries of files that are gone and linked to nothing`

// filesReadScope covers the owner's files and every person's.
const filesReadScope = "files.*,*:files.*"

func cmdFiles() {
	if len(os.Args) < 3 {
		fatal(filesUsage)
	}
	switch os.Args[2] {
	case "source":
		cmdFilesSource()
	case "sync":
		cmdFilesSync()
	case "search":
		cmdFilesSearch()
	case "show":
		cmdFilesShow()
	case "open":
		cmdFilesOpen()
	case "link":
		cmdFilesLink()
	case "unlink":
		cmdFilesUnlink()
	case "rule":
		cmdFilesRule()
	case "suggestions":
		cmdFilesSuggestions()
	case "proposals":
		cmdFilesProposals()
	case "prune":
		cmdFilesPrune()
	default:
		fatal(filesUsage)
	}
}

// splitBoolFlags takes the named on/off options out of args, refusing one
// given twice, and returns the rest for parseFlags.
func splitBoolFlags(args []string, names ...string) (rest []string, set map[string]bool, err error) {
	set = map[string]bool{}
	for _, a := range args {
		matched := false
		for _, n := range names {
			if a == n {
				if set[n] {
					return nil, nil, fmt.Errorf("%s given twice", n)
				}
				set[n], matched = true, true
			}
		}
		if !matched {
			rest = append(rest, a)
		}
	}
	return rest, set, nil
}

func listFileSources() []vault.FileSourceInfo {
	resp, err := apiRequest("GET", "/vault/files/sources", nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var srcs []vault.FileSourceInfo
	if err := apiResult(resp, &srcs); err != nil {
		fatal("%v", err)
	}
	return srcs
}

// findSource picks a source by label or by the start of its ID.
func findSource(srcs []vault.FileSourceInfo, ref string) vault.FileSourceInfo {
	var hit []vault.FileSourceInfo
	for _, s := range srcs {
		if strings.EqualFold(s.Label, ref) || (len(ref) >= 4 && strings.HasPrefix(s.ID, strings.ToLower(ref))) {
			hit = append(hit, s)
		}
	}
	switch len(hit) {
	case 1:
		return hit[0]
	case 0:
		fatal("no source %q: 'pvault files source list' shows them", ref)
	}
	fatal("%q matches more than one source: use more of the ID", ref)
	return vault.FileSourceInfo{}
}

func cmdFilesSource() {
	if len(os.Args) < 4 {
		fatal(filesUsage)
	}
	switch os.Args[3] {
	case "add":
		cmdFilesSourceAdd()
	case "connect":
		cmdFilesSourceConnect()
	case "reconnect":
		cmdFilesSourceReconnect()
	case "list":
		positionalArgs(os.Args[4:], filesUsage, 0, 0)
		needGrant("files.*", "read")
		srcs := listFileSources()
		if len(srcs) == 0 {
			fmt.Println("No sources yet: pvault files source add <folder>")
			return
		}
		for _, s := range srcs {
			last := s.LastSync
			if last == "" {
				last = "never synced"
			} else {
				last = "synced " + last
			}
			where := s.Root
			if s.Via == vault.ViaConnector {
				where = "files an agent found through its own connector and you accepted (never synced)"
			} else if where == "" {
				where = "through its API"
				if s.Account != "" {
					where += ": " + s.Account
				}
			}
			fmt.Printf("%s  %-10s %-24s %6d files  %s\n    %s\n", s.ID, s.Provider, s.Label, s.Files, last, where)
		}
	case "remove":
		args := positionalArgs(os.Args[4:], filesUsage, 1, 1)
		// It removes every person's links to the source's files.
		needGrant(filesReadScope, "write")
		src := findSource(listFileSources(), args[0])
		resp, err := apiRequest("DELETE", "/vault/files/sources/"+url.PathEscape(src.ID), nil)
		if err != nil {
			fatal("request failed: %v", err)
		}
		var out map[string]int64
		if err := apiResult(resp, &out); err != nil {
			fatal("%v", err)
		}
		fmt.Printf("Forgot %s: %d index entries, their links and the source's rules removed. The files themselves were not touched.\n", src.Label, out["forgotten"])
		if src.ClientID != "" {
			// The sign-in is only on this computer; it stays valid at the provider
			// until the user revokes pvault there.
			if err := secretStore.Delete(filesync.ServiceName(src.ID)); err != nil {
				fmt.Fprintf(os.Stderr, "Could not delete its saved sign-in from the Keychain (%v): remove the item %s there.\n", err, filesync.ServiceName(src.ID))
			}
			fmt.Printf("Its sign-in was removed from the Keychain. To take pvault's access away at %s too, remove it in your account's connected apps.\n", src.Provider)
		}
	default:
		fatal(filesUsage)
	}
}

func cmdFilesSourceAdd() {
	if len(os.Args) < 5 {
		fatal(filesUsage)
	}
	folder := os.Args[4]
	label, provider := "", ""
	if err := parseFlags(os.Args[5:], map[string]*string{"--label": &label, "--provider": &provider}); err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	root, err := filesync.ResolveRoot(folder)
	if err != nil {
		fatal("%v", err)
	}
	detected, account, ok := filesync.DetectProvider(root)
	if provider == "" {
		provider = "local"
		if ok {
			provider = detected
		}
	}
	if label == "" {
		label = account
		if label == "" {
			label = filepath.Base(root)
		}
	}
	needGrant("files.*", "write")
	resp, err := apiRequest("POST", "/vault/files/sources", map[string]string{
		"provider": provider, "label": label, "root": root, "account": account,
	})
	if err != nil {
		fatal("request failed: %v", err)
	}
	var src vault.FileSourceInfo
	if err := apiResult(resp, &src); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Added %s (%s) as %s.\nRun 'pvault files sync' to index it.\n", src.Label, src.Provider, src.ID)
}

// syncBatch is how many files one request carries at most (the server allows
// 500), and syncBatchBytes roughly how large one request may grow: the server
// refuses a body over 1 MB, and long accented paths grow when sent as JSON.
const (
	syncBatch      = 200
	syncBatchBytes = 400 << 10
)

// batches splits files into runs of at most syncBatch files and about
// syncBatchBytes of JSON.
func batches(files []vault.FileObservation) [][]vault.FileObservation {
	var out [][]vault.FileObservation
	start, size := 0, 0
	for i, f := range files {
		n := 2*(len(f.Key)+len(f.Path)+len(f.Name)) + 200 // worst case: every byte escaped
		if i > start && (i-start >= syncBatch || size+n > syncBatchBytes) {
			out = append(out, files[start:i])
			start, size = i, 0
		}
		size += n
	}
	if start < len(files) {
		out = append(out, files[start:])
	}
	return out
}

func cmdFilesSync() {
	rest, bools, err := splitBoolFlags(os.Args[3:], "--confirm-removals")
	if err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	args := positionalArgs(rest, filesUsage, 0, 1)
	// A sync only keeps up the files its grant can see, and a folder holds
	// people's files too.
	needGrant(filesReadScope, "write")
	srcs := listFileSources()
	if len(args) == 1 {
		srcs = []vault.FileSourceInfo{findSource(srcs, args[0])}
	}
	synced := 0
	for _, src := range srcs {
		if src.Via == vault.ViaConnector {
			if len(args) == 1 {
				fatal("%s holds files an agent found through its own connector: there is nothing to read", src.Label)
			}
			continue
		}
		synced++
		syncSource(src, bools["--confirm-removals"])
	}
	if synced == 0 {
		fmt.Println("Nothing to sync: pvault files source add <folder>")
	}
}

func syncSource(src vault.FileSourceInfo, confirmRemovals bool) {
	fmt.Printf("%s: ", src.Label)
	scan, where, ok := scanSource(src)
	if !ok {
		return
	}
	if len(scan.Files) == 0 && !scan.Complete() {
		fmt.Printf("couldn't read it, so nothing was marked gone.\n%s\n", incompleteNote(scan))
		return
	}
	if len(scan.Files) == 0 && src.Files > 0 {
		fmt.Printf("found no files in %s although %d are indexed: not touching the index. If it really is empty, 'pvault files source remove' and add it again.\n", where, src.Files)
		return
	}
	if len(scan.Files) == 0 {
		fmt.Println("no files.")
		return
	}

	id := make([]byte, 8)
	crand.Read(id)
	syncID := hex.EncodeToString(id)
	var total vault.FileSyncResult
	for n, batch := range batches(scan.Files) {
		resp, err := apiRequest("POST", "/vault/files/sync", map[string]any{
			"source_id": src.ID, "sync_id": syncID, "begin": n == 0, "files": batch,
		})
		if err != nil {
			fatal("request failed: %v", err)
		}
		var res vault.FileSyncResult
		if err := apiResult(resp, &res); err != nil {
			fmt.Printf("the vault refused a batch (%v). Stopped; nothing was marked gone.\n", err)
			return
		}
		total.Added += res.Added
		total.Updated += res.Updated
		total.Unchanged += res.Unchanged
		total.Linked += res.Linked
	}

	note := ""
	if scan.Complete() {
		resp, err := apiRequest("POST", "/vault/files/sync/finish", map[string]any{"source_id": src.ID, "sync_id": syncID, "confirm": confirmRemovals})
		if err != nil {
			fatal("request failed: %v", err)
		}
		var res vault.FileSyncResult
		if err := apiResult(resp, &res); err != nil {
			fmt.Printf("indexed, but could not finish (%v)\n", err)
			if strings.Contains(err.Error(), "confirm it") {
				fmt.Println("  If they really are gone, run 'pvault files sync --confirm-removals'.")
			}
			return
		}
		total.Gone = res.Gone
		if resp, err := apiRequest("POST", "/vault/files/sources/"+url.PathEscape(src.ID)+"/state", map[string]string{"cursor": ""}); err == nil {
			resp.Body.Close()
		}
	} else {
		note = incompleteNote(scan)
	}

	fmt.Printf("%s files", formatCount(len(scan.Files)))
	if scan.Placeholders > 0 {
		fmt.Printf(" (%d online-only, listed by name and never opened)", scan.Placeholders)
	}
	fmt.Printf(": %d new, %d changed, %d unchanged, %d gone, %d linked by rules", total.Added, total.Updated, total.Unchanged, total.Gone, total.Linked)
	if scan.Skipped > 0 {
		fmt.Printf("; %d hidden/link/odd-named left out", scan.Skipped)
	}
	fmt.Println(".")
	if note != "" {
		fmt.Println(note)
	}
}

func incompleteNote(s *filesync.Scan) string {
	var b strings.Builder
	b.WriteString("  The scan was incomplete, so nothing was marked gone:")
	if s.Truncated {
		fmt.Fprintf(&b, " it stopped at %d files;", filesync.MaxFiles)
	}
	for i, err := range s.Errors {
		if i == 3 {
			fmt.Fprintf(&b, " and %d more", len(s.Errors)-3)
			break
		}
		fmt.Fprintf(&b, "\n    %v", err)
	}
	return b.String()
}

func formatCount(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func cmdFilesSearch() {
	rest, bools, err := splitBoolFlags(os.Args[3:], "--unlinked", "--gone")
	if err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	var text []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		text = append(text, rest[0])
		rest = rest[1:]
	}
	var forRef, pratica, source, provider, limit string
	if err := parseFlags(rest, map[string]*string{"--for": &forRef, "--pratica": &pratica, "--source": &source, "--provider": &provider, "--limit": &limit}); err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	q := url.Values{}
	if len(text) > 0 {
		q.Set("q", strings.Join(text, " "))
	}
	for k, v := range map[string]string{"for": forRef, "pratica": pratica, "provider": provider, "limit": limit} {
		if v != "" {
			q.Set(k, v)
		}
	}
	needGrant(filesReadScope, "read")
	srcs := listFileSources()
	if source != "" {
		q.Set("source", findSource(srcs, source).ID)
	}
	if bools["--unlinked"] {
		q.Set("unlinked", "1")
	}
	if bools["--gone"] {
		q.Set("gone", "1")
	}
	resp, err := apiRequest("GET", "/vault/files?"+q.Encode(), nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var out struct {
		Files []vault.FileInfo `json:"files"`
		Total int              `json:"total"`
	}
	if err := apiResult(resp, &out); err != nil {
		fatal("%v", err)
	}
	if len(out.Files) == 0 {
		fmt.Println("No files found.")
		return
	}
	labels := map[string]string{}
	for _, s := range srcs {
		labels[s.ID] = s.Label
	}
	for _, f := range out.Files {
		printFile(f, labels, false)
	}
	if out.Total > len(out.Files) {
		fmt.Printf("\n%d more: narrow the search or raise --limit.\n", out.Total-len(out.Files))
	}
}

func linkSummary(links []vault.FileLinkInfo) string {
	var parts []string
	for _, l := range links {
		who := l.Holder
		switch {
		case l.Kind == vault.FileKindPratica:
			who = "pratica " + l.PraticaSlug
		case who == "":
			who = "io"
		case l.Kind == vault.FileKindClient:
			who = "client " + who
		}
		if l.Kind == vault.FileKindBusiness {
			who = "business"
		}
		if l.Role != "" {
			who += " (" + l.Role + ")"
		}
		parts = append(parts, who)
	}
	return strings.Join(parts, ", ")
}

func printFile(f vault.FileInfo, labels map[string]string, long bool) {
	state := ""
	if f.Gone {
		state = "  [gone]"
	}
	if f.Tier == vault.TierPersonal {
		state += "  [personal]"
	}
	fmt.Printf("%s  %s%s\n", f.ID, f.Name, state)
	for _, l := range f.Locations {
		where := labels[l.Source]
		if where == "" {
			where = l.Source
		}
		fmt.Printf("    %s: %s\n", where, l.Path)
		if l.WebURL != "" && long {
			fmt.Printf("    %s\n", l.WebURL)
		}
	}
	if long {
		fmt.Printf("    %s, %s\n", humanSize(f.Size), f.Mtime)
	}
	if s := linkSummary(f.Links); s != "" {
		fmt.Printf("    linked to %s\n", s)
	} else {
		fmt.Println("    not linked")
	}
}

func cmdFilesShow() {
	args := positionalArgs(os.Args[3:], filesUsage, 1, 1)
	needGrant(filesReadScope, "read")
	srcs := listFileSources()
	f := getFile(args[0])
	labels := map[string]string{}
	for _, s := range srcs {
		labels[s.ID] = s.Label
	}
	printFile(*f, labels, true)
	for _, l := range f.Links {
		from := ""
		if l.Origin != vault.FileOriginManual {
			from = ", from " + l.Origin
		}
		fmt.Printf("    link %s: %s%s\n", l.ID, l.Kind, from)
	}
}

func getFile(id string) *vault.FileInfo {
	resp, err := apiRequest("GET", "/vault/files/"+url.PathEscape(id), nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var f vault.FileInfo
	if err := apiResult(resp, &f); err != nil {
		fatal("%v", err)
	}
	return &f
}

func cmdFilesOpen() {
	rest, bools, err := splitBoolFlags(os.Args[3:], "--print")
	if err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	args := positionalArgs(rest, filesUsage, 1, 1)
	needGrant(filesReadScope, "read")
	srcs := listFileSources()
	f := getFile(args[0])
	for _, l := range f.Locations {
		var src *vault.FileSourceInfo
		for i := range srcs {
			if srcs[i].ID == l.Source {
				src = &srcs[i]
			}
		}
		if src != nil && src.Root != "" {
			p, err := localPathOf(src.Root, l.Path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", l.Path, err)
				continue
			}
			fmt.Println(p)
			if !bools["--print"] {
				revealInFinder(p)
			}
			return
		}
		if l.WebURL != "" {
			fmt.Println(l.WebURL)
			if !bools["--print"] && runtime.GOOS == "darwin" {
				exec.Command("/usr/bin/open", l.WebURL).Run()
			}
			return
		}
	}
	fatal("%s is not reachable from this computer: its source has no folder here", f.Name)
}

// localPathOf joins a source's root and a path below it, refusing a result
// that is not a regular file inside the root.
func localPathOf(root, rel string) (string, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	inside := func(q string) bool { return q == root || strings.HasPrefix(q, root+string(filepath.Separator)) }
	if !inside(p) {
		return "", fmt.Errorf("path leaves the source folder")
	}
	// A folder on the way may have become a link since the file was indexed.
	if real, err := filepath.EvalSymlinks(filepath.Dir(p)); err != nil || !inside(filepath.Join(real, filepath.Base(p))) {
		return "", fmt.Errorf("path no longer leads to a place inside the source folder")
	}
	st, err := os.Lstat(p)
	if err != nil {
		return "", fmt.Errorf("not there any more (moved or deleted; run 'pvault files sync')")
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file")
	}
	return p, nil
}

// revealInFinder selects the file in Finder: unlike opening it, that neither
// runs an application nor downloads an online-only file.
func revealInFinder(p string) {
	if runtime.GOOS == "darwin" {
		exec.Command("/usr/bin/open", "-R", p).Run()
	}
}

// holderScope is the scope ID a grant needs to link for the holder named by
// a --for value (io is the owner).
func holderScope(ref string) string {
	if ref == "" || ref == vault.OwnerRef {
		return vault.FilesScopeID("")
	}
	return vault.FilesScopeID(ref)
}

func cmdFilesLink() {
	if len(os.Args) < 4 {
		fatal(filesUsage)
	}
	id := os.Args[3]
	var forRef, pratica, kind, role, note string
	if err := parseFlags(os.Args[4:], map[string]*string{"--for": &forRef, "--pratica": &pratica, "--kind": &kind, "--role": &role, "--note": &note}); err != nil {
		fatal("%v\n%s", err, filesUsage)
	}
	if forRef == "" && pratica == "" {
		fatal("say whom the file is for: --for <slug|io> and/or --pratica <slug>\n%s", filesUsage)
	}
	needGrant(holderScope(forRef), "write")
	resp, err := apiRequest("POST", "/vault/files/"+url.PathEscape(id)+"/links", map[string]string{
		"for": forRef, "kind": kind, "pratica": pratica, "role": role, "note": note,
	})
	if err != nil {
		fatal("request failed: %v", err)
	}
	var l vault.FileLinkInfo
	if err := apiResult(resp, &l); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Linked: %s (link %s).\n", linkSummary([]vault.FileLinkInfo{l}), l.ID)
}

func cmdFilesUnlink() {
	args := positionalArgs(os.Args[3:], filesUsage, 1, 1)
	needGrant("files.*,*:files.*", "write")
	resp, err := apiRequest("DELETE", "/vault/files/links/"+url.PathEscape(args[0]), nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if err := apiResult(resp, nil); err != nil {
		fatal("%v", err)
	}
	fmt.Println("Unlinked. The file is still in the index.")
}

func cmdFilesRule() {
	if len(os.Args) < 4 {
		fatal(filesUsage)
	}
	switch os.Args[3] {
	case "add":
		if len(os.Args) < 5 {
			fatal(filesUsage)
		}
		srcRef := os.Args[4]
		var p, forRef, pratica, kind string
		if err := parseFlags(os.Args[5:], map[string]*string{"--path": &p, "--for": &forRef, "--pratica": &pratica, "--kind": &kind}); err != nil {
			fatal("%v\n%s", err, filesUsage)
		}
		if p == "" || (forRef == "" && pratica == "") {
			fatal("a rule needs --path and --for and/or --pratica\n%s", filesUsage)
		}
		// Finding the source needs the owner's files too; the rule itself
		// only the holder's.
		scope := holderScope(forRef)
		if scope != vault.FilesScopeID("") {
			scope += "," + vault.FilesScopeID("")
		}
		needGrant(scope, "write")
		src := findSource(listFileSources(), srcRef)
		resp, err := apiRequest("POST", "/vault/files/rules", map[string]string{
			"source_id": src.ID, "path": p, "for": forRef, "kind": kind, "pratica": pratica,
		})
		if err != nil {
			fatal("request failed: %v", err)
		}
		var out struct {
			Rule   vault.FileRuleInfo `json:"rule"`
			Linked int                `json:"linked"`
		}
		if err := apiResult(resp, &out); err != nil {
			fatal("%v", err)
		}
		fmt.Printf("Rule %s: files under %s of %s are linked to %s. Linked %d existing file(s) now; new ones are linked when indexed.\n",
			out.Rule.ID, out.Rule.Path, src.Label, linkSummary([]vault.FileLinkInfo{{Holder: out.Rule.Holder, Kind: out.Rule.Kind, PraticaSlug: out.Rule.Pratica}}), out.Linked)
	case "list":
		args := positionalArgs(os.Args[4:], filesUsage, 0, 1)
		needGrant(filesReadScope, "read")
		srcs := listFileSources()
		q := ""
		if len(args) == 1 {
			q = "?source=" + url.QueryEscape(findSource(srcs, args[0]).ID)
		}
		resp, err := apiRequest("GET", "/vault/files/rules"+q, nil)
		if err != nil {
			fatal("request failed: %v", err)
		}
		var rules []vault.FileRuleInfo
		if err := apiResult(resp, &rules); err != nil {
			fatal("%v", err)
		}
		if len(rules) == 0 {
			fmt.Println("No rules.")
		}
		labels := map[string]string{}
		for _, s := range srcs {
			labels[s.ID] = s.Label
		}
		for _, r := range rules {
			fmt.Printf("%s  %s %s -> %s\n", r.ID, labels[r.SourceID], r.Path, linkSummary([]vault.FileLinkInfo{{Holder: r.Holder, Kind: r.Kind, PraticaSlug: r.Pratica}}))
		}
	case "remove":
		args := positionalArgs(os.Args[4:], filesUsage, 1, 1)
		needGrant(filesReadScope, "write")
		resp, err := apiRequest("DELETE", "/vault/files/rules/"+url.PathEscape(args[0]), nil)
		if err != nil {
			fatal("request failed: %v", err)
		}
		if err := apiResult(resp, nil); err != nil {
			fatal("%v", err)
		}
		fmt.Println("Rule removed. Links it made stay: 'pvault files unlink <link-id>' removes one.")
	default:
		fatal(filesUsage)
	}
}

func cmdFilesSuggestions() {
	args := os.Args[3:]
	if len(args) == 0 {
		needGrant(filesReadScope, "read")
		resp, err := apiRequest("GET", "/vault/files/suggestions", nil)
		if err != nil {
			fatal("request failed: %v", err)
		}
		var sugs []vault.FileSuggestionInfo
		if err := apiResult(resp, &sugs); err != nil {
			fatal("%v", err)
		}
		if len(sugs) == 0 {
			fmt.Println("No suggestions waiting.")
			return
		}
		for _, s := range sugs {
			f := getFile(s.FileID)
			fmt.Printf("%s  %s proposes %s for %s\n", s.ID, s.ProposedBy, linkSummary([]vault.FileLinkInfo{{Holder: s.Holder, Kind: s.Kind, PraticaSlug: s.Pratica}}), f.Name)
			if len(f.Locations) > 0 {
				fmt.Printf("    %s\n", f.Locations[0].Path)
			}
			if s.Reason != "" {
				fmt.Printf("    reason: %s\n", s.Reason) // the agent's words, not ours
			}
		}
		fmt.Println("\npvault files suggestions accept <id> | reject <id>")
		return
	}
	if len(args) != 2 || (args[0] != "accept" && args[0] != "reject") {
		fatal(filesUsage)
	}
	needGrant("files.*,*:files.*", "write")
	resp, err := apiRequest("POST", "/vault/files/suggestions/"+url.PathEscape(args[1])+"/"+args[0], nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if err := apiResult(resp, nil); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Suggestion %sed.\n", args[0])
}

func cmdFilesPrune() {
	positionalArgs(os.Args[3:], filesUsage, 0, 0)
	needGrant("files.*", "write")
	resp, err := apiRequest("POST", "/vault/files/prune", map[string]string{})
	if err != nil {
		fatal("request failed: %v", err)
	}
	var out map[string]int
	if err := apiResult(resp, &out); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Dropped %d index entries of files that are gone and linked to nothing.\n", out["pruned"])
}

func cmdFilesProposals() {
	args := os.Args[3:]
	if len(args) == 0 {
		needGrant(filesReadScope, "read")
		resp, err := apiRequest("GET", "/vault/files/proposals", nil)
		if err != nil {
			fatal("request failed: %v", err)
		}
		var list []vault.FileProposalInfo
		if err := apiResult(resp, &list); err != nil {
			fatal("%v", err)
		}
		if len(list) == 0 {
			fmt.Println("No proposals waiting.")
			return
		}
		for _, p := range list {
			// Everything after the id is the agent's: names and words from outside.
			fmt.Printf("%s  %s proposes adding %q from %s\n", p.ID, p.ProposedBy, p.Name, providerName(p.Provider))
			fmt.Printf("    %s\n", p.Path)
			if p.WebURL != "" {
				fmt.Printf("    %s\n", p.WebURL)
			}
			if p.Holder != "" || p.Pratica != "" || p.Kind != "" {
				fmt.Printf("    and linking it to %s\n", linkSummary([]vault.FileLinkInfo{{Holder: p.Holder, Kind: p.Kind, PraticaSlug: p.Pratica, Role: p.Role}}))
			}
			if p.Reason != "" {
				fmt.Printf("    reason: %s\n", p.Reason)
			}
		}
		fmt.Println("\nIt is only a pointer, and only as fresh as today. pvault files proposals accept <id> | reject <id>")
		return
	}
	if len(args) != 2 || (args[0] != "accept" && args[0] != "reject") {
		fatal(filesUsage)
	}
	needGrant(filesReadScope, "write")
	resp, err := apiRequest("POST", "/vault/files/proposals/"+url.PathEscape(args[1])+"/"+args[0], nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var out struct {
		File *vault.FileInfo `json:"file"`
	}
	if err := apiResult(resp, &out); err != nil {
		fatal("%v", err)
	}
	if args[0] == "reject" {
		fmt.Println("Proposal rejected.")
		return
	}
	fmt.Printf("Added to the index: %s (%s).\n", out.File.Name, out.File.ID)
}

func providerName(p string) string {
	return map[string]string{"gdrive": "Google Drive", "onedrive": "OneDrive", "sharepoint": "SharePoint", "dropbox": "Dropbox"}[p]
}
