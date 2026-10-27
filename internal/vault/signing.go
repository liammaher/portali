package vault

import (
	"bytes"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// The sign gate: an agent files a signing request (a PDF and where the
// signature goes), the vault stamps the user's handwritten signature on it,
// and the user reviews that very file and approves it in person. See
// docs/design/sign-gate.md.

const (
	// SignatureCategory holds the signature image as a document: one PNG or
	// JPEG, always personal.
	SignatureCategory = "firma_autografa"
	// SigningScopeID is what a token scope must allow to file a request.
	SigningScopeID = SignatureCategory + ".signing"

	MaxSignatureImage  = 500 << 10
	MaxSigningPDF      = 10 << 20
	MaxPendingSignings = 5
	SigningTTL         = 30 * time.Minute
	// SignGrantTTL is how long the password or fingerprint of one review
	// stays good: long enough to read a form, short enough to be one review.
	SignGrantTTL = 10 * time.Minute
	// MaxPlacements is the signature and date boxes one request may ask for;
	// each is drawn separately, so the file grows with their number.
	MaxPlacements = 8
	// MaxPendingPerRequester and MaxRequestsPerHour keep one agent from using
	// up the vault's pending slots, or its CPU and audit log.
	MaxPendingPerRequester = 3
	MaxRequestsPerHour     = 20
	// maxSigned caps the signed file, so the user can always fetch it.
	maxSigned = 2 * MaxSigningPDF
	// signingRecord is how long a decided or expired request stays listed
	// (without its file), so a client polling it learns what happened.
	signingRecord = time.Hour
)

// Where a signing request stands.
const (
	SigningPending  = "pending"
	SigningApproved = "approved"
	SigningRejected = "rejected"
	SigningExpired  = "expired"
)

var (
	ErrSigningNotFound       = errors.New("signing request not found")
	ErrSigningNotPending     = errors.New("signing request is no longer pending")
	ErrSigningStale          = errors.New("the date stamped on the signature is not today's: ask for a new request")
	ErrSigningHash           = errors.New("that is not the hash of the document to sign")
	ErrSigningLimit          = fmt.Errorf("%d signing requests are already waiting: review or reject one first", MaxPendingSignings)
	ErrSigningRequesterLimit = fmt.Errorf("this token already has %d signing requests waiting", MaxPendingPerRequester)
	ErrSigningRate           = fmt.Errorf("this token has filed %d signing requests in the last hour", MaxRequestsPerHour)
	ErrNoSignature           = errors.New("no signature on file: store one with 'pvault doc add " + SignatureCategory + " <image.png>'")
)

var reSigningID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidSigningID reports whether id has the shape of a signing request ID.
func ValidSigningID(id string) bool { return reSigningID.MatchString(id) }

// signingNow is the clock, replaced in tests.
var signingNow = time.Now

// SigningInput is a request to sign a PDF.
type SigningInput struct {
	Requester   string // the token's consumer name, or "session"
	RequesterID string // what tells two tokens of one name apart; "" for the user
	Name        string // what the user sees (the form's file name)
	Purpose     string // one line: which pratica and why
	Signer      string // "" or "io": v1 signs only for the vault's owner
	PDF         []byte
	Placements  []Placement
}

// SigningInfo is a request without its file.
type SigningInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Purpose   string    `json:"purpose,omitempty"`
	Requester string    `json:"requester"`
	Signer    string    `json:"signer"`
	Status    string    `json:"status"`
	Pages     int       `json:"pages"`
	SHA256    string    `json:"sha256"` // of the signed file the user reviews
	Date      string    `json:"date"`   // YYYY-MM-DD written beside the signature
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type signRequest struct {
	info        SigningInfo
	requesterID string
	candidate   []byte // the stamped file; dropped when the request is decided
}

// signingStore holds the requests of one session, in memory only.
type signingStore struct {
	mu       sync.Mutex
	reqs     map[string]*signRequest
	reserved int                    // requests being stamped, which already count against the limits
	filed    map[string][]time.Time // when each requester filed, for the hourly cap
}

func (st *signingStore) clear() {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, r := range st.reqs {
		clear(r.candidate)
	}
	st.reqs = nil
	st.reserved = 0
	st.filed = nil
}

// settle expires what is overdue and forgets what has been over for a while;
// st.mu must be held.
func (st *signingStore) settle(now time.Time) {
	for id, r := range st.reqs {
		if r.info.Status == SigningPending && !now.Before(r.info.ExpiresAt) {
			r.info.Status = SigningExpired
			clear(r.candidate)
			r.candidate = nil
		}
		if r.info.Status != SigningPending && now.Sub(r.info.ExpiresAt) > signingRecord {
			delete(st.reqs, id)
		}
	}
}

// reserve checks the limits and takes one of the vault's places for a request
// about to be stamped.
func (st *signingStore) reserve(requesterID string, now time.Time) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.settle(now)
	pending, own := st.reserved, 0
	for _, r := range st.reqs {
		if r.info.Status == SigningPending {
			pending++
			if requesterID != "" && r.requesterID == requesterID {
				own++
			}
		}
	}
	if pending >= MaxPendingSignings {
		return ErrSigningLimit
	}
	if requesterID != "" {
		if own >= MaxPendingPerRequester {
			return ErrSigningRequesterLimit
		}
		recent := st.filed[requesterID][:0]
		for _, t := range st.filed[requesterID] {
			if now.Sub(t) < time.Hour {
				recent = append(recent, t)
			}
		}
		if len(recent) >= MaxRequestsPerHour {
			return ErrSigningRate
		}
		if st.filed == nil {
			st.filed = map[string][]time.Time{}
		}
		st.filed[requesterID] = append(recent, now)
	}
	st.reserved++
	return nil
}

// signing returns the live session's store.
func (v *Vault) signing() (*signingStore, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.session == nil || !v.session.HasKey() {
		return nil, ErrLocked
	}
	return &v.session.signing, nil
}

// RequestSigning stamps the owner's signature on in.PDF at the placements and
// holds the result until the user approves it. Nothing is released before.
func (v *Vault) RequestSigning(in SigningInput) (*SigningInfo, error) {
	if v.kind == KindImpresa {
		return nil, &ValidationError{Field: "signer", Msg: "a business vault can't sign by hand: its people sign, which is not supported yet"}
	}
	if in.Signer != "" && in.Signer != "io" {
		return nil, &ValidationError{Field: "signer", Msg: "only the vault's owner (io) can sign for now"}
	}
	name, err := cleanDocumentName(in.Name)
	if err == nil {
		err = checkPlainText("name", name)
	}
	if err != nil {
		return nil, err
	}
	purpose, err := cleanPurpose(in.Purpose)
	if err != nil {
		return nil, err
	}
	if len(in.PDF) == 0 || len(in.PDF) > MaxSigningPDF || !bytes.HasPrefix(in.PDF, []byte("%PDF-")) {
		return nil, &ValidationError{Field: "pdf", Msg: fmt.Sprintf("must be a PDF of at most %d MB", MaxSigningPDF>>20)}
	}
	if n := len(in.Placements); n == 0 || n > 2*MaxPlacements {
		return nil, &ValidationError{Field: "placements", Msg: fmt.Sprintf("must hold 1 to %d signature boxes", MaxPlacements)}
	}
	st, err := v.signing()
	if err != nil {
		return nil, err
	}
	now := signingNow()
	// Reserve a place before the slow part, so requests that arrive together
	// can't all pass the limit.
	if err := st.reserve(in.RequesterID, now); err != nil {
		return nil, err
	}
	reserved := true
	defer func() {
		if reserved {
			st.mu.Lock()
			st.reserved--
			st.mu.Unlock()
		}
	}()

	img, err := v.signatureImage()
	if err != nil {
		return nil, err
	}
	defer clear(img)
	stamped, pages, err := stampSignature(in.PDF, img, now.Format("02/01/2006"), in.Placements)
	if err != nil {
		return nil, err
	}
	if len(stamped) > maxSigned {
		return nil, &ValidationError{Field: "pdf", Msg: "the signed file would be larger than 20 MB"}
	}
	idBytes := make([]byte, 16)
	if _, err := crand.Read(idBytes); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(stamped)
	info := SigningInfo{
		ID: hex.EncodeToString(idBytes), Name: name, Purpose: purpose, Requester: in.Requester, Signer: "io",
		Status: SigningPending, Pages: pages, SHA256: hex.EncodeToString(sum[:]),
		Date: now.Format("2006-01-02"), CreatedAt: now, ExpiresAt: now.Add(SigningTTL),
	}
	st.mu.Lock()
	if st.reqs == nil {
		st.reqs = map[string]*signRequest{}
	}
	st.reqs[info.ID] = &signRequest{info: info, requesterID: in.RequesterID, candidate: stamped}
	st.reserved--
	reserved = false
	st.mu.Unlock()
	v.auditSigning(in.Requester, "sign_request", info)
	return &info, nil
}

// signatureImage decrypts the owner's newest signature document.
func (v *Vault) signatureImage() ([]byte, error) {
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}
	defer clear(vaultKey)
	docs, err := v.db.ListDocuments() // newest first within a category
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if doc.Category == SignatureCategory {
			return v.decryptDocumentContent(vaultKey, doc)
		}
	}
	return nil, ErrNoSignature
}

// ListSignings returns the requests a requester made, or all of them when
// requester is empty. Newest first.
func (v *Vault) ListSignings(requesterID string) ([]SigningInfo, error) {
	st, err := v.signing()
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.settle(signingNow())
	out := []SigningInfo{}
	for _, r := range st.reqs {
		if requesterID == "" || r.requesterID == requesterID {
			out = append(out, r.info)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// SigningStatus returns one request; requester limits it to that requester's
// own (empty: any).
func (v *Vault) SigningStatus(id, requesterID string) (*SigningInfo, error) {
	st, err := v.signing()
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	r, err := st.find(id, requesterID)
	if err != nil {
		return nil, err
	}
	info := r.info
	return &info, nil
}

// find settles the store and returns request id, hiding other requesters'.
func (st *signingStore) find(id, requesterID string) (*signRequest, error) {
	st.settle(signingNow())
	r, ok := st.reqs[id]
	if !ok || (requesterID != "" && r.requesterID != requesterID) {
		return nil, ErrSigningNotFound
	}
	return r, nil
}

// SigningCandidate returns the stamped file of a pending request, for the
// user's review.
func (v *Vault) SigningCandidate(id, by string) (*SigningInfo, []byte, error) {
	st, err := v.signing()
	if err != nil {
		return nil, nil, err
	}
	st.mu.Lock()
	r, err := st.find(id, "")
	if err != nil {
		st.mu.Unlock()
		return nil, nil, err
	}
	if r.info.Status != SigningPending {
		st.mu.Unlock()
		return nil, nil, ErrSigningNotPending
	}
	info, file := r.info, bytes.Clone(r.candidate)
	st.mu.Unlock()
	v.auditSigning(by, "sign_view", info)
	return &info, file, nil
}

// ApproveSigning marks a pending request approved if sha256 is the hash of
// the file the user reviewed, and says who approved ("session" or "grant").
// The vault drops its copy: the reviewer holds the file.
func (v *Vault) ApproveSigning(id, sha256Hex, by string) (*SigningInfo, error) {
	st, err := v.signing()
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	r, err := st.find(id, "")
	if err != nil {
		st.mu.Unlock()
		return nil, err
	}
	if r.info.Status != SigningPending {
		st.mu.Unlock()
		return nil, ErrSigningNotPending
	}
	if signingNow().Format("2006-01-02") != r.info.Date {
		r.info.Status = SigningExpired
		clear(r.candidate)
		r.candidate = nil
		info := r.info
		st.mu.Unlock()
		v.auditSigning(by, "sign_refused", info)
		return nil, ErrSigningStale
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(sha256Hex))), []byte(r.info.SHA256)) != 1 {
		info := r.info
		st.mu.Unlock()
		v.auditSigning(by, "sign_refused", info)
		return nil, ErrSigningHash
	}
	r.info.Status = SigningApproved
	clear(r.candidate)
	r.candidate = nil
	info := r.info
	st.mu.Unlock()

	v.dropSignGrants(id)
	v.auditSigning(by, "sign_approve", info)
	return &info, nil
}

// RejectSigning refuses a pending request. requester limits it to that
// requester's own (empty: the user).
func (v *Vault) RejectSigning(id, requesterID, by string) (*SigningInfo, error) {
	st, err := v.signing()
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	r, err := st.find(id, requesterID)
	if err != nil {
		st.mu.Unlock()
		return nil, err
	}
	if r.info.Status != SigningPending {
		st.mu.Unlock()
		return nil, ErrSigningNotPending
	}
	r.info.Status = SigningRejected
	clear(r.candidate)
	r.candidate = nil
	info := r.info
	st.mu.Unlock()

	v.dropSignGrants(id)
	v.auditSigning(by, "sign_reject", info)
	return &info, nil
}

// CreateSignGrant checks the password like any grant, then issues one that
// can only read and approve request id (SignGrantTTL). It is how the user
// authorises one review.
func (v *Vault) CreateSignGrant(password, secretKeyHex, id string) (string, time.Time, error) {
	session, err := v.checkGrantCredentials(password, secretKeyHex)
	if err != nil {
		return "", time.Time{}, err
	}
	info, err := v.SigningStatus(id, "")
	if err != nil {
		return "", time.Time{}, err
	}
	if info.Status != SigningPending {
		return "", time.Time{}, ErrSigningNotPending
	}
	token, expires, err := session.AddSignGrant(SignGrantTTL, id)
	if err != nil {
		return "", time.Time{}, err
	}
	session.Touch()
	v.auditSigning("vault", "sign_grant", *info)
	return token, expires, nil
}

// SigningGrant returns the request a valid sign grant is for.
func (v *Vault) SigningGrant(token string) (id string, ok bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.session == nil {
		return "", false
	}
	return v.session.SigningGrant(token)
}

func (v *Vault) dropSignGrants(id string) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.session != nil {
		v.session.DropSignGrants(id)
	}
}

func (v *Vault) auditSigning(consumer, action string, info SigningInfo) {
	v.db.LogAccess(store.AuditEntry{
		Consumer: consumer, Scope: SigningScopeID, Action: action,
		Purpose: fmt.Sprintf("%s; %d pages; id %s; sha256 %s", info.Name, info.Pages, info.ID[:8], info.SHA256),
	})
}

// cleanPurpose keeps a request's one-line reason short and printable.
func cleanPurpose(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return "", &ValidationError{Field: "purpose", Msg: "is longer than 200 bytes"}
	}
	if err := checkPlainText("purpose", s); err != nil {
		return "", err
	}
	return s, nil
}

// checkPlainText refuses what could make a name or purpose read differently
// from what it is where the user sees it: control characters, and invisible
// formatting such as bidi overrides, zero-width characters and line separators.
func checkPlainText(field, s string) error {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return &ValidationError{Field: field, Msg: "contains control or invisible formatting characters"}
		}
	}
	return nil
}

// checkSignatureImage is the rule for documents in SignatureCategory: a small
// PNG with a transparent background that the stamper can use. A JPEG, or a PNG
// with a white background, would paint a box over the printed signature line.
func checkSignatureImage(contentType string, data []byte) error {
	if contentType != "image/png" {
		return &ValidationError{Field: "document", Msg: "the signature must be a PNG with a transparent background"}
	}
	if len(data) > MaxSignatureImage {
		return &ValidationError{Field: "document", Msg: fmt.Sprintf("the signature image is larger than %d KB", MaxSignatureImage>>10)}
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxSignaturePixels {
		return &ValidationError{Field: "document", Msg: "the signature image can't be read or is too large"}
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return &ValidationError{Field: "document", Msg: "the signature image can't be read"}
	}
	if o, ok := img.(interface{ Opaque() bool }); ok && o.Opaque() {
		return &ValidationError{Field: "document", Msg: "the signature image has no transparent background: it would cover the signature line (save it as a PNG with a transparent background)"}
	}
	if _, _, err := stampSignature(minimalPDF, data, "01/01/2000", []Placement{{Page: 1, Rect: [4]float64{10, 10, 110, 60}}}); err != nil {
		return &ValidationError{Field: "document", Msg: "the signature image can't be read or placed"}
	}
	return nil
}
