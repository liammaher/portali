package vault

import (
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/lovincyrus/personal-vault/internal/crypto"
	"github.com/lovincyrus/personal-vault/internal/store"
)

// MaxDocumentSize caps a single stored document (scans, PDFs).
const MaxDocumentSize = 20 << 20 // 20 MB

var (
	reDocumentID  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	reContentType = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)
)

// DocumentInfo is decrypted document metadata returned to callers.
type DocumentInfo struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Sensitivity string    `json:"sensitivity"`
	CreatedAt   time.Time `json:"created_at"`
}

// DocumentScopeID is the field-style ID token scopes are checked against for
// documents in a category: "passaporto.*" or "passaporto.documents" grants
// access to passport scans.
func DocumentScopeID(category string) string {
	return category + ".documents"
}

// DefaultDocumentSensitivity is the tier of a document stored without one
// (outside the SPID/CieID categories, which are always personal).
const DefaultDocumentSensitivity = "sensitive"

// AddDocument encrypts and stores a file under a category. The content and
// original filename are encrypted with the category's documents subkey and
// bound to the document ID, so ciphertexts cannot be swapped between records.
func (v *Vault) AddDocument(category, name, contentType string, data []byte, sensitivity string) (*DocumentInfo, error) {
	if !ValidCategoryName(category) {
		return nil, &ValidationError{Field: "category", Msg: "only letters, digits, underscore and hyphen allowed"}
	}
	name, err := cleanDocumentName(name)
	if err != nil {
		return nil, err
	}
	contentType, err = cleanContentType(contentType)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, &ValidationError{Field: "document", Msg: "file is empty"}
	}
	if len(data) > MaxDocumentSize {
		return nil, &ValidationError{Field: "document", Msg: fmt.Sprintf("file is larger than %d MB", MaxDocumentSize>>20)}
	}
	// SPID/CieID documents (e.g. recovery codes) are always personal.
	personalOnly := IsPersonalCategory(category) || IsCredentialField(DocumentScopeID(category))
	switch {
	case sensitivity == "" && personalOnly:
		sensitivity = TierPersonal
	case sensitivity == "":
		sensitivity = DefaultDocumentSensitivity
	case !validTiers[sensitivity]:
		return nil, ErrInvalidTier
	case personalOnly && sensitivity != TierPersonal:
		return nil, errCredentialTier(DocumentScopeID(category))
	}

	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}
	subkey, err := crypto.DeriveSubkey(vaultKey, v.salt, documentKeyInfo(category))
	if err != nil {
		return nil, err
	}

	idBytes := make([]byte, 16)
	if _, err := crand.Read(idBytes); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(idBytes)

	content, err := crypto.EncryptWithAAD(subkey, data, documentAAD(id))
	if err != nil {
		return nil, fmt.Errorf("encrypt document: %w", err)
	}
	encName, err := crypto.EncryptWithAAD(subkey, []byte(name), documentNameAAD(id))
	if err != nil {
		return nil, fmt.Errorf("encrypt document name: %w", err)
	}
	if err := v.writeDocumentFile(id, content); err != nil {
		return nil, err
	}

	doc := store.Document{
		ID:          id,
		Category:    category,
		Name:        base64.StdEncoding.EncodeToString(encName),
		ContentType: contentType,
		Size:        int64(len(data)),
		Sensitivity: sensitivity,
		CreatedAt:   time.Now(),
	}
	if err := v.db.CreateDocument(doc); err != nil {
		os.Remove(v.documentPath(id))
		return nil, err
	}

	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: DocumentScopeID(category), Action: "document_add", Purpose: contentType})
	v.markChanged()
	return &DocumentInfo{ID: id, Category: category, Name: name, ContentType: contentType, Size: doc.Size, Sensitivity: sensitivity, CreatedAt: doc.CreatedAt}, nil
}

// DocumentMeta returns a document's category, type, size and tier for access
// checks, without decrypting the filename (Name is empty). Nil if missing.
func (v *Vault) DocumentMeta(id string) (*DocumentInfo, error) {
	if !reDocumentID.MatchString(id) {
		return nil, nil
	}
	if _, err := v.requireUnlocked(); err != nil {
		return nil, err
	}
	doc, err := v.db.GetDocument(id)
	if err != nil || doc == nil {
		return nil, err
	}
	return &DocumentInfo{
		ID:          doc.ID,
		Category:    doc.Category,
		ContentType: doc.ContentType,
		Size:        doc.Size,
		Sensitivity: doc.Sensitivity,
		CreatedAt:   doc.CreatedAt,
	}, nil
}

// ListDocuments returns metadata for the documents allow accepts (checked
// against DocumentScopeID and the document's tier before decrypting names).
func (v *Vault) ListDocuments(allow FieldFilter) ([]DocumentInfo, error) {
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}
	docs, err := v.db.ListDocuments()
	if err != nil {
		return nil, err
	}
	result := []DocumentInfo{}
	for _, doc := range docs {
		if allow != nil && !allow(DocumentScopeID(doc.Category), doc.Sensitivity) {
			continue
		}
		info, err := v.decryptDocumentInfo(vaultKey, doc)
		if err != nil {
			return nil, err
		}
		result = append(result, *info)
	}
	return result, nil
}

// GetDocument decrypts a document's content. Returns nil info if missing.
func (v *Vault) GetDocument(id string) (*DocumentInfo, []byte, error) {
	if !reDocumentID.MatchString(id) {
		return nil, nil, nil
	}
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, nil, err
	}
	doc, err := v.db.GetDocument(id)
	if err != nil || doc == nil {
		return nil, nil, err
	}
	info, err := v.decryptDocumentInfo(vaultKey, *doc)
	if err != nil {
		return nil, nil, err
	}

	ciphertext, err := os.ReadFile(v.documentPath(id))
	if err != nil {
		return nil, nil, fmt.Errorf("read document %s: %w", id, err)
	}
	subkey, err := crypto.DeriveSubkey(vaultKey, v.salt, documentKeyInfo(doc.Category))
	if err != nil {
		return nil, nil, err
	}
	data, err := crypto.DecryptWithAAD(subkey, ciphertext, documentAAD(id))
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt document %s: %w", id, err)
	}

	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: DocumentScopeID(doc.Category), Action: "document_read"})
	return info, data, nil
}

// DeleteDocument removes a document's encrypted file and metadata.
// Returns false if it did not exist.
func (v *Vault) DeleteDocument(id string) (bool, error) {
	if !reDocumentID.MatchString(id) {
		return false, nil
	}
	if _, err := v.requireUnlocked(); err != nil {
		return false, err
	}
	doc, err := v.db.GetDocument(id)
	if err != nil || doc == nil {
		return false, err
	}
	// Remove the row first: if removing the file then fails, only an
	// unreadable encrypted orphan remains, never a listed document without content.
	if _, err := v.db.DeleteDocument(id); err != nil {
		return false, err
	}
	if err := os.Remove(v.documentPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove document file: %w", err)
	}
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: DocumentScopeID(doc.Category), Action: "document_delete"})
	v.markChanged()
	return true, nil
}

func (v *Vault) decryptDocumentInfo(vaultKey []byte, doc store.Document) (*DocumentInfo, error) {
	subkey, err := crypto.DeriveSubkey(vaultKey, v.salt, documentKeyInfo(doc.Category))
	if err != nil {
		return nil, err
	}
	encName, err := base64.StdEncoding.DecodeString(doc.Name)
	if err != nil {
		return nil, fmt.Errorf("decode document name %s: %w", doc.ID, err)
	}
	name, err := crypto.DecryptWithAAD(subkey, encName, documentNameAAD(doc.ID))
	if err != nil {
		return nil, fmt.Errorf("decrypt document name %s: %w", doc.ID, err)
	}
	return &DocumentInfo{
		ID:          doc.ID,
		Category:    doc.Category,
		Name:        string(name),
		ContentType: doc.ContentType,
		Size:        doc.Size,
		Sensitivity: doc.Sensitivity,
		CreatedAt:   doc.CreatedAt,
	}, nil
}

// writeDocumentFile writes ciphertext atomically with owner-only permissions.
func (v *Vault) writeDocumentFile(id string, ciphertext []byte) error {
	dir := filepath.Join(v.dir, "documents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create documents dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("create document file: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(ciphertext); err != nil {
		tmp.Close()
		return fmt.Errorf("write document file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), v.documentPath(id))
}

func (v *Vault) documentPath(id string) string {
	return filepath.Join(v.dir, "documents", id+".enc")
}

// documentKeyInfo is the HKDF info for a category's documents subkey. Field
// categories cannot contain "/", so it never collides with a field subkey.
func documentKeyInfo(category string) string { return "documents/" + category }

func documentAAD(id string) []byte     { return []byte("document:" + id) }
func documentNameAAD(id string) []byte { return []byte("document-name:" + id) }

// cleanDocumentName keeps only the base filename and rejects control characters.
func cleanDocumentName(name string) (string, error) {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "/" || strings.Trim(name, ".") == "" {
		return "", &ValidationError{Field: "name", Msg: "filename is required"}
	}
	if len(name) > 255 {
		return "", &ValidationError{Field: "name", Msg: "filename is longer than 255 bytes"}
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", &ValidationError{Field: "name", Msg: "filename contains control characters"}
		}
	}
	return name, nil
}

// cleanContentType normalizes a MIME type, defaulting to octet-stream.
func cleanContentType(ct string) (string, error) {
	ct = strings.TrimSpace(ct)
	if ct == "" {
		return "application/octet-stream", nil
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || !reContentType.MatchString(strings.ToLower(mediaType)) {
		return "", &ValidationError{Field: "content_type", Msg: "invalid content type"}
	}
	return strings.ToLower(mediaType), nil
}
