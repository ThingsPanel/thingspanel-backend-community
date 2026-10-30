package fileassets

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretBoxRoundTripAndFreshNonce(t *testing.T) {
	box := newTestSecretBox(t, "active", map[string][]byte{"active": bytes.Repeat([]byte{0x11}, 32)})
	plaintext := []byte("short-lived signed download URL: sentinel-secret-value")
	aad := []byte("tenant=tenant-1;asset=asset-1;purpose=image-url")

	first, err := box.Seal(plaintext, aad)
	require.NoError(t, err)
	second, err := box.Seal(plaintext, aad)
	require.NoError(t, err)
	require.Equal(t, SecretBoxVersion, first.Version)
	require.Equal(t, "active", first.KeyID)
	require.Len(t, first.Nonce, 12)
	require.NotEqual(t, first.Nonce, second.Nonce)
	require.NotEqual(t, first.Ciphertext, second.Ciphertext)

	opened, err := box.Open(first, aad)
	require.NoError(t, err)
	require.Equal(t, plaintext, opened)
	require.NotContains(t, string(first.Ciphertext), string(plaintext))
}

func TestSecretBoxSupportsKeyRotationAndAuthenticatesAAD(t *testing.T) {
	oldKey := bytes.Repeat([]byte{0x21}, 32)
	newKey := bytes.Repeat([]byte{0x42}, 32)
	oldBox := newTestSecretBox(t, "key-v1", map[string][]byte{"key-v1": oldKey})
	oldEnvelope, err := oldBox.Seal([]byte("persisted lease body"), []byte("tenant=1;asset=2;purpose=lease"))
	require.NoError(t, err)

	rotated := newTestSecretBox(t, "key-v2", map[string][]byte{"key-v1": oldKey, "key-v2": newKey})
	opened, err := rotated.Open(oldEnvelope, []byte("tenant=1;asset=2;purpose=lease"))
	require.NoError(t, err)
	require.Equal(t, "persisted lease body", string(opened))
	newEnvelope, err := rotated.Seal([]byte("new response"), []byte("tenant=1;asset=2;purpose=lease"))
	require.NoError(t, err)
	require.Equal(t, "key-v2", newEnvelope.KeyID)

	for _, wrongAAD := range [][]byte{
		[]byte("tenant=other;asset=2;purpose=lease"),
		[]byte("tenant=1;asset=other;purpose=lease"),
		[]byte("tenant=1;asset=2;purpose=other"),
	} {
		_, err := rotated.Open(oldEnvelope, wrongAAD)
		requireSecretBoxError(t, err, SecretBoxIntegrityError, "persisted lease body", string(wrongAAD))
	}
}

func TestSecretBoxDetectsEnvelopeTampering(t *testing.T) {
	box := newTestSecretBox(t, "active", map[string][]byte{
		"active": bytes.Repeat([]byte{0x31}, 32),
		"other":  bytes.Repeat([]byte{0x32}, 32),
	})
	envelope, err := box.Seal([]byte("do-not-leak-plaintext"), []byte("tenant=1;asset=2"))
	require.NoError(t, err)

	tamperedNonce := cloneEnvelope(envelope)
	tamperedNonce.Nonce[0] ^= 0xff
	_, err = box.Open(tamperedNonce, []byte("tenant=1;asset=2"))
	requireSecretBoxError(t, err, SecretBoxIntegrityError, "do-not-leak-plaintext")

	tamperedHeader := cloneEnvelope(envelope)
	tamperedHeader.KeyID = "other"
	_, err = box.Open(tamperedHeader, []byte("tenant=1;asset=2"))
	requireSecretBoxError(t, err, SecretBoxIntegrityError, "do-not-leak-plaintext")

	tamperedCiphertext := cloneEnvelope(envelope)
	tamperedCiphertext.Ciphertext[len(tamperedCiphertext.Ciphertext)-1] ^= 0x01
	_, err = box.Open(tamperedCiphertext, []byte("tenant=1;asset=2"))
	requireSecretBoxError(t, err, SecretBoxIntegrityError, "do-not-leak-plaintext")

	badNonceSize := cloneEnvelope(envelope)
	badNonceSize.Nonce = badNonceSize.Nonce[:len(badNonceSize.Nonce)-1]
	_, err = box.Open(badNonceSize, []byte("tenant=1;asset=2"))
	requireSecretBoxError(t, err, SecretBoxIntegrityError, "do-not-leak-plaintext")
}

func TestSecretBoxHasFixedSafeErrorsForConfigurationVersionAndLimits(t *testing.T) {
	_, err := NewSecretBox("missing", map[string][]byte{"known": bytes.Repeat([]byte{0x10}, 32)})
	requireSecretBoxError(t, err, SecretBoxConfigurationError, "missing", "known")
	_, err = NewSecretBox("active", map[string][]byte{"active": []byte("short-secret-sentinel")})
	requireSecretBoxError(t, err, SecretBoxConfigurationError, "short-secret-sentinel")
	_, err = NewSecretBox("bad\nkey", map[string][]byte{"bad\nkey": bytes.Repeat([]byte{0x10}, 32)})
	requireSecretBoxError(t, err, SecretBoxConfigurationError, "bad\nkey")

	box := newTestSecretBox(t, "active", map[string][]byte{"active": bytes.Repeat([]byte{0x10}, 32)})
	_, err = box.Seal(bytes.Repeat([]byte("x"), MaxSecretBoxPlaintext+1), []byte("private-aad-sentinel"))
	requireSecretBoxError(t, err, SecretBoxPlaintextTooLarge, "private-aad-sentinel", strings.Repeat("x", 16))

	unknownVersion := Envelope{Version: SecretBoxVersion + 1, KeyID: "active"}
	_, err = box.Open(unknownVersion, []byte("private-aad-sentinel"))
	requireSecretBoxError(t, err, SecretBoxUnsupportedVersion, "private-aad-sentinel")

	missingKey := Envelope{Version: SecretBoxVersion, KeyID: "not-configured"}
	_, err = box.Open(missingKey, []byte("private-aad-sentinel"))
	requireSecretBoxError(t, err, SecretBoxConfigurationError, "not-configured", "private-aad-sentinel")

	oversizedEnvelope := Envelope{
		Version: SecretBoxVersion, KeyID: "active", Nonce: make([]byte, 12),
		Ciphertext: make([]byte, MaxSecretBoxPlaintext+16+1),
	}
	_, err = box.Open(oversizedEnvelope, nil)
	requireSecretBoxError(t, err, SecretBoxPlaintextTooLarge)
}

func TestSecretBoxCopiesKeyMaterialAndMap(t *testing.T) {
	key := bytes.Repeat([]byte{0x55}, 32)
	keys := map[string][]byte{"active": key, "old": bytes.Repeat([]byte{0x66}, 32)}
	box := newTestSecretBox(t, "active", keys)
	for i := range key {
		key[i] = 0
	}
	delete(keys, "old")
	keys["active"] = bytes.Repeat([]byte{0x77}, 32)

	sealed, err := box.Seal([]byte("copy-proof"), []byte("aad"))
	require.NoError(t, err)
	opened, err := box.Open(sealed, []byte("aad"))
	require.NoError(t, err)
	require.Equal(t, "copy-proof", string(opened))
}

func newTestSecretBox(t *testing.T, active string, keys map[string][]byte) *SecretBox {
	t.Helper()
	box, err := NewSecretBox(active, keys)
	require.NoError(t, err)
	return box
}

func cloneEnvelope(source Envelope) Envelope {
	return Envelope{
		Version: source.Version, KeyID: source.KeyID,
		Nonce:      append([]byte(nil), source.Nonce...),
		Ciphertext: append([]byte(nil), source.Ciphertext...),
	}
}

func requireSecretBoxError(t *testing.T, err error, code SecretBoxErrorCode, forbidden ...string) {
	t.Helper()
	require.Error(t, err)
	var typed *SecretBoxError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, code, typed.Code)
	for _, value := range forbidden {
		if value != "" {
			require.NotContains(t, err.Error(), value)
		}
	}
}
