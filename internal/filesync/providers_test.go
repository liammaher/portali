package filesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// signedIn is a token source whose access token is good for an hour, so the
// provider tests see only the provider's API.
func signedIn(t *testing.T) *TokenSource {
	t.Helper()
	store := &MemoryStore{}
	if err := SaveToken(store, "svc", &Token{AccessToken: "tok-1", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	ts := newFakeTokenServer(t)
	src, err := NewTokenSource(testFlow(ts), store, "svc")
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func testClient(t *testing.T, base string) *apiClient {
	t.Helper()
	c, err := newAPIClient(nil, signedIn(t), base)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// paths lists a scan's files as "path|key", sorted.
func paths(s *Scan) string {
	var out []string
	for _, f := range s.Files {
		out = append(out, f.Path+"|"+f.Key)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func TestAPIClient_TokenGoesOnlyToTheProvidersHost(t *testing.T) {
	var got string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		writeJSON(w, map[string]string{"ok": "1"})
	}))
	defer api.Close()
	c := testClient(t, api.URL)
	if _, err := c.get(context.Background(), api.URL+"/x"); err != nil || got != "Bearer tok-1" {
		t.Fatalf("own host: %v, auth %q", err, got)
	}
	got = ""
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("Authorization") }))
	defer evil.Close()
	if _, err := c.get(context.Background(), evil.URL+"/steal"); err == nil || got != "" {
		t.Fatalf("a link to another host: %v, auth sent %q", err, got)
	}
}

func TestAPIClient_RetriesWhatItIsToldToAndRefreshesOnce(t *testing.T) {
	var calls int32
	var waits []time.Duration
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n := atomic.AddInt32(&calls, 1); {
		case n == 1:
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		case n == 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			writeJSON(w, map[string]string{"ok": "1"})
		}
	}))
	defer api.Close()
	c := testClient(t, api.URL)
	c.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	if _, err := c.get(context.Background(), api.URL+"/x"); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 2 || waits[0] != 7*time.Second || waits[1] < time.Second {
		t.Fatalf("waits = %v", waits)
	}

	// A refused token is refreshed once; refused again, it is an AuthError.
	var seen []string
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer deny.Close()
	c = testClient(t, deny.URL)
	var ae *AuthError
	if _, err := c.get(context.Background(), deny.URL+"/x"); !errors.As(err, &ae) {
		t.Fatalf("always 401 = %v", err)
	}
	if len(seen) != 2 || seen[0] == seen[1] {
		t.Fatalf("expected two requests with different tokens, got %v", seen)
	}
}

func TestAPIClient_GivesUpAndDescribesRefusals(t *testing.T) {
	var calls int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer down.Close()
	if _, err := testClient(t, down.URL).get(context.Background(), down.URL+"/x"); err == nil || atomic.LoadInt32(&calls) != 4 {
		t.Fatalf("a failing server: %v after %d calls", err, calls)
	}

	nope := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":"accessDenied","message":"no\u001b[31m access"}}`))
	}))
	defer nope.Close()
	_, err := testClient(t, nope.URL).get(context.Background(), nope.URL+"/x")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "accessDenied") || strings.ContainsRune(err.Error(), 0x1b) {
		t.Fatalf("a refusal = %v", err)
	}
}

// --- Microsoft Graph ---------------------------------------------------------

func TestGraph_PathsFromParentsAcrossPages(t *testing.T) {
	var auth, query string
	var api *httptest.Server
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/drives/b!abc/root/delta":
			if r.URL.Query().Get("token") == "" {
				query = r.URL.RawQuery
				writeJSON(w, map[string]any{
					"value": []map[string]any{
						{"id": "ROOT", "name": "root", "root": map[string]any{}},
						{"id": "F1", "name": "Clienti", "folder": map[string]any{}, "parentReference": map[string]string{"id": "ROOT"}},
						{"id": "F2", "name": "Rossi", "folder": map[string]any{}, "parentReference": map[string]string{"id": "F1"}},
					},
					"@odata.nextLink": api.URL + "/drives/b!abc/root/delta?token=2",
				})
				return
			}
			writeJSON(w, map[string]any{
				"value": []map[string]any{
					{"id": "A", "name": "visura.pdf", "size": 1234, "lastModifiedDateTime": "2026-10-01T10:00:00Z", "webUrl": "https://acme.sharepoint.com/sites/s/Shared%20Documents/Clienti/Rossi/visura.pdf",
						"file": map[string]any{"mimeType": "application/pdf", "hashes": map[string]string{"quickXorHash": "QXH"}}, "parentReference": map[string]string{"id": "F2"}},
					{"id": "B", "name": "top.txt", "file": map[string]any{"hashes": map[string]string{"sha1Hash": "S1"}}, "parentReference": map[string]string{"id": "ROOT"}},
					{"id": "C", "name": "gone.pdf", "deleted": map[string]any{}, "parentReference": map[string]string{"id": "F1"}},
					{"id": "N", "name": "Notebook", "package": map[string]any{"type": "oneNote"}, "file": map[string]any{}, "parentReference": map[string]string{"id": "ROOT"}},
					// the same item again: the last one is its state
					{"id": "B", "name": "top-renamed.txt", "file": map[string]any{"hashes": map[string]string{"sha1Hash": "S2"}}, "parentReference": map[string]string{"id": "F1"}},
				},
				"@odata.deltaLink": api.URL + "/drives/b!abc/root/delta?token=end",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	g := &Graph{api: testClient(t, api.URL), base: api.URL, driveID: "b!abc"}
	s := g.Scan(context.Background())
	if !s.Complete() {
		t.Fatalf("errors: %v", s.Errors)
	}
	if got := paths(s); got != "/Clienti/Rossi/visura.pdf|A /Clienti/top-renamed.txt|B" {
		t.Fatalf("files = %s", got)
	}
	for _, f := range s.Files {
		if f.Key == "A" && (f.ProviderHash != "quickxor:QXH" || f.MIME != "application/pdf" || f.Size != 1234 || f.Mtime != "2026-10-01T10:00:00Z" || !strings.Contains(f.WebURL, "sharepoint.com")) {
			t.Fatalf("file A = %+v", f)
		}
		if f.Key == "B" && f.ProviderHash != "sha1:S2" {
			t.Fatalf("file B = %+v", f)
		}
	}
	if auth != "Bearer tok-1" || !strings.Contains(query, "%24top=200") && !strings.Contains(query, "$top=200") {
		t.Fatalf("auth %q, query %q", auth, query)
	}
}

func TestGraph_UsesTheUsersOwnDriveByDefaultAndRefusesLinksToOtherHosts(t *testing.T) {
	var paths []string
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the sign-in was sent to another host: %s", r.URL)
	}))
	defer evil.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		writeJSON(w, map[string]any{"value": []any{}, "@odata.nextLink": evil.URL + "/next"})
	}))
	defer api.Close()
	g := &Graph{api: testClient(t, api.URL), base: api.URL}
	s := g.Scan(context.Background())
	if s.Complete() || len(paths) != 1 || paths[0] != "/me/drive/root/delta" {
		t.Fatalf("complete=%v paths=%v errors=%v", s.Complete(), paths, s.Errors)
	}
}

func TestGraph_AFolderLoopMakesTheScanIncomplete(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"value": []map[string]any{
			{"id": "X", "name": "x", "folder": map[string]any{}, "parentReference": map[string]string{"id": "Y"}},
			{"id": "Y", "name": "y", "folder": map[string]any{}, "parentReference": map[string]string{"id": "X"}},
			{"id": "F", "name": "f.pdf", "file": map[string]any{}, "parentReference": map[string]string{"id": "X"}},
		}, "@odata.deltaLink": "x"})
	}))
	defer api.Close()
	g := &Graph{api: testClient(t, api.URL), base: api.URL}
	if s := g.Scan(context.Background()); s.Complete() || len(s.Files) != 0 {
		t.Fatalf("a loop: complete=%v files=%v", s.Complete(), s.Files)
	}
}

func TestGraph_FindsASharePointLibrary(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sites/acme.sharepoint.com:/sites/Studio Rossi":
			writeJSON(w, map[string]string{"id": "acme.sharepoint.com,guid1,guid2"})
		case "/sites/acme.sharepoint.com,guid1,guid2/drives":
			writeJSON(w, map[string]any{"value": []map[string]string{
				{"id": "b!sys", "name": "Appdata", "driveType": "system"},
				{"id": "b!docs", "name": "Documents", "driveType": "documentLibrary"},
				{"id": "b!cli", "name": "Clienti", "driveType": "documentLibrary"},
			}})
		case "/me/drive":
			writeJSON(w, map[string]string{"id": "b!mine"})
		default:
			t.Errorf("unexpected %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	g := &Graph{api: testClient(t, api.URL), base: api.URL}
	ctx := context.Background()
	if id, name, err := g.Library(ctx, "https://acme.sharepoint.com/sites/Studio Rossi/", "clienti"); err != nil || id != "b!cli" || name != "Clienti" {
		t.Fatalf("named library = %q %q %v", id, name, err)
	}
	if id, _, err := g.Library(ctx, "https://acme.sharepoint.com/sites/Studio Rossi", ""); err != nil || id != "b!docs" {
		t.Fatalf("default library = %q %v", id, err)
	}
	if _, _, err := g.Library(ctx, "https://acme.sharepoint.com/sites/Studio Rossi", "Nope"); err == nil || !strings.Contains(err.Error(), "Documents, Clienti") {
		t.Fatalf("missing library = %v", err)
	}
	if _, _, err := g.Library(ctx, "http://acme.sharepoint.com/sites/x", ""); err == nil {
		t.Fatal("an http site address")
	}
	if id, err := g.UserDrive(ctx); err != nil || id != "b!mine" {
		t.Fatalf("user drive = %q %v", id, err)
	}
}

// --- Google Drive ------------------------------------------------------------

func TestDrive_PathsFromParentsAndSharedFiles(t *testing.T) {
	var queries []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/files/root":
			writeJSON(w, map[string]string{"id": "ROOT"})
		case "/files":
			queries = append(queries, r.URL.RawQuery)
			if r.URL.Query().Get("pageToken") == "" {
				writeJSON(w, map[string]any{"files": []map[string]any{
					{"id": "d1", "name": "Clienti", "mimeType": driveFolder, "parents": []string{"ROOT"}},
					{"id": "d2", "name": "Rossi", "mimeType": driveFolder, "parents": []string{"d1"}},
					{"id": "s1", "name": "Condivisa", "mimeType": driveFolder, "parents": []string{"someone-elses-root"}},
				}, "nextPageToken": "P2"})
				return
			}
			writeJSON(w, map[string]any{"files": []map[string]any{
				{"id": "f1", "name": "visura.pdf", "mimeType": "application/pdf", "size": "2048", "modifiedTime": "2026-10-01T10:00:00.000Z",
					"md5Checksum": "abc", "parents": []string{"d2"}, "webViewLink": "https://drive.google.com/file/d/f1/view?usp=drivesdk"},
				{"id": "f2", "name": "Budget", "mimeType": "application/vnd.google-apps.spreadsheet", "parents": []string{"ROOT"}},
				{"id": "f3", "name": "dentro.pdf", "mimeType": "application/pdf", "parents": []string{"s1"}},
				{"id": "f4", "name": "regalo.pdf", "mimeType": "application/pdf"},
				{"id": "f5", "name": "scorciatoia", "mimeType": driveShortcut, "parents": []string{"ROOT"}},
				{"id": "f6", "name": "condiviso.pdf", "mimeType": "application/pdf", "parents": []string{"d2"}, "webViewLink": "https://drive.google.com/file/d/f6/view?usp=sharing"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	d := &Drive{api: testClient(t, api.URL), base: api.URL}
	s := d.Scan(context.Background())
	if !s.Complete() {
		t.Fatalf("errors: %v", s.Errors)
	}
	want := "/Budget|f2 /Clienti/Rossi/condiviso.pdf|f6 /Clienti/Rossi/visura.pdf|f1 /Shared with me/Condivisa/dentro.pdf|f3 /Shared with me/regalo.pdf|f4"
	if got := paths(s); got != want {
		t.Fatalf("files =\n%s\nwant\n%s", got, want)
	}
	byKey := map[string]string{}
	for _, f := range s.Files {
		byKey[f.Key] = f.ProviderHash + "|" + f.WebURL
		if f.Key == "f1" && (f.Size != 2048 || f.Mtime != "2026-10-01T10:00:00.000Z") {
			t.Fatalf("f1 = %+v", f)
		}
	}
	if byKey["f1"] != "md5:abc|https://drive.google.com/file/d/f1/view?usp=drivesdk" {
		t.Fatalf("Drive's own page for a file is kept: %q", byKey["f1"])
	}
	if byKey["f6"] != "|" {
		t.Fatalf("a sharing link was kept: %q", byKey["f6"])
	}
	if len(queries) != 2 || !strings.Contains(queries[0], "trashed+%3D+false") {
		t.Fatalf("queries = %v", queries)
	}
}

func TestDrive_AnErrorMidwayMakesTheScanIncomplete(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/files/root":
			writeJSON(w, map[string]string{"id": "ROOT"})
		case r.URL.Query().Get("pageToken") == "":
			writeJSON(w, map[string]any{"files": []map[string]any{{"id": "a", "name": "a.pdf", "parents": []string{"ROOT"}}}, "nextPageToken": "P2"})
		default:
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"code":403,"message":"rate"}}`))
		}
	}))
	defer api.Close()
	s := (&Drive{api: testClient(t, api.URL), base: api.URL}).Scan(context.Background())
	if s.Complete() {
		t.Fatal("a failed second page left a complete scan: its missing files would be marked gone")
	}
}

// --- Dropbox -----------------------------------------------------------------

func TestDropbox_RecursiveListingByCursor(t *testing.T) {
	var bodies []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		bodies = append(bodies, r.URL.Path+" "+string(buf[:n]))
		switch r.URL.Path {
		case "/files/list_folder":
			writeJSON(w, map[string]any{"entries": []map[string]any{
				{".tag": "folder", "id": "id:d", "name": "Clienti", "path_display": "/Clienti"},
				{".tag": "file", "id": "id:1", "name": "Visura Rossi.pdf", "path_display": "/Clienti/Rossi/Visura Rossi.pdf", "size": 99,
					"server_modified": "2026-10-01T10:00:00Z", "content_hash": "ch1"},
			}, "cursor": "C1", "has_more": true})
		case "/files/list_folder/continue":
			writeJSON(w, map[string]any{"entries": []map[string]any{
				{".tag": "file", "id": "id:2", "name": "nota.txt", "path_display": "/nota.txt", "size": 1, "server_modified": "2026-10-02T10:00:00Z"},
				{".tag": "deleted", "name": "x", "path_display": "/x"},
			}, "cursor": "C2", "has_more": false})
		case "/users/get_current_account":
			if len(strings.TrimSpace(string(buf[:n]))) != 0 {
				t.Errorf("get_current_account takes no body, got %q", buf[:n])
			}
			writeJSON(w, map[string]string{"email": "liam@example.it"})
		}
	}))
	defer api.Close()
	d := &Dropbox{api: testClient(t, api.URL), base: api.URL}
	s := d.Scan(context.Background())
	if !s.Complete() {
		t.Fatalf("errors: %v", s.Errors)
	}
	if got := paths(s); got != "/Clienti/Rossi/Visura Rossi.pdf|id:1 /nota.txt|id:2" {
		t.Fatalf("files = %s", got)
	}
	for _, f := range s.Files {
		if f.Key == "id:1" && (f.ProviderHash != "dropbox:ch1" || f.WebURL != "https://www.dropbox.com/home/Clienti/Rossi/Visura%20Rossi.pdf") {
			t.Fatalf("file 1 = %+v", f)
		}
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], `"recursive":true`) || !strings.Contains(bodies[0], `"include_deleted":false`) || !strings.Contains(bodies[1], `"cursor":"C1"`) {
		t.Fatalf("requests = %v", bodies)
	}
	if who := d.Who(context.Background()); who != "liam@example.it" {
		t.Fatalf("who = %q", who)
	}
}

func TestDropbox_AFailedPageMakesTheScanIncomplete(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/list_folder" {
			writeJSON(w, map[string]any{"entries": []any{}, "cursor": "C", "has_more": true})
			return
		}
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error_summary":"reset/..","error":{".tag":"reset"}}`))
	}))
	defer api.Close()
	s := (&Dropbox{api: testClient(t, api.URL), base: api.URL}).Scan(context.Background())
	if s.Complete() || !strings.Contains(fmt.Sprint(s.Errors), "409") {
		t.Fatalf("complete=%v errors=%v", s.Complete(), s.Errors)
	}
}

func TestScanAddKeepsAFileWhoseLinkTheVaultRefuses(t *testing.T) {
	s := &Scan{}
	s.add(fileObs("k1", "/a.pdf", "a.pdf", "https://1drv.ms/b/s!share"))
	s.add(fileObs("k2", "/b.pdf", "bad\x07name.pdf", ""))
	if len(s.Files) != 1 || s.Files[0].WebURL != "" || s.Files[0].Key != "k1" || s.Skipped != 1 {
		t.Fatalf("files=%+v skipped=%d", s.Files, s.Skipped)
	}
}

func fileObs(key, path, name, webURL string) vault.FileObservation {
	return vault.FileObservation{Key: key, Path: path, Name: name, WebURL: webURL}
}
