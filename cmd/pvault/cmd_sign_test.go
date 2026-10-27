package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func signTestPDF() []byte {
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

// signReviewSetup is a served vault with a signature on file and one request
// waiting. grant stands in for the prompt: it trades the password for a sign
// grant over the real socket, as signGrant does after typing it.
func signReviewSetup(t *testing.T) (*vault.Vault, *vault.SigningInfo, func(signSummary) (string, error)) {
	t.Helper()
	v, _ := touchIDVault(t, nil)
	verifiedKind = ""
	t.Cleanup(func() { verifiedKind = "" })
	img := image.NewNRGBA(image.Rect(0, 0, 200, 80))
	for x := 10; x < 190; x++ {
		img.Set(x, 40+(x%20)-10, color.NRGBA{0, 0, 150, 255})
	}
	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, img)
	if _, err := v.AddDocument(vault.SignatureCategory, "firma.png", "image/png", pngBytes.Bytes(), ""); err != nil {
		t.Fatal(err)
	}
	info, err := v.RequestSigning(vault.SigningInput{
		Requester: "agent", Name: "mod-1.pdf", Purpose: "tari", PDF: signTestPDF(),
		Placements: []vault.Placement{{Page: 1, Rect: [4]float64{100, 40, 190, 90}, DateRect: &[4]float64{10, 70, 90, 90}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	grant := func(s signSummary) (string, error) {
		resp, err := credentialRequest("POST", "/vault/session", map[string]string{"password": "test-password", "sign": s.ID})
		if err != nil {
			return "", err
		}
		var out struct {
			Token string `json:"token"`
		}
		if err := apiResult(resp, &out); err != nil {
			return "", err
		}
		return out.Token, nil
	}
	return v, info, grant
}

func TestSignReview_ApprovesAndSaves(t *testing.T) {
	v, info, grant := signReviewSetup(t)
	out := filepath.Join(t.TempDir(), "signed.pdf")
	var viewed string
	var shown []byte
	var w bytes.Buffer
	r := signReview{
		id: info.ID, out: out, w: &w, grant: grant,
		in: strings.NewReader(info.SHA256[:8] + "\n"),
		view: func(p string) {
			viewed = p
			shown, _ = os.ReadFile(p)
		},
	}
	if err := r.run(); err != nil {
		t.Fatalf("%v\n%s", err, w.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(got)
	if hex.EncodeToString(sum[:]) != info.SHA256 {
		t.Fatal("the saved file is not the one the request announced")
	}
	if !bytes.Equal(got, shown) {
		t.Fatal("the user reviewed a different file from the one saved")
	}
	if fi, _ := os.Stat(out); fi.Mode().Perm() != 0o600 {
		t.Fatalf("saved with mode %v", fi.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Dir(viewed)); !os.IsNotExist(err) {
		t.Fatal("the review copy was left behind")
	}
	if st, _ := v.SigningStatus(info.ID, ""); st.Status != vault.SigningApproved {
		t.Fatalf("status = %s", st.Status)
	}
	if !strings.Contains(w.String(), info.SHA256) {
		t.Fatalf("the full hash was not shown:\n%s", w.String())
	}
}

func TestSignReview_SignsNothingWithoutTheTypedHash(t *testing.T) {
	for name, typed := range map[string]string{"enter": "\n", "wrong": "deadbeef\n", "eof": ""} {
		t.Run(name, func(t *testing.T) {
			v, info, grant := signReviewSetup(t)
			out := filepath.Join(t.TempDir(), "signed.pdf")
			r := signReview{id: info.ID, out: out, w: &bytes.Buffer{}, grant: grant, in: strings.NewReader(typed), view: func(string) {}}
			if err := r.run(); err == nil || !strings.Contains(err.Error(), "not signed") {
				t.Fatalf("run = %v", err)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("a file was saved")
			}
			if st, _ := v.SigningStatus(info.ID, ""); st.Status != vault.SigningPending {
				t.Fatalf("status = %s", st.Status)
			}
		})
	}
}

func TestSignReview_ChecksBeforeAsking(t *testing.T) {
	_, info, grant := signReviewSetup(t)
	asked := false
	spy := func(s signSummary) (string, error) { asked = true; return grant(s) }

	existing := filepath.Join(t.TempDir(), "taken.pdf")
	os.WriteFile(existing, []byte("x"), 0o600)
	r := signReview{id: info.ID, out: existing, w: &bytes.Buffer{}, grant: spy, in: strings.NewReader(""), view: func(string) {}}
	if err := r.run(); err == nil || !strings.Contains(err.Error(), "already exists") || asked {
		t.Fatalf("existing output: err=%v asked=%v", err, asked)
	}
	r = signReview{id: "../../etc", w: &bytes.Buffer{}, grant: spy, in: strings.NewReader(""), view: func(string) {}}
	if err := r.run(); err == nil || asked {
		t.Fatalf("bad id: err=%v asked=%v", err, asked)
	}

	// Once decided, a request can't be reviewed again.
	runCmd(t, cmdSign, "sign", "reject", info.ID)
	r = signReview{id: info.ID, out: filepath.Join(t.TempDir(), "o.pdf"), w: &bytes.Buffer{}, grant: spy, in: strings.NewReader(""), view: func(string) {}}
	if err := r.run(); err == nil || !strings.Contains(err.Error(), "rejected") || asked {
		t.Fatalf("rejected request: err=%v asked=%v", err, asked)
	}
}

func TestSignList(t *testing.T) {
	_, info, _ := signReviewSetup(t)
	out := runCmd(t, cmdSign, "sign", "list")
	if !strings.Contains(out, info.ID) || !strings.Contains(out, "mod-1.pdf") || !strings.Contains(out, "pending") {
		t.Fatalf("list:\n%s", out)
	}
}

func TestSignReview_CantSaveIsKnownBeforeAsking(t *testing.T) {
	_, info, grant := signReviewSetup(t)
	asked := false
	spy := func(s signSummary) (string, error) { asked = true; return grant(s) }
	missing := filepath.Join(t.TempDir(), "no", "such", "dir", "x.pdf")
	r := signReview{id: info.ID, out: missing, w: &bytes.Buffer{}, grant: spy, in: strings.NewReader(""), view: func(string) {}}
	if err := r.run(); err == nil || !strings.Contains(err.Error(), "can't save") || asked {
		t.Fatalf("missing folder: err=%v asked=%v", err, asked)
	}
	long := filepath.Join(t.TempDir(), strings.Repeat("x", 300)+".pdf")
	r.out = long
	if err := r.run(); err == nil || asked {
		t.Fatalf("name too long: err=%v asked=%v", err, asked)
	}
}

func TestSignReview_LeavesNothingBehindWhenRefused(t *testing.T) {
	_, info, grant := signReviewSetup(t)
	out := filepath.Join(t.TempDir(), "signed.pdf")
	var viewed string
	r := signReview{id: info.ID, out: out, w: &bytes.Buffer{}, grant: grant, in: strings.NewReader("\n"),
		view: func(p string) { viewed = p }}
	if err := r.run(); err == nil {
		t.Fatal("refusal returned no error")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("the placeholder for the result was left behind")
	}
	if _, err := os.Stat(filepath.Dir(viewed)); !os.IsNotExist(err) {
		t.Fatal("the review copy of the form was left behind")
	}
}

func TestPlainTextAndDefaultName(t *testing.T) {
	for in, want := range map[string]string{
		"mod-1.pdf":            "mod-1.pdf",
		"a‮b":                  "a?b",
		"x\ny":                 "x?y",
		"\x1b[31mred":          "??31mred",
		strings.Repeat("é", 5): "ééééé",
	} {
		if got := plainText(in, 60); got != want {
			t.Errorf("plainText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := plainText(strings.Repeat("a", 70), 60); got != strings.Repeat("a", 60)+"…" {
		t.Errorf("long text: %q", got)
	}
	for _, name := range []string{strings.Repeat("n", 245) + ".pdf", "../../etc/passwd", "..hidden.pdf", "‮.pdf", ".pdf", "tari 2026 (mod-1).pdf"} {
		got := defaultSignedName(name)
		if len(got) > 255 || strings.ContainsAny(got, "/\\\x00?") || strings.HasPrefix(got, ".") || !strings.HasSuffix(got, "-firmato.pdf") {
			t.Errorf("defaultSignedName(%q) = %q", name, got)
		}
	}
	if got := defaultSignedName("tari 2026 (mod-1).pdf"); got != "tari 2026 (mod-1)-firmato.pdf" {
		t.Errorf("a plain name changed: %q", got)
	}
}
