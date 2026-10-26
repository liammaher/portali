package api

import (
	"bytes"
	"encoding/json"
	"mime"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func (e *testEnv) upload(t *testing.T, token, query, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := newTestRequest("POST", "/vault/documents?"+query, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	e.server.handler.ServeHTTP(w, req)
	return w
}

func TestDocuments_UploadDownloadDelete(t *testing.T) {
	env := setup(t)
	content := []byte("<html><script>alert(1)</script></html>")
	w := env.upload(t, env.token, "category=passaporto&name=scan.html", "text/html", content)
	if w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	var doc vault.DocumentInfo
	json.NewDecoder(w.Body).Decode(&doc)

	w = env.doRequest(t, "GET", "/vault/documents/"+doc.ID, nil, true)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), content) {
		t.Fatalf("download: %d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}
	if w.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Error("download must be sandboxed")
	}

	w = env.doRequest(t, "GET", "/vault/documents", nil, true)
	var list []vault.DocumentInfo
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 || list[0].Name != "scan.html" {
		t.Fatalf("list: %+v", list)
	}

	if w := env.doRequest(t, "DELETE", "/vault/documents/"+doc.ID, nil, true); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := env.doRequest(t, "GET", "/vault/documents/"+doc.ID, nil, true); w.Code != 404 {
		t.Fatalf("after delete: %d", w.Code)
	}
}

func TestDocuments_ServiceTokenScopeAndTier(t *testing.T) {
	env := setup(t)
	var pass, lic vault.DocumentInfo
	json.NewDecoder(env.upload(t, env.token, "category=passaporto&name=p.pdf", "application/pdf", []byte("p")).Body).Decode(&pass)
	json.NewDecoder(env.upload(t, env.token, "category=patente&name=l.pdf&sensitivity=critical", "application/pdf", []byte("l")).Body).Decode(&lic)

	token := createTokenWithAccess(t, env, "passaporto.*,patente.*", "append") // default cap: sensitive
	if w := env.doRequestWithToken(t, "GET", "/vault/documents/"+pass.ID, nil, token); w.Code != 200 {
		t.Fatalf("in-scope sensitive doc: %d", w.Code)
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/documents/"+lic.ID, nil, token); w.Code != 403 {
		t.Fatalf("critical doc with sensitive cap: %d", w.Code)
	}
	w := env.doRequestWithToken(t, "GET", "/vault/documents", nil, token)
	var list []vault.DocumentInfo
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 || list[0].ID != pass.ID {
		t.Fatalf("list should hide the critical doc: %+v", list)
	}

	other := createScopedToken(t, env, "other", "identity.*")
	if w := env.doRequestWithToken(t, "GET", "/vault/documents/"+pass.ID, nil, other); w.Code != 403 {
		t.Fatalf("out-of-scope doc: %d", w.Code)
	}
	if w := env.upload(t, other, "category=passaporto&name=x.pdf", "application/pdf", []byte("x")); w.Code != 403 {
		t.Fatalf("out-of-scope upload: %d", w.Code)
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/documents/"+pass.ID, nil, other); w.Code != 403 {
		t.Fatalf("out-of-scope delete: %d", w.Code)
	}
	// An in-scope agent can archive new documents (e.g. a downloaded receipt).
	if w := env.upload(t, token, "category=passaporto&name=receipt.pdf", "application/pdf", []byte("r")); w.Code != 200 {
		t.Fatalf("agent upload: %d %s", w.Code, w.Body.String())
	}
}

func TestDocuments_TooLargeRejected(t *testing.T) {
	env := setup(t)
	w := env.upload(t, env.token, "category=passaporto&name=big.pdf", "application/pdf", make([]byte, vault.MaxDocumentSize+1))
	if w.Code != 413 {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
	// JSON endpoints keep the 1 MB limit.
	big := strings.Repeat("x", 2<<20)
	if w := env.doRequest(t, "PUT", "/vault/fields/identity.note", map[string]string{"value": big}, true); w.Code == 200 {
		t.Fatal("2 MB JSON body should be rejected")
	}
}

func TestExpiring_API(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/passaporto.data_scadenza", map[string]string{"value": "01/01/2000"}, true)
	env.doRequest(t, "PUT", "/vault/fields/patente.data_scadenza", map[string]string{"value": "2000-06-01"}, true)

	w := env.doRequest(t, "GET", "/vault/expiring?days=30", nil, true)
	var items []vault.ExpiryInfo
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 2 || !items[0].Expired || items[0].ExpiresOn != "2000-01-01" {
		t.Fatalf("expiring: %+v", items)
	}

	token := createScopedToken(t, env, "agent", "patente.*")
	w = env.doRequestWithToken(t, "GET", "/vault/expiring", nil, token)
	items = nil
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 1 || items[0].Document != "patente" {
		t.Fatalf("scoped expiring: %+v", items)
	}
	if w := env.doRequest(t, "GET", "/vault/expiring?days=-1", nil, true); w.Code != 400 {
		t.Fatalf("negative days: %d", w.Code)
	}
	if w := env.doRequest(t, "PUT", "/vault/fields/patente.data_scadenza", map[string]string{"value": "31/02/2030"}, true); w.Code != 400 {
		t.Fatalf("impossible date: %d", w.Code)
	}
}

func TestDocuments_FilenameEncodingAndMeta(t *testing.T) {
	env := setup(t)
	for _, name := range []string{"permesso;scan.pdf", "Mario's ID (fronte).pdf", "carta d'identità.pdf"} {
		var doc vault.DocumentInfo
		json.NewDecoder(env.upload(t, env.token, "category=passaporto&name="+url.QueryEscape(name), "application/pdf", []byte("x")).Body).Decode(&doc)
		w := env.doRequest(t, "GET", "/vault/documents/"+doc.ID, nil, true)
		_, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
		if err != nil || params["filename"] != name {
			t.Errorf("filename %q round-tripped as %q (%v)", name, params["filename"], err)
		}
	}
	if w := env.upload(t, env.token, "category=passaporto&name=..", "application/pdf", []byte("x")); w.Code != 400 {
		t.Errorf("name '..' accepted: %d", w.Code)
	}
}

func TestDocuments_ListCategoryFilter(t *testing.T) {
	env := setup(t)
	env.upload(t, env.token, "category=passaporto&name=p.pdf", "application/pdf", []byte("p"))
	env.upload(t, env.token, "category=patente&name=l.pdf", "application/pdf", []byte("l"))
	w := env.doRequest(t, "GET", "/vault/documents?category=patente", nil, true)
	var list []vault.DocumentInfo
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 || list[0].Category != "patente" {
		t.Fatalf("category filter: %+v", list)
	}
}

func TestDocuments_ConcurrentUploadsCapped(t *testing.T) {
	env := setup(t)
	for i := 0; i < maxConcurrentUploads; i++ {
		env.server.uploadSlots <- struct{}{}
	}
	if w := env.upload(t, env.token, "category=passaporto&name=p.pdf", "application/pdf", []byte("p")); w.Code != 429 {
		t.Fatalf("expected 429 with all upload slots busy, got %d", w.Code)
	}
	<-env.server.uploadSlots
	if w := env.upload(t, env.token, "category=passaporto&name=p.pdf", "application/pdf", []byte("p")); w.Code != 200 {
		t.Fatalf("expected 200 once a slot frees, got %d", w.Code)
	}
}

func TestDocuments_TokenCannotLowerTier(t *testing.T) {
	env := setup(t)
	token := createTokenWithAccess(t, env, "passaporto.*", "append")
	for _, tier := range []string{"public", "standard"} {
		w := env.upload(t, token, "category=passaporto&name=p.pdf&sensitivity="+tier, "application/pdf", []byte("p"))
		if w.Code != 403 {
			t.Fatalf("token upload at %s: %d %s", tier, w.Code, w.Body.String())
		}
		if _, c := parseErrorResponse(t, w); c != "session_required" {
			t.Fatalf("token upload at %s: constraint %q", tier, c)
		}
	}
	for _, tier := range []string{"", "sensitive", "critical"} {
		if w := env.upload(t, token, "category=passaporto&name=p.pdf&sensitivity="+tier, "application/pdf", []byte("p")); w.Code != 200 {
			t.Fatalf("token upload at %q: %d %s", tier, w.Code, w.Body.String())
		}
	}
	if w := env.upload(t, token, "category=passaporto&name=p.pdf&sensitivity=bogus", "application/pdf", []byte("p")); w.Code != 400 {
		t.Fatalf("invalid tier: %d %s", w.Code, w.Body.String())
	}
	// The user's own session still chooses any tier.
	if w := env.upload(t, env.token, "category=passaporto&name=p.pdf&sensitivity=public", "application/pdf", []byte("p")); w.Code != 200 {
		t.Fatalf("session upload at public: %d %s", w.Code, w.Body.String())
	}
}

// A credential-named category (cie_pin) defaults its documents to personal,
// so a token can't create one there any more than it can write a field.
func TestDocuments_TokenCannotUploadToCredentialCategory(t *testing.T) {
	env := setup(t)
	tok := createTokenWithAccess(t, env, "*", "append")
	for _, category := range []string{"cie_pin", "cie_puk", "spid"} {
		w := env.upload(t, tok, "category="+category+"&name=letter.pdf", "application/pdf", []byte("PIN"))
		if w.Code != 403 || !strings.Contains(w.Body.String(), "personal_only") {
			t.Errorf("upload to %s: %d %s, want 403 personal_only", category, w.Code, w.Body.String())
		}
	}
}
