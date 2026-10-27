package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func signingPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	var offs []int
	for i, o := range objs {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

func signaturePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 200, 80))
	for x := 10; x < 190; x++ {
		img.Set(x, 40+(x%20)-10, color.NRGBA{0, 0, 150, 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// signingEnv is a test vault with a signature on file and an agent token that
// may file signing requests.
func signingEnv(t *testing.T, scope, access string) (*testEnv, string) {
	t.Helper()
	env := setup(t)
	if _, err := env.vault.AddDocument(vault.SignatureCategory, "firma.png", "image/png", signaturePNG(t), ""); err != nil {
		t.Fatal(err)
	}
	agent, err := env.vault.CreateServiceToken("tari-agent", scope, time.Hour, "", access)
	if err != nil {
		t.Fatal(err)
	}
	return env, agent
}

// call sends one request to the server; socket marks it as arriving on the
// Unix socket (the listener sets that on real connections).
func (e *testEnv) call(method, path, token string, body any, socket bool) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := newTestRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req = req.WithContext(context.WithValue(req.Context(), socketKey{}, socket))
	w := httptest.NewRecorder()
	e.server.handler.ServeHTTP(w, req)
	return w
}

func signRequestBody() map[string]any {
	return map[string]any{
		"name": "mod-1.pdf", "purpose": "tari-2026", "pdf": signingPDF(),
		"placements": []map[string]any{{"page": 1, "rect": []float64{100, 40, 190, 90}, "date_rect": []float64{10, 70, 90, 90}}},
	}
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return out
}

func TestSigningEndToEnd(t *testing.T) {
	env, agent := signingEnv(t, "firma_autografa.signing", vault.AccessAppend)

	w := env.call("POST", "/vault/signing", agent, signRequestBody(), false)
	if w.Code != 200 {
		t.Fatalf("request: %d %s", w.Code, w.Body.String())
	}
	info := decode[vault.SigningInfo](t, w)
	id := info.ID
	if info.Requester != "tari-agent" || info.Status != vault.SigningPending {
		t.Fatalf("info = %+v", info)
	}

	// The agent sees its request and cannot read the file or approve.
	if w := env.call("GET", "/vault/signing/"+id, agent, nil, false); w.Code != 200 {
		t.Fatalf("agent status: %d %s", w.Code, w.Body.String())
	}
	if w := env.call("GET", "/vault/signing/"+id+"/candidate", agent, nil, true); w.Code != 403 {
		t.Fatalf("agent read the signed file: %d", w.Code)
	}
	if w := env.call("POST", "/vault/signing/"+id+"/approve", agent, map[string]string{"sha256": info.SHA256}, true); w.Code != 403 {
		t.Fatalf("agent approved its own request: %d %s", w.Code, w.Body.String())
	}

	// The user trades the password for a sign grant, over the socket only.
	grantBody := map[string]string{"password": testPassword, "sign": id}
	if w := env.call("POST", "/vault/session", "", grantBody, false); w.Code != 403 || !strings.Contains(w.Body.String(), "socket_required") {
		t.Fatalf("sign grant over TCP: %d %s", w.Code, w.Body.String())
	}
	if w := env.call("POST", "/vault/session", "", map[string]string{"password": "wrong", "sign": id}, true); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	w = env.call("POST", "/vault/session", "", grantBody, true)
	if w.Code != 200 {
		t.Fatalf("sign grant: %d %s", w.Code, w.Body.String())
	}
	grant := decode[map[string]string](t, w)["token"]

	// The grant opens its request's file and approval, and nothing else.
	for _, c := range []struct{ method, path string }{
		{"GET", "/vault/fields"}, {"GET", "/vault/documents"}, {"GET", "/vault/signing"},
		{"GET", "/vault/signing/" + id}, {"DELETE", "/vault/signing/" + id},
		{"POST", "/vault/tokens/service"}, {"GET", "/vault/audit"},
		{"GET", "/vault/signing/" + strings.Repeat("a", 32) + "/candidate"},
	} {
		if w := env.call(c.method, c.path, grant, map[string]string{}, true); w.Code != 403 {
			t.Errorf("%s %s with a sign grant: %d, want 403", c.method, c.path, w.Code)
		}
	}
	w = env.call("GET", "/vault/signing/"+id+"/candidate", grant, nil, true)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("candidate: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	sum := sha256.Sum256(w.Body.Bytes())
	got := hex.EncodeToString(sum[:])
	if got != info.SHA256 || w.Header().Get("X-Signing-Sha256") != got {
		t.Fatalf("the file's hash %s is not the request's %s", got, info.SHA256)
	}

	if w := env.call("POST", "/vault/signing/"+id+"/approve", grant, map[string]string{"sha256": strings.Repeat("0", 64)}, true); w.Code != 400 {
		t.Fatalf("wrong hash: %d %s", w.Code, w.Body.String())
	}
	if w := env.call("POST", "/vault/signing/"+id+"/approve", grant, map[string]string{"sha256": got}, true); w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	// The grant died with the approval.
	if w := env.call("GET", "/vault/signing/"+id+"/candidate", grant, nil, true); w.Code != 401 {
		t.Fatalf("grant after approval: %d", w.Code)
	}
	st := decode[vault.SigningInfo](t, env.call("GET", "/vault/signing/"+id, agent, nil, false))
	if st.Status != vault.SigningApproved {
		t.Fatalf("agent sees status %q", st.Status)
	}
}

func TestSigningSessionCanReviewAndRefuse(t *testing.T) {
	env, agent := signingEnv(t, "*", vault.AccessWrite)
	info := decode[vault.SigningInfo](t, env.call("POST", "/vault/signing", agent, signRequestBody(), false))

	if w := env.call("GET", "/vault/signing", env.token, nil, false); w.Code != 200 || !strings.Contains(w.Body.String(), info.ID) {
		t.Fatalf("session list: %d %s", w.Code, w.Body.String())
	}
	if w := env.call("GET", "/vault/signing/"+info.ID+"/candidate", env.token, nil, false); w.Code != 200 {
		t.Fatalf("session candidate: %d", w.Code)
	}
	if w := env.call("DELETE", "/vault/signing/"+info.ID, env.token, nil, false); w.Code != 200 {
		t.Fatalf("session reject: %d", w.Code)
	}
	if w := env.call("GET", "/vault/signing/"+info.ID+"/candidate", env.token, nil, false); w.Code != 409 {
		t.Fatalf("candidate of a rejected request: %d", w.Code)
	}
}

func TestSigningTokenLimits(t *testing.T) {
	// Read-only and out-of-scope tokens can't file requests.
	env, readonly := signingEnv(t, "firma_autografa.signing", vault.AccessRead)
	if w := env.call("POST", "/vault/signing", readonly, signRequestBody(), false); w.Code != 403 {
		t.Fatalf("read-only token: %d", w.Code)
	}
	other, _ := env.vault.CreateServiceToken("other", "identity.*", time.Hour, "", vault.AccessWrite)
	if w := env.call("POST", "/vault/signing", other, signRequestBody(), false); w.Code != 403 {
		t.Fatalf("token without the scope: %d", w.Code)
	}
	if w := env.call("POST", "/vault/signing", "", signRequestBody(), false); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}

	// Another agent can't see, withdraw or approve a request.
	mine, _ := env.vault.CreateServiceToken("mine", "firma_autografa.signing", time.Hour, "", vault.AccessAppend)
	theirs, _ := env.vault.CreateServiceToken("theirs", "firma_autografa.signing", time.Hour, "", vault.AccessAppend)
	info := decode[vault.SigningInfo](t, env.call("POST", "/vault/signing", mine, signRequestBody(), false))
	if w := env.call("GET", "/vault/signing/"+info.ID, theirs, nil, false); w.Code != 404 {
		t.Fatalf("other agent's status: %d", w.Code)
	}
	if w := env.call("DELETE", "/vault/signing/"+info.ID, theirs, nil, false); w.Code != 404 {
		t.Fatalf("other agent withdrew it: %d", w.Code)
	}
	if l := decode[[]vault.SigningInfo](t, env.call("GET", "/vault/signing", theirs, nil, false)); len(l) != 0 {
		t.Fatalf("other agent listed %d", len(l))
	}
	if w := env.call("DELETE", "/vault/signing/"+info.ID, mine, nil, false); w.Code != 200 {
		t.Fatalf("withdraw own: %d", w.Code)
	}
}

func TestSigningRequestErrors(t *testing.T) {
	env, agent := signingEnv(t, "firma_autografa.signing", vault.AccessAppend)
	bad := signRequestBody()
	bad["pdf"] = []byte("not a pdf")
	if w := env.call("POST", "/vault/signing", agent, bad, false); w.Code != 400 {
		t.Fatalf("bad pdf: %d %s", w.Code, w.Body.String())
	}
	// No signature on file.
	empty := setup(t)
	tok, _ := empty.vault.CreateServiceToken("a", "firma_autografa.signing", time.Hour, "", vault.AccessAppend)
	if w := empty.call("POST", "/vault/signing", tok, signRequestBody(), false); w.Code != 409 || !strings.Contains(w.Body.String(), "no_signature") {
		t.Fatalf("no signature: %d %s", w.Code, w.Body.String())
	}
	// A non-JSON body is refused like any credential endpoint's.
	req := newTestRequest("POST", "/vault/signing", strings.NewReader("x"))
	req.Header.Set("Authorization", "Bearer "+agent)
	rec := httptest.NewRecorder()
	env.server.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("no content type: %d", rec.Code)
	}
}

func TestSigningSummaryIsSocketOnlyAndHoldsNoFile(t *testing.T) {
	env, agent := signingEnv(t, "firma_autografa.signing", vault.AccessAppend)
	info := decode[vault.SigningInfo](t, env.call("POST", "/vault/signing", agent, signRequestBody(), false))

	if w := env.call("GET", "/vault/signing/"+info.ID+"/summary", "", nil, false); w.Code != 404 {
		t.Fatalf("summary over TCP: %d", w.Code)
	}
	w := env.call("GET", "/vault/signing/"+info.ID+"/summary", "", nil, true)
	if w.Code != 200 {
		t.Fatalf("summary: %d %s", w.Code, w.Body.String())
	}
	sum := decode[map[string]any](t, w)
	if sum["hash8"] != info.SHA256[:8] || sum["name"] != "mod-1.pdf" || sum["pages"] != float64(1) {
		t.Fatalf("summary = %v", sum)
	}
	if strings.Contains(w.Body.String(), info.SHA256) || strings.Contains(w.Body.String(), "pdf\":") {
		t.Fatalf("summary leaks more than the prompt needs: %s", w.Body.String())
	}
}

// Two tokens that share a consumer name are two agents: neither sees, lists or
// withdraws the other's requests.
func TestSigningTokensWithOneNameAreSeparate(t *testing.T) {
	env, _ := signingEnv(t, "firma_autografa.signing", vault.AccessAppend)
	a, _ := env.vault.CreateServiceToken("same-name", "firma_autografa.signing", time.Hour, "", vault.AccessAppend)
	b, _ := env.vault.CreateServiceToken("same-name", "firma_autografa.signing", time.Hour, "", vault.AccessAppend)
	info := decode[vault.SigningInfo](t, env.call("POST", "/vault/signing", a, signRequestBody(), false))
	if w := env.call("GET", "/vault/signing/"+info.ID, b, nil, false); w.Code != 404 {
		t.Fatalf("the other token saw it: %d", w.Code)
	}
	if l := decode[[]vault.SigningInfo](t, env.call("GET", "/vault/signing", b, nil, false)); len(l) != 0 {
		t.Fatalf("the other token listed %d", len(l))
	}
	if w := env.call("DELETE", "/vault/signing/"+info.ID, b, nil, false); w.Code != 404 {
		t.Fatalf("the other token withdrew it: %d", w.Code)
	}
	if w := env.call("GET", "/vault/signing/"+info.ID, a, nil, false); w.Code != 200 {
		t.Fatalf("the filing token lost it: %d", w.Code)
	}
	// The user (a session) sees all of them, whoever filed.
	if l := decode[[]vault.SigningInfo](t, env.call("GET", "/vault/signing", env.token, nil, false)); len(l) != 1 {
		t.Fatalf("the user saw %d", len(l))
	}
}

// The sixth request in an hour from one token, and the fourth waiting, are 429.
func TestSigningPerTokenLimitsAreTooManyRequests(t *testing.T) {
	env, agent := signingEnv(t, "firma_autografa.signing", vault.AccessAppend)
	for i := 0; i < vault.MaxPendingPerRequester; i++ {
		if w := env.call("POST", "/vault/signing", agent, signRequestBody(), false); w.Code != 200 {
			t.Fatalf("request %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if w := env.call("POST", "/vault/signing", agent, signRequestBody(), false); w.Code != 429 {
		t.Fatalf("one token's fourth waiting request: %d %s", w.Code, w.Body.String())
	}
}
