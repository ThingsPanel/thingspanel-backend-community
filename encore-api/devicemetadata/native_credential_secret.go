package devicemetadata

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	credentialAADVersion     = 1
	credentialCipherSuite    = "AES-256-GCM"
	maxCredentialSecretBytes = 8192
	credentialKeyringEnv     = "METADATA_CREDENTIAL_KEYRING_B64"
	credentialActiveKeyEnv   = "METADATA_CREDENTIAL_ACTIVE_KEY_REF"
)

var (
	ErrCredentialKeyUnavailable   = errors.New("device credential key is unavailable")
	ErrCredentialProtectionFailed = errors.New("device credential protection failed")
)

const credentialFingerprintDomain = "thingspanel/device-credential/fingerprint/v1\x00"

type CredentialPurpose string

const (
	CredentialPurposeDeviceAccess       CredentialPurpose = "device_access"
	CredentialPurposeTemplateEnrollment CredentialPurpose = "template_enrollment"
)

type CredentialUse string

const (
	CredentialUseMaterial       CredentialUse = "material"
	CredentialUseReplayResponse CredentialUse = "replay_response"
)

type CredentialOwnerKind string

const (
	CredentialOwnerDevice   CredentialOwnerKind = "device"
	CredentialOwnerTemplate CredentialOwnerKind = "template"
)

// CredentialAAD binds protected bytes to tenant, owner, credential, business
// purpose, and use. ReplayBinding is empty for material and a canonical scope
// hash for replay responses; it is not the business purpose or secret itself.
type CredentialAAD struct {
	TenantID      string
	OwnerKind     CredentialOwnerKind
	OwnerID       string
	CredentialID  string
	Purpose       CredentialPurpose
	Use           CredentialUse
	ReplayBinding string
}

type ProtectedCredential struct {
	Ciphertext  []byte
	KeyID       string
	CipherSuite string
	Nonce       []byte
}

type CredentialFingerprint struct {
	KeyID       string
	Fingerprint []byte
}

// CredentialSecretProtector owns only cryptographic primitives. It performs
// no persistence, API authorization, reveal policy, or lifecycle transitions.
type CredentialSecretProtector struct {
	activeKeyRef string
	keys         map[string][]byte
}

func NewCredentialSecretProtector(activeKeyRef string, keyring map[string][]byte) (*CredentialSecretProtector, error) {
	activeKeyRef = strings.TrimSpace(activeKeyRef)
	if !validCredentialKeyRef(activeKeyRef) || len(keyring) == 0 {
		return nil, ErrCredentialKeyUnavailable
	}
	copyKeys := make(map[string][]byte, len(keyring))
	for ref, key := range keyring {
		if !validCredentialKeyRef(ref) || len(key) != 32 {
			return nil, ErrCredentialKeyUnavailable
		}
		copyKeys[ref] = append([]byte(nil), key...)
	}
	if _, ok := copyKeys[activeKeyRef]; !ok {
		return nil, ErrCredentialKeyUnavailable
	}
	return &CredentialSecretProtector{activeKeyRef: activeKeyRef, keys: copyKeys}, nil
}

// NewCredentialSecretProtectorFromEnv requires an explicit keyring and active
// key reference. It never creates or bootstraps a key implicitly.
func NewCredentialSecretProtectorFromEnv() (*CredentialSecretProtector, error) {
	raw := strings.TrimSpace(os.Getenv(credentialKeyringEnv))
	active := strings.TrimSpace(os.Getenv(credentialActiveKeyEnv))
	if raw == "" || active == "" {
		return nil, ErrCredentialKeyUnavailable
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > 64<<10 {
		return nil, ErrCredentialKeyUnavailable
	}
	var encoded map[string]string
	if err := json.Unmarshal(decoded, &encoded); err != nil || len(encoded) == 0 {
		return nil, ErrCredentialKeyUnavailable
	}
	keys := make(map[string][]byte, len(encoded))
	for ref, value := range encoded {
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(key) != 32 {
			return nil, ErrCredentialKeyUnavailable
		}
		keys[ref] = key
	}
	return NewCredentialSecretProtector(active, keys)
}

func (p *CredentialSecretProtector) Seal(aad CredentialAAD, plaintext []byte) (ProtectedCredential, error) {
	if p == nil || validateCredentialAAD(aad) != nil || len(plaintext) == 0 ||
		(aad.Use == CredentialUseMaterial && len(plaintext) > maxCredentialSecretBytes) {
		return ProtectedCredential{}, ErrCredentialProtectionFailed
	}
	key, ok := p.keys[p.activeKeyRef]
	if !ok {
		return ProtectedCredential{}, ErrCredentialKeyUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ProtectedCredential{}, ErrCredentialProtectionFailed
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ProtectedCredential{}, ErrCredentialProtectionFailed
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return ProtectedCredential{}, ErrCredentialProtectionFailed
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, canonicalCredentialAAD(aad))
	return ProtectedCredential{Ciphertext: ciphertext, KeyID: p.activeKeyRef, CipherSuite: credentialCipherSuite, Nonce: nonce}, nil
}

func (p *CredentialSecretProtector) Open(aad CredentialAAD, protected ProtectedCredential) ([]byte, error) {
	if p == nil || validateCredentialAAD(aad) != nil || protected.CipherSuite != credentialCipherSuite ||
		len(protected.Nonce) != 12 || len(protected.Ciphertext) < 17 || !validCredentialKeyRef(protected.KeyID) ||
		(aad.Use == CredentialUseMaterial && (len(protected.Ciphertext) < 17 || len(protected.Ciphertext) > maxCredentialSecretBytes+16)) {
		return nil, ErrCredentialProtectionFailed
	}
	key, ok := p.keys[protected.KeyID]
	if !ok {
		return nil, ErrCredentialKeyUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCredentialProtectionFailed
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCredentialProtectionFailed
	}
	plaintext, err := gcm.Open(nil, protected.Nonce, protected.Ciphertext, canonicalCredentialAAD(aad))
	if err != nil {
		return nil, ErrCredentialProtectionFailed
	}
	return plaintext, nil
}

// Fingerprint returns keyed summary metadata. It is not a password verifier;
// low-entropy credentials must use the separately encoded verifier mode.
func (p *CredentialSecretProtector) Fingerprint(aad CredentialAAD, secret []byte) (CredentialFingerprint, error) {
	if p == nil || validateCredentialAAD(aad) != nil || len(secret) == 0 {
		return CredentialFingerprint{}, ErrCredentialProtectionFailed
	}
	key, ok := p.keys[p.activeKeyRef]
	if !ok {
		return CredentialFingerprint{}, ErrCredentialKeyUnavailable
	}
	return CredentialFingerprint{KeyID: p.activeKeyRef, Fingerprint: credentialFingerprint(key, aad, secret)}, nil
}

func VerifyCredentialFingerprint(aad CredentialAAD, secret []byte, stored CredentialFingerprint, keyring map[string][]byte) (bool, error) {
	if validateCredentialAAD(aad) != nil || len(secret) == 0 || !validCredentialKeyRef(stored.KeyID) || len(stored.Fingerprint) != sha256.Size {
		return false, ErrCredentialProtectionFailed
	}
	key, ok := keyring[stored.KeyID]
	if !ok || len(key) != 32 {
		return false, ErrCredentialKeyUnavailable
	}
	return hmac.Equal(credentialFingerprint(key, aad, secret), stored.Fingerprint), nil
}

func credentialFingerprint(key []byte, aad CredentialAAD, secret []byte) []byte {
	derive := hmac.New(sha256.New, key)
	_, _ = derive.Write([]byte(credentialFingerprintDomain))
	fingerprintKey := derive.Sum(nil)
	mac := hmac.New(sha256.New, fingerprintKey)
	_, _ = mac.Write(canonicalCredentialAAD(aad))
	_, _ = mac.Write(secret)
	return mac.Sum(nil)
}

func GenerateCredentialSecret() (string, error) {
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return "", ErrCredentialProtectionFailed
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

func validateCredentialAAD(aad CredentialAAD) error {
	if !validCredentialUUID(aad.TenantID) || !validCredentialUUID(aad.OwnerID) || !validCredentialUUID(aad.CredentialID) {
		return ErrCredentialProtectionFailed
	}
	if aad.OwnerKind == CredentialOwnerDevice {
		if aad.Purpose != CredentialPurposeDeviceAccess {
			return ErrCredentialProtectionFailed
		}
	} else if aad.OwnerKind == CredentialOwnerTemplate {
		if aad.Purpose != CredentialPurposeTemplateEnrollment {
			return ErrCredentialProtectionFailed
		}
	} else {
		return ErrCredentialProtectionFailed
	}
	if aad.Use != CredentialUseMaterial && aad.Use != CredentialUseReplayResponse {
		return ErrCredentialProtectionFailed
	}
	if aad.Use == CredentialUseMaterial && aad.ReplayBinding != "" {
		return ErrCredentialProtectionFailed
	}
	if aad.Use == CredentialUseReplayResponse {
		decoded, err := hex.DecodeString(aad.ReplayBinding)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != aad.ReplayBinding {
			return ErrCredentialProtectionFailed
		}
	}
	return nil
}

func canonicalCredentialAAD(aad CredentialAAD) []byte {
	fields := []string{
		fmt.Sprint(credentialAADVersion), string(aad.OwnerKind), aad.TenantID,
		aad.OwnerID, aad.CredentialID, string(aad.Purpose), string(aad.Use), aad.ReplayBinding,
	}
	var out []byte
	for _, field := range fields {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		out = append(out, size[:]...)
		out = append(out, field...)
	}
	return out
}

func validCredentialUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && strings.EqualFold(parsed.String(), value)
}

func validCredentialKeyRef(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
