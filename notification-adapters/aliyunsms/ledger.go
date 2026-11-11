package aliyunsms

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const ledgerAAD = "thingspanel.aliyunsms.send-ledger.v1"

var (
	ErrLedgerConflict = errors.New("accepted SMS correlation conflicts with existing ledger record")
	ErrLedgerClosed   = errors.New("SMS ledger is closed")
)

// SendLedgerRecord binds an accepted provider BizId and recipient to the exact
// core delivery that caused it. The persisted copy is always encrypted.
type SendLedgerRecord struct {
	QueueNamespace string    `json:"queue_namespace"`
	BizID          string    `json:"biz_id"`
	Phone          string    `json:"phone"`
	TenantID       string    `json:"tenant_id"`
	InstanceID     string    `json:"instance_id"`
	DeliveryID     string    `json:"delivery_id"`
	ConfigVersion  int64     `json:"config_version"`
	SentAt         time.Time `json:"sent_at"`
}

// SendLedger is process-local and concurrency safe. OpenSendLedger obtains an
// exclusive file lock; all senders and receipt consumers in a process must
// share the same handle.
type SendLedger interface {
	RecordAccepted(SendLedgerRecord) error
	LookupAccepted(queueNamespace, bizID, phone string) (SendLedgerRecord, bool, error)
	Close() error
}

type encryptedSendLedger struct {
	mu       sync.Mutex
	dir      string
	dataPath string
	lockFile *os.File
	aead     cipher.AEAD
	closed   bool
}

type ledgerDisk struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// OpenSendLedger creates or opens an encrypted ledger. The caller supplies a
// dedicated 32-byte key from its secret manager; the key is never written.
func OpenSendLedger(path string, key []byte) (SendLedger, error) {
	if len(key) != 32 || strings.TrimSpace(path) == "" {
		return nil, errors.New("SMS ledger path and 32-byte encryption key are required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid SMS ledger path")
	}
	dir := filepath.Dir(abs)
	dirInfo, dirErr := os.Lstat(dir)
	if dirErr == nil && dirInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("SMS ledger directory cannot be a symlink")
	}
	if errors.Is(dirErr, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, errors.New("could not prepare SMS ledger directory")
		}
	} else if dirErr != nil {
		return nil, errors.New("could not inspect SMS ledger directory")
	}
	dirInfo, dirErr = os.Lstat(dir)
	if dirErr != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("SMS ledger directory must be a real directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, errors.New("could not secure SMS ledger directory")
	}
	dirInfo, dirErr = os.Lstat(dir)
	if dirErr != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || dirInfo.Mode().Perm()&0077 != 0 {
		return nil, errors.New("SMS ledger directory must be private and not a symlink")
	}
	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("SMS ledger path cannot be a symlink")
	} else if err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("SMS ledger path must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("could not inspect SMS ledger path")
	} else if err == nil {
		if err := os.Chmod(abs, 0600); err != nil {
			return nil, errors.New("could not secure SMS ledger file")
		}
	}
	lockPath := abs + ".lock"
	if info, err := os.Lstat(lockPath); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, errors.New("SMS ledger lock path is unsafe")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("could not inspect SMS ledger lock")
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.New("could not open SMS ledger lock")
	}
	if err := os.Chmod(lockPath, 0600); err != nil {
		_ = lock.Close()
		return nil, errors.New("could not secure SMS ledger lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errors.New("SMS ledger is already open by another process")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		_ = unlockAndClose(lock)
		return nil, errors.New("could not initialize SMS ledger encryption")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		_ = unlockAndClose(lock)
		return nil, errors.New("could not initialize SMS ledger encryption")
	}
	l := &encryptedSendLedger{dir: dir, dataPath: abs, lockFile: lock, aead: aead}
	if _, err := l.readRecords(); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

func (l *encryptedSendLedger) RecordAccepted(record SendLedgerRecord) error {
	if !validLedgerRecord(record) {
		return errors.New("invalid accepted SMS ledger record")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrLedgerClosed
	}
	records, err := l.readRecords()
	if err != nil {
		return err
	}
	key := ledgerRecordKey(record.QueueNamespace, record.BizID, record.Phone)
	if existing, ok := records[key]; ok {
		var old SendLedgerRecord
		if err := json.Unmarshal([]byte(existing), &old); err != nil {
			return errors.New("SMS ledger record is invalid")
		}
		if sameLedgerScope(old, record) {
			return nil
		}
		return ErrLedgerConflict
	}
	serialized, err := json.Marshal(record)
	if err != nil {
		return errors.New("could not encode SMS ledger record")
	}
	records[key] = string(serialized)
	return l.writeRecords(records)
}

func (l *encryptedSendLedger) LookupAccepted(queueNamespace, bizID, phone string) (SendLedgerRecord, bool, error) {
	if queueNamespace == "" || bizID == "" || phone == "" {
		return SendLedgerRecord{}, false, errors.New("invalid SMS ledger lookup")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return SendLedgerRecord{}, false, ErrLedgerClosed
	}
	records, err := l.readRecords()
	if err != nil {
		return SendLedgerRecord{}, false, err
	}
	serialized, ok := records[ledgerRecordKey(queueNamespace, bizID, phone)]
	if !ok {
		return SendLedgerRecord{}, false, nil
	}
	var record SendLedgerRecord
	if err := json.Unmarshal([]byte(serialized), &record); err != nil {
		return SendLedgerRecord{}, false, errors.New("SMS ledger record is invalid")
	}
	return record, true, nil
}

func (l *encryptedSendLedger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return unlockAndClose(l.lockFile)
}

func (l *encryptedSendLedger) readRecords() (map[string]string, error) {
	data, err := os.ReadFile(l.dataPath)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, errors.New("could not read SMS ledger")
	}
	info, err := os.Lstat(l.dataPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("SMS ledger file is not a regular file")
	}
	var disk ledgerDisk
	if err := json.Unmarshal(data, &disk); err != nil || disk.Version != 1 || disk.Nonce == "" || disk.Ciphertext == "" {
		return nil, errors.New("SMS ledger is corrupt or unsupported")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(disk.Nonce)
	if err != nil || len(nonce) != l.aead.NonceSize() {
		return nil, errors.New("SMS ledger is corrupt or unsupported")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(disk.Ciphertext)
	if err != nil {
		return nil, errors.New("SMS ledger is corrupt or unsupported")
	}
	plaintext, err := l.aead.Open(nil, nonce, ciphertext, []byte(ledgerAAD))
	if err != nil {
		return nil, errors.New("SMS ledger authentication failed")
	}
	var records map[string]string
	if err := json.Unmarshal(plaintext, &records); err != nil || records == nil {
		return nil, errors.New("SMS ledger payload is invalid")
	}
	return records, nil
}

func (l *encryptedSendLedger) writeRecords(records map[string]string) error {
	plaintext, err := json.Marshal(records)
	if err != nil {
		return errors.New("could not encode SMS ledger")
	}
	nonce := make([]byte, l.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return errors.New("could not secure SMS ledger")
	}
	ciphertext := l.aead.Seal(nil, nonce, plaintext, []byte(ledgerAAD))
	disk, err := json.Marshal(ledgerDisk{Version: 1, Nonce: base64.RawStdEncoding.EncodeToString(nonce), Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext)})
	if err != nil {
		return errors.New("could not encode SMS ledger")
	}
	tmp, err := os.CreateTemp(l.dir, ".sms-ledger-*")
	if err != nil {
		return errors.New("could not write SMS ledger")
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return errors.New("could not secure SMS ledger")
	}
	if _, err := tmp.Write(disk); err != nil {
		_ = tmp.Close()
		return errors.New("could not write SMS ledger")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errors.New("could not persist SMS ledger")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("could not persist SMS ledger")
	}
	if err := os.Rename(tmpPath, l.dataPath); err != nil {
		return errors.New("could not commit SMS ledger")
	}
	dir, err := os.Open(l.dir)
	if err != nil {
		return errors.New("could not sync SMS ledger directory")
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return errors.New("could not sync SMS ledger directory")
	}
	return nil
}

func validLedgerRecord(r SendLedgerRecord) bool {
	return r.QueueNamespace != "" && r.BizID != "" && r.Phone != "" && r.TenantID != "" && r.InstanceID != "" && r.DeliveryID != "" && r.ConfigVersion > 0 && !r.SentAt.IsZero()
}

func sameLedgerScope(a, b SendLedgerRecord) bool {
	return a.QueueNamespace == b.QueueNamespace && a.BizID == b.BizID && a.Phone == b.Phone && a.TenantID == b.TenantID && a.InstanceID == b.InstanceID && a.DeliveryID == b.DeliveryID && a.ConfigVersion == b.ConfigVersion
}

func ledgerRecordKey(namespace, bizID, phone string) string {
	h := sha256.New()
	for _, value := range []string{namespace, bizID, phone} {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = io.WriteString(h, value)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func queueNamespace(accessKeyID, region string) string {
	h := sha256.New()
	for _, value := range []string{accessKeyID, region, "SmsReport"} {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = io.WriteString(h, value)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func unlockAndClose(file *os.File) error {
	if file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return errors.New("could not release SMS ledger lock")
	}
	if closeErr != nil {
		return errors.New("could not close SMS ledger lock")
	}
	return nil
}
