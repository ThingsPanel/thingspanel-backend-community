package notification

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const snapshotAADVersion = 1

var (
	ErrPrivacyGuardClosed = errors.New("notification privacy guard is closed")
	ErrSnapshotKeyMissing = errors.New("notification snapshot key is unavailable")
)

// AESGCMProtector encrypts notification snapshots with an externally supplied
// keyring. Key material is never included in a ProtectedSnapshot or error.
type AESGCMProtector struct {
	activeKeyRef string
	keys         map[string][]byte
}

func NewAESGCMProtector(activeKeyRef string, keyring map[string][]byte) (*AESGCMProtector, error) {
	activeKeyRef = strings.TrimSpace(activeKeyRef)
	if activeKeyRef == "" || len(activeKeyRef) > 128 || len(keyring) == 0 {
		return nil, ErrSnapshotKeyMissing
	}
	copyKeys := make(map[string][]byte, len(keyring))
	for ref, key := range keyring {
		if strings.TrimSpace(ref) != ref || ref == "" || len(ref) > 128 || len(key) != 32 {
			return nil, ErrSnapshotKeyMissing
		}
		copyKeys[ref] = append([]byte(nil), key...)
	}
	if _, ok := copyKeys[activeKeyRef]; !ok {
		return nil, ErrSnapshotKeyMissing
	}
	return &AESGCMProtector{activeKeyRef: activeKeyRef, keys: copyKeys}, nil
}

// NewAESGCMProtectorFromEnv reads base64-encoded JSON key material from
// NOTIFICATION_KEYRING_B64. The JSON maps stable key references to base64
// 32-byte AES keys. Errors never echo environment values or key material.
func NewAESGCMProtectorFromEnv() (*AESGCMProtector, error) {
	raw := strings.TrimSpace(os.Getenv("NOTIFICATION_KEYRING_B64"))
	active := strings.TrimSpace(os.Getenv("NOTIFICATION_ACTIVE_KEY_REF"))
	if raw == "" || active == "" {
		return nil, ErrSnapshotKeyMissing
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(decoded) > 64<<10 {
		return nil, ErrSnapshotKeyMissing
	}
	var encoded map[string]string
	if err := json.Unmarshal(decoded, &encoded); err != nil || len(encoded) == 0 {
		return nil, ErrSnapshotKeyMissing
	}
	keys := make(map[string][]byte, len(encoded))
	for ref, value := range encoded {
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(key) != 32 {
			return nil, ErrSnapshotKeyMissing
		}
		keys[ref] = key
	}
	return NewAESGCMProtector(active, keys)
}

func (p *AESGCMProtector) Seal(ctx context.Context, aad SnapshotAAD, plaintext []byte) (ProtectedSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ProtectedSnapshot{}, ErrSnapshotProtectionFailed
	}
	if err := aad.validate(); err != nil || p == nil {
		return ProtectedSnapshot{}, ErrSnapshotProtectionFailed
	}
	key := p.keys[p.activeKeyRef]
	block, err := aes.NewCipher(key)
	if err != nil {
		return ProtectedSnapshot{}, ErrSnapshotProtectionFailed
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ProtectedSnapshot{}, ErrSnapshotProtectionFailed
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return ProtectedSnapshot{}, ErrSnapshotProtectionFailed
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, canonicalSnapshotAAD(aad))
	return ProtectedSnapshot{KeyRef: p.activeKeyRef, Ciphertext: sealed, AADVersion: snapshotAADVersion}, nil
}

func (p *AESGCMProtector) Open(ctx context.Context, aad SnapshotAAD, protected ProtectedSnapshot) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrSnapshotProtectionFailed
	}
	if err := aad.validate(); err != nil || p == nil || protected.AADVersion != snapshotAADVersion {
		return nil, ErrSnapshotProtectionFailed
	}
	key, ok := p.keys[protected.KeyRef]
	if !ok {
		return nil, ErrSnapshotKeyMissing
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrSnapshotProtectionFailed
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(protected.Ciphertext) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrSnapshotProtectionFailed
	}
	nonce := protected.Ciphertext[:gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, protected.Ciphertext[gcm.NonceSize():], canonicalSnapshotAAD(aad))
	if err != nil {
		return nil, ErrSnapshotProtectionFailed
	}
	return plain, nil
}

// canonicalSnapshotAAD uses length-prefixed fields so no separator or encoding
// ambiguity can make two resource/purpose/slot contexts share authentication.
func canonicalSnapshotAAD(aad SnapshotAAD) []byte {
	fields := []string{aad.ScopeKind, aad.TenantID, aad.ResourceID, fmt.Sprint(aad.Version), aad.Purpose, aad.Slot}
	var out []byte
	for _, field := range fields {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		out = append(out, length[:]...)
		out = append(out, field...)
	}
	return out
}

// PrivacyGuard proves that the explicitly selected self-hosted Encore infra
// config has no tracing provider. The deployment must set this path to the
// same immutable file used for ENCORE_INFRA_CONFIG_PATH. In application mode
// Encore removes ENCORE_* variables from os.Environ, so this non-ENCORE mirror
// is required and its digest should be pinned by the deployment manifest.
type PrivacyGuard struct {
	infraPath string
	infraHash [32]byte
	verified  bool
	readEnv   func() ([]byte, error)
	linux     bool
}

func NewPrivacyGuardFromEnv() (*PrivacyGuard, error) {
	if runtime.GOOS != "linux" {
		return nil, ErrPrivacyGuardClosed
	}
	return newPrivacyGuard(readOriginalProcessEnvironment, runtime.GOOS)
}

func readOriginalProcessEnvironment() ([]byte, error) {
	return os.ReadFile("/proc/self/environ")
}

func newPrivacyGuard(readEnv func() ([]byte, error), goos string) (*PrivacyGuard, error) {
	if readEnv == nil || goos != "linux" {
		return nil, ErrPrivacyGuardClosed
	}
	env, err := readEnv()
	if err != nil {
		return nil, ErrPrivacyGuardClosed
	}
	values, err := parseOriginalEnvironment(env)
	if err != nil {
		return nil, ErrPrivacyGuardClosed
	}
	path, err := validatePrivacyEnvironment(values)
	if err != nil {
		return nil, ErrPrivacyGuardClosed
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > 4<<20 {
		return nil, ErrPrivacyGuardClosed
	}
	if !infraConfigDisablesTracing(data) {
		return nil, ErrPrivacyGuardClosed
	}
	hash := sha256.Sum256(data)
	return &PrivacyGuard{infraPath: path, infraHash: hash, verified: true, readEnv: readEnv, linux: goos == "linux"}, nil
}

func (g *PrivacyGuard) Check() error {
	if g == nil || !g.verified || g.infraPath == "" || g.readEnv == nil || !g.linux {
		return ErrPrivacyGuardClosed
	}
	env, err := g.readEnv()
	if err != nil {
		return ErrPrivacyGuardClosed
	}
	values, err := parseOriginalEnvironment(env)
	if err != nil {
		return ErrPrivacyGuardClosed
	}
	path, err := validatePrivacyEnvironment(values)
	if err != nil || path != g.infraPath {
		return ErrPrivacyGuardClosed
	}
	data, err := os.ReadFile(g.infraPath)
	if err != nil || len(data) == 0 || len(data) > 4<<20 {
		return ErrPrivacyGuardClosed
	}
	current := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(current[:], g.infraHash[:]) != 1 || !infraConfigDisablesTracing(data) {
		return ErrPrivacyGuardClosed
	}
	return nil
}

func parseOriginalEnvironment(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || raw[len(raw)-1] != 0 {
		return nil, ErrPrivacyGuardClosed
	}
	values := make(map[string]string)
	for _, entry := range strings.Split(string(raw[:len(raw)-1]), "\x00") {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			return nil, ErrPrivacyGuardClosed
		}
		if isPrivacyEnvironmentKey(name) {
			if _, duplicate := values[name]; duplicate {
				return nil, ErrPrivacyGuardClosed
			}
			values[name] = value
		}
	}
	return values, nil
}

func isPrivacyEnvironmentKey(name string) bool {
	return strings.HasPrefix(name, "ENCORE_") || strings.HasPrefix(name, "OTEL_") || name == "NOTIFICATION_ENCORE_INFRA_CONFIG_PATH"
}

func validatePrivacyEnvironment(values map[string]string) (string, error) {
	infra := values["ENCORE_INFRA_CONFIG_PATH"]
	mirror := values["NOTIFICATION_ENCORE_INFRA_CONFIG_PATH"]
	if infra == "" || mirror == "" || !filepath.IsAbs(infra) || filepath.Clean(infra) != infra || mirror != infra {
		return "", ErrPrivacyGuardClosed
	}
	for _, name := range []string{"ENCORE_RUNTIME_CONFIG", "ENCORE_RUNTIME_CONFIG_PATH"} {
		if _, exists := values[name]; exists {
			return "", ErrPrivacyGuardClosed
		}
	}
	for name := range values {
		if strings.HasPrefix(name, "ENCORE_TRACE_") || strings.HasPrefix(name, "OTEL_") {
			return "", ErrPrivacyGuardClosed
		}
	}
	return infra, nil
}

func infraConfigDisablesTracing(data []byte) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return false
	}
	// Encore v1.58.6's Go and Rust infra-to-runtime conversion does not map
	// trace_endpoint/sampling from InfraConfig and creates no trace provider.
	// Reject trace-shaped unknown fields so a future config cannot be silently
	// interpreted differently by this guard and the runtime.
	for key, value := range raw {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "trace") || strings.Contains(lower, "observability") {
			if len(strings.TrimSpace(string(value))) != 0 && string(value) != "null" && string(value) != "[]" && string(value) != "{}" {
				return false
			}
		}
	}
	return true
}

// SnapshotDigest is used only for safe idempotency hashing. Callers must hash
// the canonical request bytes before removing secrets from the request.
func SnapshotDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// stableSecretFields returns a deterministic copy for schema-driven config
// handling and avoids exposing caller-owned mutable slices across layers.
func stableSecretFields(fields []string) []string {
	out := append([]string(nil), fields...)
	sort.Strings(out)
	return out
}
