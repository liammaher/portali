package vault

import (
	"archive/tar"
	"bufio"
	"bytes"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lovincyrus/personal-vault/internal/crypto"
	"github.com/lovincyrus/personal-vault/internal/store"
)

// Backups are whole-vault copies meant to live off this machine (iCloud
// Drive, a USB disk). A backup holds a snapshot of vault.db and every
// encrypted document, packed in a tar stream that is encrypted again with a
// key derived from the vault key, so field names, tiers and the audit log are
// hidden too. Restoring needs the profile password and the secret key, which
// is never in the backup: keep it apart from the backup files.
//
// File layout: magic "PVAULTBK" | version | created (unix, int64) | salt
// length | vault salt | 16-byte file ID, then the chunked AES-256-GCM stream
// (crypto.NewStreamWriter) bound to that header. The file key is
// HKDF(vault key, salt, "backup/<file ID>"), unique per file.

const (
	backupMagic   = "PVAULTBK"
	backupVersion = 1
	backupIDLen   = 16
	// backupKeep newest backups are always kept, plus the newest of each of
	// the last backupKeepDays days, so a bad edit can't rotate out every
	// good copy within an hour.
	backupKeep     = 10
	backupKeepDays = 30

	metaBackupDir  = "backup_dir"
	metaBackupLast = "backup_last"
)

// backupDelay is how long after the last change an automatic backup runs.
// Changes in quick succession (onboarding, a pratica) share one backup.
var backupDelay = time.Minute

var (
	// ErrNoBackupDir is returned by Backup before a folder has been chosen.
	ErrNoBackupDir = errors.New("no backup folder set — run 'pvault backup --to <folder>'")
	// ErrBackupUnreadable covers both a wrong password or secret key and a
	// damaged file: authenticated encryption can't tell them apart.
	ErrBackupUnreadable = errors.New("cannot decrypt backup: wrong password or secret key, or the file is damaged")

	reBackupName   = regexp.MustCompile(`^pvault-([0-9a-f]{8})-(\d{8}T\d{6}\.\d{3}Z)\.pvbackup$`)
	reBackupDocEnt = regexp.MustCompile(`^documents/[0-9a-f]{32}\.enc$`)
)

const backupTimeFormat = "20060102T150405.000Z"

// backupState tracks automatic backups. mu serializes backup runs; stateMu
// guards the fields below it.
type backupState struct {
	mu      sync.Mutex
	stateMu sync.Mutex
	timer   *time.Timer
	pending bool
	lastErr string
}

// BackupStatus is the backup part of VaultStatus. The folder is left out
// because status is public.
type BackupStatus struct {
	Configured bool   `json:"configured"`
	Last       string `json:"last,omitempty"` // RFC3339 time of the last good backup
	Error      string `json:"error,omitempty"`
}

// BackupInfo describes a backup that decrypted and checked out.
type BackupInfo struct {
	Created   time.Time `json:"created"`
	Fields    int       `json:"fields"`
	Documents int       `json:"documents"`
}

// SetBackupDir chooses the folder backups are written to. It must already
// exist and lie outside the vault folder.
func (v *Vault) SetBackupDir(dir string) error {
	if _, err := v.requireUnlocked(); err != nil {
		return err
	}
	if !filepath.IsAbs(dir) {
		return &ValidationError{Field: "dir", Msg: "must be an absolute path"}
	}
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return &ValidationError{Field: "dir", Msg: "folder does not exist"}
	}
	if isWithin(dir, v.dir) {
		return &ValidationError{Field: "dir", Msg: "must be outside the vault folder, or the backup is lost with it"}
	}
	return v.db.SetMeta(metaBackupDir, dir)
}

// isWithin reports whether path is base or inside it. It resolves symlinks
// and compares files, not names, so links, a relative base and
// case-insensitive disks can't hide it.
func isWithin(path, base string) bool {
	baseInfo, err := os.Stat(base)
	if err != nil {
		return false
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	for p := path; ; p = filepath.Dir(p) {
		if info, err := os.Stat(p); err == nil && os.SameFile(info, baseInfo) {
			return true
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
}

// backupStatus reports backup state for Status.
func (v *Vault) backupStatus() *BackupStatus {
	dir, _ := v.db.GetMeta(metaBackupDir)
	last, _ := v.db.GetMeta(metaBackupLast)
	v.backup.stateMu.Lock()
	defer v.backup.stateMu.Unlock()
	return &BackupStatus{Configured: dir != "", Last: last, Error: v.backup.lastErr}
}

// Backup writes an encrypted backup to the chosen folder now, prunes old
// ones, and returns the new file's path.
func (v *Vault) Backup() (string, error) {
	v.backup.mu.Lock()
	defer v.backup.mu.Unlock()
	return v.backupLocked()
}

// backupLocked is Backup for a caller holding backup.mu.
func (v *Vault) backupLocked() (string, error) {
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return "", err
	}
	defer zero(vaultKey)

	dir, err := v.db.GetMeta(metaBackupDir)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", ErrNoBackupDir
	}
	now := time.Now().UTC()
	path, err := v.writeBackup(vaultKey, dir, now)

	v.backup.stateMu.Lock()
	v.backup.lastErr = backupStatusError(err, dir)
	v.backup.stateMu.Unlock()
	if err != nil {
		return "", err
	}

	v.db.SetMeta(metaBackupLast, now.Format(time.RFC3339))
	pruneBackups(dir, backupVaultID(v.salt), now)
	return path, nil
}

// backupStatusError words a backup failure for the public status, which must
// not show the folder (os errors include the path).
func backupStatusError(err error, dir string) string {
	if err == nil {
		return ""
	}
	if info, serr := os.Stat(dir); serr != nil || !info.IsDir() {
		return "backup folder is missing — run 'pvault backup --to <folder>'"
	}
	switch {
	case errors.Is(err, os.ErrPermission):
		return "no permission to write the backup (check the folder's permissions)"
	default:
		return "last backup failed — run 'pvault backup' to see why"
	}
}

// markChanged schedules an automatic backup backupDelay after the latest
// change. Call it after every successful write.
func (v *Vault) markChanged() {
	s := &v.backup
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.pending = true
	if s.timer == nil {
		s.timer = time.AfterFunc(backupDelay, v.flushBackup)
	} else {
		s.timer.Reset(backupDelay)
	}
}

// flushBackup runs a scheduled backup now, if there is one and a folder is
// set. Lock and auto-lock call it while the key is still available, so the
// last changes of a session are never left out.
func (v *Vault) flushBackup() {
	v.backup.mu.Lock()
	defer v.backup.mu.Unlock()
	v.flushBackupLocked()
}

// flushBackupLocked is flushBackup for a caller holding backup.mu, so no
// other flush can take the pending backup and lose it to a lock.
func (v *Vault) flushBackupLocked() {
	v.mu.RLock()
	unlocked := v.session != nil
	v.mu.RUnlock()
	if !unlocked {
		return
	}

	s := &v.backup
	s.stateMu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	pending := s.pending
	s.pending = false
	s.stateMu.Unlock()
	if !pending {
		return
	}

	if dir, err := v.db.GetMeta(metaBackupDir); err != nil || dir == "" {
		return
	}
	if _, err := v.backupLocked(); err != nil {
		// Try again at the next change or lock.
		s.stateMu.Lock()
		s.pending = true
		s.stateMu.Unlock()
	}
}

func (v *Vault) writeBackup(vaultKey []byte, dir string, now time.Time) (string, error) {
	// backup.mu is held, so any work dir already here was left by a crash.
	// It holds a database snapshot (field names, audit log): remove it.
	if entries, err := os.ReadDir(v.dir); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), ".backup-") {
				os.RemoveAll(filepath.Join(v.dir, e.Name()))
			}
		}
	}
	work, err := os.MkdirTemp(v.dir, ".backup-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)

	snapshot := filepath.Join(work, "vault.db")
	if err := v.db.Snapshot(snapshot); err != nil {
		return "", fmt.Errorf("snapshot database: %w", err)
	}
	docIDs, err := store.SnapshotDocumentIDs(snapshot)
	if err != nil {
		return "", fmt.Errorf("read snapshot: %w", err)
	}
	// A document deleted since the snapshot (its row goes first), or whose
	// file is lost, is left out of the backup rather than failing every one.
	var present, missing []string
	for _, id := range docIDs {
		_, err := os.Stat(v.documentPath(id))
		switch {
		case err == nil:
			present = append(present, id)
		case errors.Is(err, os.ErrNotExist):
			missing = append(missing, id)
		default:
			return "", err
		}
	}
	if len(missing) > 0 {
		if err := store.SnapshotDropDocuments(snapshot, missing); err != nil {
			return "", fmt.Errorf("update snapshot: %w", err)
		}
	}
	docIDs = present

	fileID := make([]byte, backupIDLen)
	if _, err := crand.Read(fileID); err != nil {
		return "", err
	}
	header := encodeBackupHeader(now, v.salt, fileID)
	key, err := crypto.DeriveSubkey(vaultKey, v.salt, backupKeyInfo(fileID))
	if err != nil {
		return "", err
	}
	defer zero(key)

	tmp, err := os.CreateTemp(dir, ".pvault-backup-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create backup file: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return "", err
	}

	bw := bufio.NewWriter(tmp)
	err = func() error {
		if _, err := bw.Write(header); err != nil {
			return err
		}
		sw, err := crypto.NewStreamWriter(bw, key, header)
		if err != nil {
			return err
		}
		tw := tar.NewWriter(sw)
		if err := addTarFile(tw, "vault.db", snapshot); err != nil {
			return err
		}
		for _, id := range docIDs {
			if err := addTarFile(tw, "documents/"+id+".enc", v.documentPath(id)); err != nil {
				return err
			}
		}
		if err := tw.Close(); err != nil {
			return err
		}
		if err := sw.Close(); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
		return tmp.Sync()
	}()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}

	path := filepath.Join(dir, backupFileName(v.salt, now))
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func addTarFile(tw *tar.Writer, name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// backupVaultID names the vault in backup file names, so vaults sharing a
// folder never prune each other's backups. A restored vault keeps its salt
// and so continues the same series. The salt is not secret.
func backupVaultID(salt []byte) string {
	h := sha256.Sum256(salt)
	return hex.EncodeToString(h[:4])
}

func backupFileName(salt []byte, created time.Time) string {
	return "pvault-" + backupVaultID(salt) + "-" + created.Format(backupTimeFormat) + ".pvbackup"
}

// pruneBackups deletes this vault's old backups in dir, keeping the
// backupKeep newest and the newest of each of the last backupKeepDays days,
// plus temp files a crash left over a day ago. Other files are not touched.
func pruneBackups(dir, vaultID string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if m := reBackupName.FindStringSubmatch(e.Name()); m != nil && m[1] == vaultID {
			names = append(names, e.Name())
		} else if strings.HasPrefix(e.Name(), ".pvault-backup-") && strings.HasSuffix(e.Name(), ".tmp") {
			if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > 24*time.Hour {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // newest first

	seenDays := map[string]bool{}
	for i, name := range names {
		stamp := reBackupName.FindStringSubmatch(name)[2]
		created, err := time.Parse(backupTimeFormat, stamp)
		if err != nil {
			continue
		}
		day := stamp[:8]
		keep := i < backupKeep || (!seenDays[day] && now.Sub(created) < backupKeepDays*24*time.Hour)
		seenDays[day] = true
		if !keep {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

func backupKeyInfo(fileID []byte) string { return "backup/" + hex.EncodeToString(fileID) }

func encodeBackupHeader(created time.Time, salt, fileID []byte) []byte {
	var b bytes.Buffer
	b.WriteString(backupMagic)
	b.WriteByte(backupVersion)
	binary.Write(&b, binary.BigEndian, created.Unix())
	b.WriteByte(byte(len(salt)))
	b.Write(salt)
	b.Write(fileID)
	return b.Bytes()
}

type backupHeader struct {
	raw     []byte
	created time.Time
	salt    []byte
	fileID  []byte
}

func readBackupHeader(r io.Reader) (*backupHeader, error) {
	notBackup := errors.New("not a pvault backup file")
	fixed := make([]byte, len(backupMagic)+1+8+1)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return nil, notBackup
	}
	if string(fixed[:len(backupMagic)]) != backupMagic {
		return nil, notBackup
	}
	if v := fixed[len(backupMagic)]; v != backupVersion {
		return nil, fmt.Errorf("backup format version %d is not supported by this pvault", v)
	}
	created := int64(binary.BigEndian.Uint64(fixed[len(backupMagic)+1:]))
	saltLen := int(fixed[len(fixed)-1])
	if saltLen == 0 {
		return nil, notBackup
	}
	rest := make([]byte, saltLen+backupIDLen)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, notBackup
	}
	return &backupHeader{
		raw:     append(fixed, rest...),
		created: time.Unix(created, 0).UTC(),
		salt:    rest[:saltLen],
		fileID:  rest[saltLen:],
	}, nil
}

// CheckBackup decrypts a backup and checks it is complete and unlocks with
// the password and secret key, without changing any vault.
func CheckBackup(path, password, secretKeyHex string) (*BackupInfo, error) {
	work, err := os.MkdirTemp("", "pvault-check-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	return restoreInto(path, filepath.Join(work, "vault"), password, secretKeyHex, false)
}

// RestoreBackup recreates a vault at dir from a backup. dir must not exist
// or be empty; an existing vault is never overwritten. The secret key is
// written to dir/secret.key as on init.
func RestoreBackup(path, dir, password, secretKeyHex string) (*BackupInfo, error) {
	dir = filepath.Clean(dir) // with a trailing slash, Dir(dir) would be dir itself
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty — restore into a new folder (set VAULT_DIR) or move the old one away first", dir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	// Stage next to dir so the final step is a rename on the same disk.
	work, err := os.MkdirTemp(parent, ".pvault-restore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	staged := filepath.Join(work, "vault")
	info, err := restoreInto(path, staged, password, secretKeyHex, true)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(staged, dir); err != nil {
		return nil, err
	}
	return info, nil
}

// restoreInto decrypts a backup into target (created here) and verifies it,
// writing secret.key there too if writeKey is set.
func restoreInto(path, target, password, secretKeyHex string, writeKey bool) (*BackupInfo, error) {
	// Accept the key as copied by hand: any case, spaces or dashes.
	secretKeyHex = strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(secretKeyHex))
	sk, err := hex.DecodeString(strings.TrimSpace(secretKeyHex))
	if err != nil {
		return nil, ErrBackupUnreadable
	}
	defer zero(sk)

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	hdr, err := readBackupHeader(br)
	if err != nil {
		return nil, err
	}

	vaultKey := crypto.DeriveVaultKey([]byte(password), sk, hdr.salt)
	defer zero(vaultKey)
	key, err := crypto.DeriveSubkey(vaultKey, hdr.salt, backupKeyInfo(hdr.fileID))
	if err != nil {
		return nil, err
	}
	defer zero(key)
	sr, err := crypto.NewStreamReader(br, key, hdr.raw)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Join(target, "documents"), 0700); err != nil {
		return nil, err
	}
	if err := extractBackup(sr, target); err != nil {
		if errors.Is(err, crypto.ErrStreamCorrupt) {
			return nil, ErrBackupUnreadable
		}
		return nil, err
	}

	// The restored vault must unlock with these credentials and list no
	// document whose file is missing.
	v, err := Open(target)
	if err != nil {
		return nil, fmt.Errorf("open restored vault: %w", err)
	}
	defer v.db.Close()
	if err := v.CheckCredentials(password, secretKeyHex); err != nil {
		return nil, ErrBackupUnreadable
	}
	docs, err := v.db.ListDocuments()
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		if _, err := os.Stat(v.documentPath(d.ID)); err != nil {
			return nil, fmt.Errorf("backup is missing document %s", d.ID)
		}
	}
	fields, err := v.db.FieldCount("")
	if err != nil {
		return nil, err
	}
	if writeKey {
		if err := os.WriteFile(filepath.Join(target, "secret.key"), []byte(hex.EncodeToString(sk)+"\n"), 0600); err != nil {
			return nil, err
		}
	}
	return &BackupInfo{Created: hdr.created, Fields: fields, Documents: len(docs)}, nil
}

// extractBackup unpacks the decrypted tar stream, accepting only vault.db
// and documents/<id>.enc, then reads the stream to its authenticated end.
func extractBackup(sr io.Reader, target string) error {
	tr := tar.NewReader(sr)
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || seen[h.Name] || (h.Name != "vault.db" && !reBackupDocEnt.MatchString(h.Name)) {
			return fmt.Errorf("backup contains an unexpected entry %q", h.Name)
		}
		seen[h.Name] = true
		out, err := os.OpenFile(filepath.Join(target, filepath.FromSlash(h.Name)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	// tar stops at its end marker; drain so the final chunk is authenticated.
	if _, err := io.Copy(io.Discard, sr); err != nil {
		return err
	}
	if !seen["vault.db"] {
		return errors.New("backup contains no database")
	}
	return nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
