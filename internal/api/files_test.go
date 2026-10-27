package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

type fileList struct {
	Files []vault.FileInfo `json:"files"`
	Total int              `json:"total"`
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return out
}

func fileObs(key, path, name string) map[string]any {
	return map[string]any{"key": key, "path": path, "name": name, "size": 10}
}

// filesEnv is a vault with a person, a local source and three synced files.
func filesEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	env := setup(t)
	if err := env.vault.Set("leo:famiglia.relazione", "figlio", ""); err != nil {
		t.Fatal(err)
	}
	w := env.doRequest(t, "POST", "/vault/files/sources", map[string]string{"provider": "local", "label": "Acme", "root": "/tmp/acme"}, true)
	if w.Code != 200 {
		t.Fatalf("add source: %d %s", w.Code, w.Body.String())
	}
	src := decode[vault.FileSourceInfo](t, w)
	w = env.doRequest(t, "POST", "/vault/files/sync", map[string]any{
		"source_id": src.ID, "sync_id": "sync-0001", "begin": true,
		"files": []map[string]any{
			fileObs("/Casa/contratto.pdf", "/Casa/contratto.pdf", "contratto.pdf"),
			fileObs("/Leo/scuola.pdf", "/Leo/scuola.pdf", "scuola.pdf"),
			fileObs("/Leo/SPID password.txt", "/Leo/SPID password.txt", "SPID password.txt"),
		},
	}, true)
	if w.Code != 200 {
		t.Fatalf("sync: %d %s", w.Code, w.Body.String())
	}
	return env, src.ID
}

func findFile(t *testing.T, env *testEnv, name string) vault.FileInfo {
	t.Helper()
	w := env.doRequest(t, "GET", "/vault/files?q="+strings.ReplaceAll(name, " ", "+"), nil, true)
	l := decode[fileList](t, w)
	if len(l.Files) != 1 {
		t.Fatalf("%q matched %d files", name, len(l.Files))
	}
	return l.Files[0]
}

func TestFiles_SessionFlow(t *testing.T) {
	env, src := filesEnv(t)

	// Finishing the sync with one file missing tombstones only that one.
	env.doRequest(t, "POST", "/vault/files/sync", map[string]any{
		"source_id": src, "sync_id": "sync-0002", "begin": true,
		"files": []map[string]any{
			fileObs("/Casa/contratto.pdf", "/Casa/contratto.pdf", "contratto.pdf"),
			fileObs("/Leo/scuola.pdf", "/Leo/scuola.pdf", "scuola.pdf"),
		},
	}, true)
	w := env.doRequest(t, "POST", "/vault/files/sync/finish", map[string]string{"source_id": src, "sync_id": "sync-0002"}, true)
	if res := decode[vault.FileSyncResult](t, w); w.Code != 200 || res.Gone != 1 {
		t.Fatalf("finish = %d %+v", w.Code, res)
	}
	if l := decode[fileList](t, env.doRequest(t, "GET", "/vault/files", nil, true)); l.Total != 2 {
		t.Fatalf("listed %d, want 2 (one is gone)", l.Total)
	}
	if l := decode[fileList](t, env.doRequest(t, "GET", "/vault/files?gone=1", nil, true)); l.Total != 3 {
		t.Fatalf("with gone: %d, want 3", l.Total)
	}

	// Link, search by holder, unlink.
	f := findFile(t, env, "scuola")
	w = env.doRequest(t, "POST", "/vault/files/"+f.ID+"/links", map[string]string{"for": "leo", "role": "pagella"}, true)
	if w.Code != 200 {
		t.Fatalf("link: %d %s", w.Code, w.Body.String())
	}
	link := decode[vault.FileLinkInfo](t, w)
	if l := decode[fileList](t, env.doRequest(t, "GET", "/vault/files?for=leo", nil, true)); l.Total != 1 {
		t.Fatalf("leo's files = %d", l.Total)
	}
	if l := decode[fileList](t, env.doRequest(t, "GET", "/vault/files?unlinked=1", nil, true)); l.Total != 1 {
		t.Fatalf("unlinked = %d", l.Total)
	}
	if w := env.doRequest(t, "DELETE", "/vault/files/links/"+link.ID, nil, true); w.Code != 200 {
		t.Fatalf("unlink: %d", w.Code)
	}
	if w := env.doRequest(t, "DELETE", "/vault/files/links/"+link.ID, nil, true); w.Code != 404 {
		t.Fatalf("unlink twice: %d", w.Code)
	}

	// Bad input.
	if w := env.doRequest(t, "POST", "/vault/files/"+f.ID+"/links", map[string]string{"for": "ghost"}, true); w.Code != 400 {
		t.Fatalf("link to nobody: %d", w.Code)
	}
	if w := env.doRequest(t, "GET", "/vault/files?for=BAD%20SLUG", nil, true); w.Code != 400 {
		t.Fatalf("bad for: %d", w.Code)
	}
	if w := env.doRequest(t, "GET", "/vault/files/nonexistent", nil, true); w.Code != 404 {
		t.Fatalf("missing file: %d", w.Code)
	}
}

func TestFiles_FinishNeedsARememberedSync(t *testing.T) {
	env, src := filesEnv(t)
	w := env.doRequest(t, "POST", "/vault/files/sync/finish", map[string]string{"source_id": src, "sync_id": "never-ran-1"}, true)
	if w.Code != 400 {
		t.Fatalf("finish of an unknown sync: %d", w.Code)
	}
	if l := decode[fileList](t, env.doRequest(t, "GET", "/vault/files?gone=1", nil, true)); l.Total != 3 {
		t.Fatal("a failed finish must not tombstone anything")
	}
	// A new sync replaces the running one: the old id can no longer finish.
	env.doRequest(t, "POST", "/vault/files/sync", map[string]any{"source_id": src, "sync_id": "sync-0009", "begin": true, "files": []map[string]any{}}, true)
	if w := env.doRequest(t, "POST", "/vault/files/sync/finish", map[string]string{"source_id": src, "sync_id": "sync-0001"}, true); w.Code != 400 {
		t.Fatalf("finish of a replaced sync: %d", w.Code)
	}
}

func TestFiles_SyncRejectsSharingLinksAndBigBatches(t *testing.T) {
	env, src := filesEnv(t)
	bad := fileObs("k", "/a.pdf", "a.pdf")
	bad["web_url"] = "https://1drv.ms/b/s!AbC"
	w := env.doRequest(t, "POST", "/vault/files/sync", map[string]any{"source_id": src, "files": []map[string]any{bad}}, true)
	if w.Code != 400 {
		t.Fatalf("sharing link: %d %s", w.Code, w.Body.String())
	}
	many := make([]map[string]any, maxSyncBatch+1)
	for i := range many {
		many[i] = fileObs("k"+string(rune('a'+i%26))+strings.Repeat("x", i/26), "/a", "a.pdf")
	}
	if w := env.doRequest(t, "POST", "/vault/files/sync", map[string]any{"source_id": src, "files": many}, true); w.Code != 400 {
		t.Fatalf("oversized batch: %d", w.Code)
	}
	if w := env.doRequest(t, "POST", "/vault/files/sync", map[string]any{"source_id": src, "sync_id": "x", "begin": true, "files": []map[string]any{}}, true); w.Code != 400 {
		t.Fatalf("short sync id: %d", w.Code)
	}
}

func TestFiles_ServiceTokenBoundaries(t *testing.T) {
	env, src := filesEnv(t)
	scuola := findFile(t, env, "scuola")
	env.doRequest(t, "POST", "/vault/files/"+scuola.ID+"/links", map[string]string{"for": "leo"}, true)

	owner := createTokenWithAccess(t, env, "files.*", "read")
	leo := createTokenWithAccess(t, env, "leo:files.*", "read")
	names := func(token string) string {
		w := env.doRequestWithToken(t, "GET", "/vault/files", nil, token)
		if w.Code != 200 {
			t.Fatalf("search: %d %s", w.Code, w.Body.String())
		}
		var out []string
		for _, f := range decode[fileList](t, w).Files {
			out = append(out, f.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(owner); got != "contratto.pdf" {
		t.Fatalf("owner-scope token sees %q (not Leo's, never the personal one)", got)
	}
	if got := names(leo); got != "scuola.pdf" {
		t.Fatalf("leo-scope token sees %q", got)
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/files/"+scuola.ID, nil, owner); w.Code != 404 {
		t.Fatalf("a file outside the scope: %d, want 404", w.Code)
	}
	spid := findFile(t, env, "SPID")
	if w := env.doRequestWithToken(t, "GET", "/vault/files/"+spid.ID, nil, leo); w.Code != 404 {
		t.Fatalf("a personal file for a token: %d, want 404", w.Code)
	}

	// A token can't change who owns what, or add places to read.
	for _, c := range []struct{ method, path string }{
		{"POST", "/vault/files/" + scuola.ID + "/links"},
		{"POST", "/vault/files/rules"},
		{"POST", "/vault/files/sources"},
		{"GET", "/vault/files/rules"},
		{"GET", "/vault/files/suggestions"},
		{"POST", "/vault/files/prune"},
		{"DELETE", "/vault/files/sources/" + src},
	} {
		wr := createTokenWithAccess(t, env, "*", "write")
		if w := env.doRequestWithToken(t, c.method, c.path, map[string]string{"for": "leo", "provider": "local", "label": "x", "root": "/x"}, wr); w.Code != 403 {
			t.Errorf("%s %s with a write token = %d, want 403", c.method, c.path, w.Code)
		}
	}

	// A read-only token can't sync; a write token scoped to files can.
	if w := env.doRequestWithToken(t, "POST", "/vault/files/sync", map[string]any{"source_id": src, "files": []map[string]any{}}, owner); w.Code != 403 {
		t.Fatalf("read-only sync: %d", w.Code)
	}
	sync := createTokenWithAccess(t, env, "files.*", "write")
	if w := env.doRequestWithToken(t, "POST", "/vault/files/sync", map[string]any{"source_id": src, "files": []map[string]any{fileObs("/Casa/contratto.pdf", "/Casa/contratto.pdf", "contratto.pdf")}}, sync); w.Code != 200 {
		t.Fatalf("sync token: %d %s", w.Code, w.Body.String())
	}
	other := createTokenWithAccess(t, env, "identity.*", "write")
	if w := env.doRequestWithToken(t, "POST", "/vault/files/sync", map[string]any{"source_id": src, "files": []map[string]any{}}, other); w.Code != 403 {
		t.Fatalf("a token with no files scope synced: %d", w.Code)
	}
}

func TestFiles_AgentSuggestsUserDecides(t *testing.T) {
	env, _ := filesEnv(t)
	scuola := findFile(t, env, "scuola")
	agent := createTokenWithAccess(t, env, "*:files.*", "append")

	w := env.doRequestWithToken(t, "POST", "/vault/files/"+scuola.ID+"/suggest", map[string]string{"for": "leo", "reason": "folder says Leo"}, agent)
	if w.Code != 200 {
		t.Fatalf("suggest: %d %s", w.Code, w.Body.String())
	}
	sug := decode[vault.FileSuggestionInfo](t, w)
	if sug.ProposedBy == "" || sug.ProposedBy == "you" {
		t.Fatalf("proposed_by = %q, want the token's consumer", sug.ProposedBy)
	}
	if w := env.doRequestWithToken(t, "POST", "/vault/files/suggestions/"+sug.ID+"/accept", nil, agent); w.Code != 403 {
		t.Fatalf("a token accepting its own suggestion: %d", w.Code)
	}
	if l := decode[fileList](t, env.doRequest(t, "GET", "/vault/files?for=leo", nil, true)); l.Total != 0 {
		t.Fatal("a suggestion must not link")
	}
	list := decode[[]vault.FileSuggestionInfo](t, env.doRequest(t, "GET", "/vault/files/suggestions", nil, true))
	if len(list) != 1 {
		t.Fatalf("suggestions = %d", len(list))
	}
	if w := env.doRequest(t, "POST", "/vault/files/suggestions/"+sug.ID+"/accept", nil, true); w.Code != 200 {
		t.Fatalf("accept: %d %s", w.Code, w.Body.String())
	}
	if l := decode[fileList](t, env.doRequest(t, "GET", "/vault/files?for=leo", nil, true)); l.Total != 1 {
		t.Fatal("accepted suggestion didn't link")
	}
	if w := env.doRequest(t, "POST", "/vault/files/suggestions/"+sug.ID+"/bogus", nil, true); w.Code != 404 {
		t.Fatalf("unknown action: %d", w.Code)
	}

	// A read-only token can't suggest; a narrow one can't suggest for Leo.
	ro := createTokenWithAccess(t, env, "*:files.*", "read")
	if w := env.doRequestWithToken(t, "POST", "/vault/files/"+scuola.ID+"/suggest", map[string]string{"for": "leo"}, ro); w.Code != 403 {
		t.Fatalf("read-only suggest: %d", w.Code)
	}
	narrow := createTokenWithAccess(t, env, "files.*", "append")
	if w := env.doRequestWithToken(t, "POST", "/vault/files/"+scuola.ID+"/suggest", map[string]string{"for": "leo"}, narrow); w.Code != 403 {
		t.Fatalf("suggest for a holder outside the scope: %d", w.Code)
	}
}

func TestFiles_RulesOverTheAPI(t *testing.T) {
	env, src := filesEnv(t)
	w := env.doRequest(t, "POST", "/vault/files/rules", map[string]string{"source_id": src, "path": "/Leo", "for": "leo"}, true)
	if w.Code != 200 {
		t.Fatalf("rule: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Linked int `json:"linked"`
	}
	json.NewDecoder(w.Body).Decode(&out)
	if out.Linked != 2 {
		t.Fatalf("rule linked %d files, want 2", out.Linked)
	}
	rules := decode[[]vault.FileRuleInfo](t, env.doRequest(t, "GET", "/vault/files/rules", nil, true))
	if len(rules) != 1 || rules[0].Path != "/Leo" {
		t.Fatalf("rules = %+v", rules)
	}
	if w := env.doRequest(t, "DELETE", "/vault/files/rules/"+rules[0].ID, nil, true); w.Code != 200 {
		t.Fatalf("remove rule: %d", w.Code)
	}
}

func TestFiles_ForgetAndPrune(t *testing.T) {
	env, src := filesEnv(t)
	env.doRequest(t, "POST", "/vault/files/sync", map[string]any{"source_id": src, "full": true, "files": []map[string]any{}}, true)
	w := env.doRequest(t, "POST", "/vault/files/prune", nil, true)
	if got := decode[map[string]int](t, w); got["pruned"] != 3 {
		t.Fatalf("prune = %v", got)
	}
	if w := env.doRequest(t, "DELETE", "/vault/files/sources/"+src, nil, true); w.Code != 200 {
		t.Fatalf("forget: %d", w.Code)
	}
	if srcs := decode[[]vault.FileSourceInfo](t, env.doRequest(t, "GET", "/vault/files/sources", nil, true)); len(srcs) != 0 {
		t.Fatalf("sources left: %+v", srcs)
	}
}

// filesEnvSK is filesEnv with the secret key, to ask for scoped grants.
func filesEnvSK(t *testing.T) (*testEnv, string, string) {
	t.Helper()
	env, sk := setupWithSecretKey(t)
	env.vault.Set("leo:famiglia.relazione", "figlio", "")
	w := env.doRequest(t, "POST", "/vault/files/sources", map[string]string{"provider": "local", "label": "Acme", "root": "/tmp/acme"}, true)
	src := decode[vault.FileSourceInfo](t, w)
	env.doRequest(t, "POST", "/vault/files/sync", map[string]any{
		"source_id": src.ID, "full": true,
		"files": []map[string]any{fileObs("/a.pdf", "/a.pdf", "a.pdf"), fileObs("/Leo/b.pdf", "/Leo/b.pdf", "b.pdf")},
	}, true)
	return env, sk, src.ID
}

// A grant the user approved is limited by what they approved: its access
// level and the holders in its scope. (Found by review: a read grant for
// files.* could forget a source, erasing every person's links.)
func TestFiles_ApprovedGrantsAreHeldToWhatWasApproved(t *testing.T) {
	env, sk, src := filesEnvSK(t)
	a, b := findFile(t, env, "a.pdf"), findFile(t, env, "b.pdf")
	env.doRequest(t, "POST", "/vault/files/"+b.ID+"/links", map[string]string{"for": "leo"}, true)
	b = findFile(t, env, "b.pdf")
	leoLink := b.Links[0].ID
	rule := decode[struct {
		Rule vault.FileRuleInfo `json:"rule"`
	}](t, env.doRequest(t, "POST", "/vault/files/rules", map[string]string{"source_id": src, "path": "/Leo", "for": "leo"}, true)).Rule
	sug := decode[vault.FileSuggestionInfo](t, env.doRequestWithToken(t, "POST", "/vault/files/"+a.ID+"/suggest",
		map[string]string{"for": "leo"}, createTokenWithAccess(t, env, "*:files.*", "append")))

	// Read-only grant: nothing that changes the index.
	ro := scopedGrant(t, env, sk, "files.*,*:files.*", "read")
	for _, c := range []struct{ method, path string }{
		{"POST", "/vault/files/sources"}, {"POST", "/vault/files/prune"}, {"DELETE", "/vault/files/links/" + leoLink},
		{"DELETE", "/vault/files/sources/" + src}, {"DELETE", "/vault/files/rules/" + rule.ID},
		{"POST", "/vault/files/suggestions/" + sug.ID + "/accept"}, {"POST", "/vault/files/" + a.ID + "/links"},
	} {
		w := env.doRequestWithToken(t, c.method, c.path, map[string]string{"provider": "local", "label": "x", "root": "/x", "for": "io"}, ro)
		if w.Code != 403 {
			t.Errorf("%s %s with a read grant = %d, want 403", c.method, c.path, w.Code)
		}
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/files/rules", nil, ro); w.Code != 200 {
		t.Errorf("a read grant lists rules: %d", w.Code)
	}

	// A write grant for the owner's files only can't touch leo's rows.
	owner := scopedGrant(t, env, sk, "files.*", "write")
	for _, c := range []struct{ method, path string }{
		{"DELETE", "/vault/files/links/" + leoLink}, {"DELETE", "/vault/files/rules/" + rule.ID},
		{"POST", "/vault/files/suggestions/" + sug.ID + "/accept"},
	} {
		if w := env.doRequestWithToken(t, c.method, c.path, nil, owner); w.Code != 404 {
			t.Errorf("%s %s with an owner-only grant = %d, want 404", c.method, c.path, w.Code)
		}
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/files/sources/"+src, nil, owner); w.Code != 403 {
		t.Errorf("forgetting a source with an owner-only grant = %d, want 403", w.Code)
	}
	if got := decode[[]vault.FileRuleInfo](t, env.doRequestWithToken(t, "GET", "/vault/files/rules", nil, owner)); len(got) != 0 {
		t.Errorf("an owner-only grant lists leo's rules: %+v", got)
	}
	if got := decode[[]vault.FileSuggestionInfo](t, env.doRequestWithToken(t, "GET", "/vault/files/suggestions", nil, owner)); len(got) != 0 {
		t.Errorf("an owner-only grant lists leo's suggestions: %+v", got)
	}

	// A grant for leo's files can act for leo, and only for leo.
	leo := scopedGrant(t, env, sk, "leo:files.*", "write")
	if w := env.doRequestWithToken(t, "POST", "/vault/files/"+a.ID+"/links", map[string]string{"for": "io"}, leo); w.Code != 403 {
		t.Errorf("leo's grant linking for the owner = %d, want 403", w.Code)
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/files/links/"+leoLink, nil, leo); w.Code != 200 {
		t.Errorf("leo's grant unlinking leo's link = %d", w.Code)
	}

	// Only a grant reaching everyone may forget a source.
	all := scopedGrant(t, env, sk, "files.*,*:files.*", "write")
	if w := env.doRequestWithToken(t, "DELETE", "/vault/files/sources/"+src, nil, all); w.Code != 200 {
		t.Errorf("forgetting with a grant for everyone = %d %s", w.Code, w.Body.String())
	}
}

// A person's file that lost its last link doesn't become the owner's.
func TestFiles_UnlinkedPersonFileStaysHiddenFromOwnerToken(t *testing.T) {
	env, _, _ := filesEnvSK(t)
	b := findFile(t, env, "b.pdf")
	l := decode[vault.FileLinkInfo](t, env.doRequest(t, "POST", "/vault/files/"+b.ID+"/links", map[string]string{"for": "leo"}, true))
	env.doRequest(t, "DELETE", "/vault/files/links/"+l.ID, nil, true)
	owner := createTokenWithAccess(t, env, "files.*", "read")
	if w := env.doRequestWithToken(t, "GET", "/vault/files/"+b.ID, nil, owner); w.Code != 404 {
		t.Fatalf("owner token reads leo's unlinked file: %d", w.Code)
	}
	everyone := createTokenWithAccess(t, env, "*:files.*", "read")
	if w := env.doRequestWithToken(t, "GET", "/vault/files/"+b.ID, nil, everyone); w.Code != 200 {
		t.Fatalf("a token reaching everyone: %d", w.Code)
	}
}

func TestFiles_FinishRefusesToMarkMostOfASourceGone(t *testing.T) {
	env := setup(t)
	src := decode[vault.FileSourceInfo](t, env.doRequest(t, "POST", "/vault/files/sources", map[string]string{"provider": "local", "label": "A", "root": "/tmp/a"}, true))
	var files []map[string]any
	for i := 0; i < 30; i++ {
		files = append(files, fileObs(fmt.Sprint("k", i), "/f.pdf", "f.pdf"))
	}
	env.doRequest(t, "POST", "/vault/files/sync", map[string]any{"source_id": src.ID, "full": true, "files": files}, true)

	env.doRequest(t, "POST", "/vault/files/sync", map[string]any{"source_id": src.ID, "sync_id": "sync-hhhh-1", "begin": true, "files": files[:3]}, true)
	w := env.doRequest(t, "POST", "/vault/files/sync/finish", map[string]any{"source_id": src.ID, "sync_id": "sync-hhhh-1"}, true)
	if w.Code != 400 {
		t.Fatalf("finish marking 27 of 30 gone: %d", w.Code)
	}
	w = env.doRequest(t, "POST", "/vault/files/sync/finish", map[string]any{"source_id": src.ID, "sync_id": "sync-hhhh-1", "confirm": true}, true)
	if res := decode[vault.FileSyncResult](t, w); w.Code != 200 || res.Gone != 27 {
		t.Fatalf("confirmed finish: %d %+v", w.Code, res)
	}
}
