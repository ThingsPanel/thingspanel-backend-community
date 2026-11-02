package fileassets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
)

const (
	SecretBoxVersion      uint32 = 1
	MaxSecretBoxPlaintext        = 64 * 1024
)

type SecretBoxErrorCode string

const (
	SecretBoxConfigurationError SecretBoxErrorCode = "configuration"
	SecretBoxIntegrityError     SecretBoxErrorCode = "integrity"
	SecretBoxUnsupportedVersion SecretBoxErrorCode = "unsupported_version"
	SecretBoxPlaintextTooLarge  SecretBoxErrorCode = "plaintext_too_large"
)

// SecretBoxError contains only a stable safe code; it never includes key
// material, plaintext, caller AAD, or raw cryptographic errors.
type SecretBoxError struct {
	Code SecretBoxErrorCode
}

func (e *SecretBoxError) Error() string {
	switch e.Code {
	case SecretBoxConfigurationError:
		return "secret box configuration is invalid"
	case SecretBoxIntegrityError:
		return "secret box envelope authentication failed"
	case SecretBoxUnsupportedVersion:
		return "secret box envelope version is unsupported"
	case SecretBoxPlaintextTooLarge:
		return "secret box payload exceeds the size limit"
	default:
		return "secret box operation failed"
	}
}

// Envelope is safe for persistence as JSON. Nonce and Ciphertext encode as
// base64; plaintext is never included.
type Envelope struct {
	Version    uint32 `json:"version"`
	KeyID      string `json:"keyId"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

// SecretBox encrypts small persisted capabilities using AES-256-GCM.
type SecretBox struct {
	activeKeyID string
	keys        map[string][32]byte
}

// NewSecretBox validates and copies an externally supplied keyring. The active
// key is used only for Seal; all configured keys remain available to Open.
func NewSecretBox(activeKeyID string, keys map[string][]byte) (*SecretBox, error) {
	if !validKeyID(activeKeyID) || len(keys) == 0 {
		return nil, secretBoxError(SecretBoxConfigurationError)
	}
	keyring := make(map[string][32]byte, len(keys))
	for keyID, key := range keys {
		if !validKeyID(keyID) || len(key) != aes.BlockSize*2 {
			return nil, secretBoxError(SecretBoxConfigurationError)
		}
		var copied [32]byte
		copy(copied[:], key)
		keyring[keyID] = copied
	}
	if _, ok := keyring[activeKeyID]; !ok {
		return nil, secretBoxError(SecretBoxConfigurationError)
	}
	return &SecretBox{activeKeyID: activeKeyID, keys: keyring}, nil
}

// Seal encrypts plaintext with a fresh GCM nonce and authenticates the
// version/key ID header together with caller-provided associated data.
func (box *SecretBox) Seal(plaintext, aad []byte) (Envelope, error) {
	if box == nil || len(box.keys) == 0 {
		return Envelope{}, secretBoxError(SecretBoxConfigurationError)
	}
	if len(plaintext) > MaxSecretBoxPlaintext {
		return Envelope{}, secretBoxError(SecretBoxPlaintextTooLarge)
	}
	key, ok := box.keys[box.activeKeyID]
	if !ok {
		return Envelope{}, secretBoxError(SecretBoxConfigurationError)
	}
	gcm, err := newGCM(key[:])
	if err != nil {
		return Envelope{}, secretBoxError(SecretBoxConfigurationError)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, secretBoxError(SecretBoxConfigurationError)
	}
	envelope := Envelope{Version: SecretBoxVersion, KeyID: box.activeKeyID, Nonce: nonce}
	envelope.Ciphertext = gcm.Seal(nil, nonce, plaintext, envelopeAAD(envelope.Version, envelope.KeyID, aad))
	return envelope, nil
}

// Open authenticates the envelope header and caller-provided associated data
// before returning plaintext. The caller must enforce its own authorization
// and must not expose plaintext errors or content to untrusted callers.
func (box *SecretBox) Open(envelope Envelope, aad []byte) ([]byte, error) {
	if envelope.Version != SecretBoxVersion {
		return nil, secretBoxError(SecretBoxUnsupportedVersion)
	}
	if box == nil || len(box.keys) == 0 {
		return nil, secretBoxError(SecretBoxConfigurationError)
	}
	if !validKeyID(envelope.KeyID) {
		return nil, secretBoxError(SecretBoxIntegrityError)
	}
	key, ok := box.keys[envelope.KeyID]
	if !ok {
		return nil, secretBoxError(SecretBoxConfigurationError)
	}
	gcm, err := newGCM(key[:])
	if err != nil {
		return nil, secretBoxError(SecretBoxConfigurationError)
	}
	if len(envelope.Ciphertext) > MaxSecretBoxPlaintext+gcm.Overhead() {
		return nil, secretBoxError(SecretBoxPlaintextTooLarge)
	}
	if len(envelope.Nonce) != gcm.NonceSize() || len(envelope.Ciphertext) < gcm.Overhead() {
		return nil, secretBoxError(SecretBoxIntegrityError)
	}
	plaintext, err := gcm.Open(nil, envelope.Nonce, envelope.Ciphertext, envelopeAAD(envelope.Version, envelope.KeyID, aad))
	if err != nil {
		return nil, secretBoxError(SecretBoxIntegrityError)
	}
	if len(plaintext) > MaxSecretBoxPlaintext {
		return nil, secretBoxError(SecretBoxPlaintextTooLarge)
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func envelopeAAD(version uint32, keyID string, aad []byte) []byte {
	const magic = "thingspanel-fileassets-secretbox\x00"
	buf := make([]byte, len(magic)+4+4+len(keyID)+8+len(aad))
	offset := copy(buf, magic)
	binary.BigEndian.PutUint32(buf[offset:offset+4], version)
	offset += 4
	binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(len(keyID)))
	offset += 4
	offset += copy(buf[offset:], keyID)
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(aad)))
	offset += 8
	copy(buf[offset:], aad)
	return buf
}

func validKeyID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func secretBoxError(code SecretBoxErrorCode) error {
	return &SecretBoxError{Code: code}
}
