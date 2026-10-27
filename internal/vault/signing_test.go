package vault

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func testSignaturePNG(t *testing.T) []byte {
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

// signingVault is an unlocked vault with a signature on file.
func signingVault(t *testing.T) (*Vault, string) {
	t.Helper()
	v, sk := tmpVault(t)
	if _, err := v.AddDocument(SignatureCategory, "firma.png", "image/png", testSignaturePNG(t), ""); err != nil {
		t.Fatal(err)
	}
	return v, sk
}

func signingInput() SigningInput {
	return SigningInput{
		Requester: "tari-agent", RequesterID: "tok-a", Name: "mod-1.pdf", Purpose: "tari-2026",
		PDF:        minimalPDF,
		Placements: []Placement{{Page: 1, Rect: [4]float64{100, 40, 190, 90}, DateRect: &[4]float64{10, 70, 90, 90}}},
	}
}

func hashOf(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestSigningRequestApproveFlow(t *testing.T) {
	v, _ := signingVault(t)
	info, err := v.RequestSigning(signingInput())
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != SigningPending || info.Pages != 1 || info.Signer != "io" || info.Requester != "tari-agent" {
		t.Fatalf("info = %+v", info)
	}
	_, file, err := v.SigningCandidate(info.ID, "session")
	if err != nil {
		t.Fatal(err)
	}
	if hashOf(file) != info.SHA256 {
		t.Fatal("the candidate's hash is not the one the request reports")
	}
	if bytes.Equal(file, minimalPDF) || len(file) <= len(minimalPDF) {
		t.Fatal("the candidate has no signature on it")
	}
	if _, err := api.ReadContext(t.Context(), bytes.NewReader(file), model.NewDefaultConfiguration()); err != nil {
		t.Fatalf("the signed file is not a readable PDF: %v", err)
	}

	if _, err := v.ApproveSigning(info.ID, strings.Repeat("0", 64), "session"); !errors.Is(err, ErrSigningHash) {
		t.Fatalf("a wrong hash approved: %v", err)
	}
	if st, _ := v.SigningStatus(info.ID, ""); st.Status != SigningPending {
		t.Fatalf("a refused approval changed the status to %s", st.Status)
	}
	done, err := v.ApproveSigning(info.ID, strings.ToUpper(info.SHA256), "session")
	if err != nil || done.Status != SigningApproved {
		t.Fatalf("approve = %+v, %v", done, err)
	}
	if _, err := v.ApproveSigning(info.ID, info.SHA256, "session"); !errors.Is(err, ErrSigningNotPending) {
		t.Fatalf("a second approval: %v", err)
	}
	if _, _, err := v.SigningCandidate(info.ID, "session"); !errors.Is(err, ErrSigningNotPending) {
		t.Fatalf("the file is still available after approval: %v", err)
	}

	log, _ := v.AuditLog(50)
	var actions []string
	for _, e := range log {
		if e.Scope == SigningScopeID {
			actions = append(actions, e.Action)
			if !strings.Contains(e.Purpose, info.SHA256) {
				t.Errorf("%s audit entry lacks the hash: %q", e.Action, e.Purpose)
			}
		}
	}
	sort.Strings(actions)
	if strings.Join(actions, ",") != "sign_approve,sign_refused,sign_request,sign_view" {
		t.Fatalf("audit actions = %v", actions)
	}
}

func TestSigningNeedsASignatureOnFile(t *testing.T) {
	v, _ := tmpVault(t)
	if _, err := v.RequestSigning(signingInput()); !errors.Is(err, ErrNoSignature) {
		t.Fatalf("got %v", err)
	}
}

func TestSigningInputIsChecked(t *testing.T) {
	v, _ := signingVault(t)
	cases := map[string]func(*SigningInput){
		"not a pdf":       func(in *SigningInput) { in.PDF = []byte("hello") },
		"empty":           func(in *SigningInput) { in.PDF = nil },
		"no boxes":        func(in *SigningInput) { in.Placements = nil },
		"page 2 of 1":     func(in *SigningInput) { in.Placements[0].Page = 2 },
		"page 0":          func(in *SigningInput) { in.Placements[0].Page = 0 },
		"off the page":    func(in *SigningInput) { in.Placements[0].Rect = [4]float64{100, 40, 400, 90} },
		"too small":       func(in *SigningInput) { in.Placements[0].Rect = [4]float64{100, 40, 105, 45} },
		"upside down":     func(in *SigningInput) { in.Placements[0].Rect = [4]float64{190, 90, 100, 40} },
		"date off page":   func(in *SigningInput) { in.Placements[0].DateRect = &[4]float64{10, 70, 90, 400} },
		"someone else":    func(in *SigningInput) { in.Signer = "leo" },
		"bad name":        func(in *SigningInput) { in.Name = "" },
		"long purpose":    func(in *SigningInput) { in.Purpose = strings.Repeat("x", 201) },
		"control in desc": func(in *SigningInput) { in.Purpose = "a\x00b" },
	}
	for name, mutate := range cases {
		in := signingInput()
		in.Placements = append([]Placement(nil), in.Placements...)
		mutate(&in)
		_, err := v.RequestSigning(in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("%s: got %v, want a ValidationError", name, err)
		}
	}
	if l, _ := v.ListSignings(""); len(l) != 0 {
		t.Fatalf("refused requests were kept: %+v", l)
	}
}

func TestSigningRefusesBusinessVault(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	if _, err := v.RequestSigning(signingInput()); err == nil {
		t.Fatal("a business vault signed by hand")
	}
}

func TestSigningLimitExpiryAndStaleDate(t *testing.T) {
	v, _ := signingVault(t)
	base := time.Date(2026, 10, 27, 10, 0, 0, 0, time.Local)
	signingNow = func() time.Time { return base }
	t.Cleanup(func() { signingNow = time.Now })

	byToken := func(i int) SigningInput {
		in := signingInput()
		in.RequesterID = fmt.Sprintf("tok-%d", i)
		return in
	}
	var ids []string
	for i := 0; i < MaxPendingSignings; i++ {
		info, err := v.RequestSigning(byToken(i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, info.ID)
	}
	if _, err := v.RequestSigning(byToken(99)); !errors.Is(err, ErrSigningLimit) {
		t.Fatalf("sixth request: %v", err)
	}
	if _, err := v.RejectSigning(ids[0], "", "session"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.RequestSigning(byToken(98)); err != nil {
		t.Fatalf("a rejected request still counts: %v", err)
	}

	// A request left for 31 minutes is expired and can't be approved.
	signingNow = func() time.Time { return base.Add(SigningTTL + time.Minute) }
	info, _ := v.SigningStatus(ids[1], "")
	if info.Status != SigningExpired {
		t.Fatalf("status after the TTL = %s", info.Status)
	}
	if _, err := v.ApproveSigning(ids[1], info.SHA256, "session"); !errors.Is(err, ErrSigningNotPending) {
		t.Fatalf("approved an expired request: %v", err)
	}

	// A request whose date is yesterday's can't be approved either.
	signingNow = func() time.Time { return time.Date(2026, 10, 27, 23, 50, 0, 0, time.Local) }
	late, err := v.RequestSigning(byToken(97))
	if err != nil {
		t.Fatal(err)
	}
	signingNow = func() time.Time { return time.Date(2026, 10, 28, 0, 5, 0, 0, time.Local) }
	if _, err := v.ApproveSigning(late.ID, late.SHA256, "session"); !errors.Is(err, ErrSigningStale) {
		t.Fatalf("approved across midnight: %v", err)
	}

	// Long after, the record is forgotten.
	signingNow = func() time.Time { return base.Add(48 * time.Hour) }
	if l, _ := v.ListSignings(""); len(l) != 0 {
		t.Fatalf("old records kept: %d", len(l))
	}
}

func TestSigningRequestsAreVisibleOnlyToTheirRequester(t *testing.T) {
	v, _ := signingVault(t)
	in := signingInput()
	info, err := v.RequestSigning(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.SigningStatus(info.ID, "someone-else"); !errors.Is(err, ErrSigningNotFound) {
		t.Fatalf("another requester saw it: %v", err)
	}
	if l, _ := v.ListSignings("someone-else"); len(l) != 0 {
		t.Fatalf("another requester listed it: %+v", l)
	}
	if _, err := v.RejectSigning(info.ID, "someone-else", "someone-else"); !errors.Is(err, ErrSigningNotFound) {
		t.Fatalf("another requester rejected it: %v", err)
	}
	if _, err := v.RejectSigning(info.ID, in.RequesterID, in.Requester); err != nil {
		t.Fatalf("the requester can't withdraw its own: %v", err)
	}
}

func TestLockDropsSigningRequests(t *testing.T) {
	v, sk := signingVault(t)
	info, err := v.RequestSigning(signingInput())
	if err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if _, err := v.SigningStatus(info.ID, ""); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked vault: %v", err)
	}
	if _, err := v.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	if _, err := v.SigningStatus(info.ID, ""); !errors.Is(err, ErrSigningNotFound) {
		t.Fatalf("a request survived a lock: %v", err)
	}
}

func TestSignGrantReachesOnlyItsRequest(t *testing.T) {
	v, sk := signingVault(t)
	a, _ := v.RequestSigning(signingInput())
	b, _ := v.RequestSigning(signingInput())

	if _, _, err := v.CreateSignGrant("wrong", sk, a.ID); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := v.CreateSignGrant(testPassword, sk, strings.Repeat("a", 32)); !errors.Is(err, ErrSigningNotFound) {
		t.Fatalf("unknown request: %v", err)
	}
	token, _, err := v.CreateSignGrant(testPassword, sk, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := v.SigningGrant(token); !ok || id != a.ID {
		t.Fatalf("SigningGrant = %q, %v", id, ok)
	}
	// It opens nothing else.
	if v.ValidateToken(token) {
		t.Fatal("a sign grant is a session")
	}
	if _, _, ok := v.ScopedGrant(token); ok {
		t.Fatal("a sign grant is a scoped grant")
	}
	// A session or scoped grant is not a sign grant.
	if sess, _, _ := v.CreateSessionGrant(testPassword, sk, time.Minute); sess != "" {
		if _, ok := v.SigningGrant(sess); ok {
			t.Fatal("a session grant is a sign grant")
		}
	}
	// Approving or rejecting its request ends it; the other request's
	// grants are untouched.
	other, _, _ := v.CreateSignGrant(testPassword, sk, b.ID)
	if _, err := v.ApproveSigning(a.ID, a.SHA256, "grant"); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.SigningGrant(token); ok {
		t.Fatal("a sign grant outlived the approval")
	}
	if _, ok := v.SigningGrant(other); !ok {
		t.Fatal("approving one request ended another's grant")
	}
	if _, _, err := v.CreateSignGrant(testPassword, sk, a.ID); !errors.Is(err, ErrSigningNotPending) {
		t.Fatalf("a grant for a decided request: %v", err)
	}
}

func TestSignatureDocumentRules(t *testing.T) {
	v, _ := tmpVault(t)
	good := testSignaturePNG(t)

	for name, tc := range map[string]struct {
		ct   string
		data []byte
	}{
		"pdf":       {"application/pdf", minimalPDF},
		"not image": {"image/png", []byte("not a png")},
		"too big":   {"image/png", append(append([]byte(nil), good...), make([]byte, MaxSignatureImage)...)},
	} {
		if _, err := v.AddDocument(SignatureCategory, "firma.png", tc.ct, tc.data, ""); err == nil {
			t.Errorf("%s was stored", name)
		}
	}
	if _, err := v.AddDocument(SignatureCategory, "firma.png", "image/png", good, "standard"); err == nil {
		t.Error("a signature was stored below the personal tier")
	}

	first, err := v.AddDocument(SignatureCategory, "a.png", "image/png", good, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Sensitivity != TierPersonal {
		t.Fatalf("tier = %s", first.Sensitivity)
	}
	second, err := v.AddDocument(SignatureCategory, "b.png", "image/png", good, "")
	if err != nil {
		t.Fatal(err)
	}
	docs, _ := v.ListDocuments(nil)
	var left []string
	for _, d := range docs {
		if d.Category == SignatureCategory {
			left = append(left, d.ID)
		}
	}
	if len(left) != 1 || left[0] != second.ID {
		t.Fatalf("signature documents = %v, want only the newest %s", left, second.ID)
	}
}

// pdfWithStream is a one-page PDF whose content stream is the given bytes.
func pdfWithStream(t *testing.T, stream []byte, extra string) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	var offs []int
	obj := func(body string) {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offs), body)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 4 0 R >>")
	offs = append(offs, b.Len())
	fmt.Fprintf(&b, "4 0 obj\n<< /Length %d %s>>\nstream\n", len(stream), extra)
	b.Write(stream)
	b.WriteString("\nendstream\nendobj\n")
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, x)
	return b.Bytes()
}

func TestSigningRefusesADecompressionBomb(t *testing.T) {
	v, _ := signingVault(t)
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zeros := make([]byte, 1<<20)
	for i := 0; i < 300; i++ { // 300 MB of zeros, a few hundred KB compressed
		zw.Write(zeros)
	}
	zw.Close()
	if z.Len() > 2<<20 {
		t.Fatalf("test bomb is %d bytes, not small", z.Len())
	}
	in := signingInput()
	in.PDF = pdfWithStream(t, z.Bytes(), "/Filter /FlateDecode ")

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	_, err := v.RequestSigning(in)
	runtime.ReadMemStats(&after)
	var verr *ValidationError
	if !errors.As(err, &verr) || !strings.Contains(err.Error(), "64 MB") {
		t.Fatalf("a bomb was not refused for what it is: %v", err)
	}
	if grew := (after.TotalAlloc - before.TotalAlloc) >> 20; grew > 200 {
		t.Fatalf("refusing it allocated %d MB", grew)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("refusing it took %v", time.Since(start))
	}
	// An honest compressed stream still signs.
	var ok bytes.Buffer
	w := zlib.NewWriter(&ok)
	w.Write([]byte("0.5 g 10 10 20 20 re f"))
	w.Close()
	in.PDF = pdfWithStream(t, ok.Bytes(), "/Filter /FlateDecode ")
	if _, err := v.RequestSigning(in); err != nil {
		t.Fatalf("an honest PDF with a compressed stream: %v", err)
	}
}

func TestSigningRefusesEncryptedAndHugePDFs(t *testing.T) {
	v, _ := signingVault(t)
	var enc bytes.Buffer
	if err := api.Encrypt(t.Context(), bytes.NewReader(minimalPDF), &enc, model.NewAESConfiguration("", "owner", 256)); err != nil {
		t.Fatal(err)
	}
	in := signingInput()
	in.PDF = enc.Bytes()
	if _, err := v.RequestSigning(in); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("an encrypted PDF: %v", err)
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	var offs []int
	obj := func(body string) {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offs), body)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, MaxSigningPages+1)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", i+3)
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids)))
	for range kids {
		obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] >>")
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, x)
	in.PDF = b.Bytes()
	if _, err := v.RequestSigning(in); err == nil || !strings.Contains(err.Error(), "1 to 1000 pages") {
		t.Fatalf("a %d-page PDF: %v", len(kids), err)
	}
}

func TestSigningTextCantImpersonate(t *testing.T) {
	v, _ := signingVault(t)
	for name, mutate := range map[string]func(*SigningInput){
		"bidi override in the name":  func(in *SigningInput) { in.Name = "mod‮txt.fdp" },
		"zero width in the name":     func(in *SigningInput) { in.Name = "mod​-1.pdf" },
		"line separator in the name": func(in *SigningInput) { in.Name = "mod -1.pdf" },
		"bidi override in purpose":   func(in *SigningInput) { in.Purpose = "pay‮1 EUR" },
	} {
		in := signingInput()
		mutate(&in)
		if _, err := v.RequestSigning(in); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestSignatureMustBeATransparentPNG(t *testing.T) {
	v, _ := tmpVault(t)
	opaque := image.NewNRGBA(image.Rect(0, 0, 100, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 100; x++ {
			opaque.Set(x, y, color.NRGBA{255, 255, 255, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, opaque)
	if _, err := v.AddDocument(SignatureCategory, "white.png", "image/png", buf.Bytes(), ""); err == nil || !strings.Contains(err.Error(), "transparent") {
		t.Fatalf("a PNG on a white background: %v", err)
	}
	var jpg bytes.Buffer
	jpeg.Encode(&jpg, opaque, nil)
	if _, err := v.AddDocument(SignatureCategory, "sig.jpg", "image/jpeg", jpg.Bytes(), ""); err == nil {
		t.Fatal("a JPEG was accepted as a signature")
	}
	if _, err := v.AddDocument(SignatureCategory, "ok.png", "image/png", testSignaturePNG(t), ""); err != nil {
		t.Fatalf("a PNG with a transparent background: %v", err)
	}
}

func TestSigningLimitsPerRequesterAndConcurrent(t *testing.T) {
	v, _ := signingVault(t)
	signingNow = func() time.Time { return time.Date(2026, 10, 27, 10, 0, 0, 0, time.Local) }
	t.Cleanup(func() { signingNow = time.Now })

	// One token can't use all the vault's places.
	for i := 0; i < MaxPendingPerRequester; i++ {
		if _, err := v.RequestSigning(signingInput()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.RequestSigning(signingInput()); !errors.Is(err, ErrSigningRequesterLimit) {
		t.Fatalf("a token's fourth request: %v", err)
	}
	other := signingInput()
	other.RequesterID = "tok-b"
	if _, err := v.RequestSigning(other); err != nil {
		t.Fatalf("another token was blocked by the first: %v", err)
	}

	// Requests that arrive together can't all pass the limit.
	v2, _ := signingVault(t)
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := signingInput()
			in.RequesterID = fmt.Sprintf("tok-%d", i)
			if _, err := v2.RequestSigning(in); err == nil {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != MaxPendingSignings {
		t.Fatalf("%d of 20 simultaneous requests were accepted, want %d", accepted.Load(), MaxPendingSignings)
	}
	if l, _ := v2.ListSignings(""); len(l) != MaxPendingSignings {
		t.Fatalf("%d pending", len(l))
	}
}

func TestSigningRatePerRequester(t *testing.T) {
	v, _ := signingVault(t)
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.Local)
	signingNow = func() time.Time { return now }
	t.Cleanup(func() { signingNow = time.Now })
	var last error
	for i := 0; i < MaxRequestsPerHour+1; i++ {
		info, err := v.RequestSigning(signingInput())
		last = err
		if err != nil {
			break
		}
		v.RejectSigning(info.ID, "tok-a", "tari-agent") // withdrawing doesn't reset the count
	}
	if !errors.Is(last, ErrSigningRate) {
		t.Fatalf("a request/withdraw loop: %v", last)
	}
	now = now.Add(61 * time.Minute)
	if _, err := v.RequestSigning(signingInput()); err != nil {
		t.Fatalf("after the hour: %v", err)
	}
}

func TestSigningPlacementsAreCapped(t *testing.T) {
	v, _ := signingVault(t)
	in := signingInput()
	in.Placements = nil
	for i := 0; i < MaxPlacements+1; i++ {
		in.Placements = append(in.Placements, Placement{Page: 1, Rect: [4]float64{100, 40, 190, 90}})
	}
	if _, err := v.RequestSigning(in); err == nil || !strings.Contains(err.Error(), "signature boxes") {
		t.Fatalf("%d placements: %v", len(in.Placements), err)
	}
}

func TestSignatureMustReallyBeAPNG(t *testing.T) {
	v, _ := tmpVault(t)
	img := image.NewPaletted(image.Rect(0, 0, 40, 20), color.Palette{color.Transparent, color.Black})
	var gifBytes bytes.Buffer
	gif.Encode(&gifBytes, img, nil)
	if _, err := v.AddDocument(SignatureCategory, "sig.png", "image/png", gifBytes.Bytes(), ""); err == nil {
		t.Fatal("a GIF passed as a PNG was accepted")
	}
}

func TestReservedConsumerNames(t *testing.T) {
	v, _ := tmpVault(t)
	for _, name := range []string{"session", "Grant", " vault "} {
		if _, err := v.CreateServiceToken(name, "identity.*", time.Hour, "", ""); err == nil {
			t.Errorf("a token named %q was created", name)
		}
	}
	if _, err := v.CreateServiceToken("tari-agent", "identity.*", time.Hour, "", ""); err != nil {
		t.Fatal(err)
	}
}
