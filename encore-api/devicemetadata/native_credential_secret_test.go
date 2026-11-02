package devicemetadata

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func testCredentialProtector(t *testing.T) *CredentialSecretProtector {
	t.Helper()
	key := bytes.Repeat([]byte{0x5a}, 32)
	protector, err := NewCredentialSecretProtector("key-v1", map[string][]byte{"key-v1": key})
	if err != nil {
		t.Fatal("create credential protector")
	}
	return protector
}

func testCredentialAAD() CredentialAAD {
	return CredentialAAD{
		TenantID:     "11111111-1111-4111-8111-111111111111",
		OwnerKind:    CredentialOwnerDevice,
		OwnerID:      "22222222-2222-4222-8222-222222222222",
		CredentialID: "33333333-3333-4333-8333-333333333333",
		Purpose:      CredentialPurposeDeviceAccess,
		Use:          CredentialUseMaterial,
	}
}

func TestCredentialSecretProtectionRoundTripAndRandomNonce(t *testing.T) {
	protector := testCredentialProtector(t)
	aad := testCredentialAAD()
	secret := []byte("credential-secret-sentinel")
	first, err := protector.Seal(aad, secret)
	if err != nil {
		t.Fatal("seal credential")
	}
	second, err := protector.Seal(aad, secret)
	if err != nil {
		t.Fatal("seal second credential")
	}
	if first.KeyID != "key-v1" || first.CipherSuite != credentialCipherSuite || len(first.Nonce) != 12 || len(first.Ciphertext) < 16 {
		t.Fatalf("invalid protected credential metadata: key=%q suite=%q nonce=%d ciphertext=%d", first.KeyID, first.CipherSuite, len(first.Nonce), len(first.Ciphertext))
	}
	if bytes.Equal(first.Nonce, second.Nonce) {
		t.Fatal("separate encryptions reused a nonce")
	}
	if bytes.Contains(first.Ciphertext, secret) {
		t.Fatal("ciphertext contains plaintext")
	}
	opened, err := protector.Open(aad, first)
	if err != nil || !bytes.Equal(opened, secret) {
		t.Fatal("credential did not round-trip")
	}
}

func TestCredentialSecretProtectionRejectsAADSubstitutionAndDamage(t *testing.T) {
	protector := testCredentialProtector(t)
	aad := testCredentialAAD()
	secret := []byte("credential-secret-sentinel")
	protected, err := protector.Seal(aad, secret)
	if err != nil {
		t.Fatal("seal credential")
	}
	variants := map[string]func(*CredentialAAD){
		"tenant": func(v *CredentialAAD) { v.TenantID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" },
		"owner kind": func(v *CredentialAAD) {
			v.OwnerKind = CredentialOwnerTemplate
			v.OwnerID = "44444444-4444-4444-8444-444444444444"
			v.Purpose = CredentialPurposeTemplateEnrollment
		},
		"owner id":      func(v *CredentialAAD) { v.OwnerID = "55555555-5555-4555-8555-555555555555" },
		"credential id": func(v *CredentialAAD) { v.CredentialID = "66666666-6666-4666-8666-666666666666" },
		"purpose":       func(v *CredentialAAD) { v.Purpose = CredentialPurposeTemplateEnrollment },
		"use":           func(v *CredentialAAD) { v.Use = CredentialUseReplayResponse; v.ReplayBinding = strings.Repeat("a", 64) },
	}
	for name, change := range variants {
		t.Run(name, func(t *testing.T) {
			changed := aad
			change(&changed)
			if _, err := protector.Open(changed, protected); !errors.Is(err, ErrCredentialProtectionFailed) {
				t.Fatal("AAD substitution was not rejected")
			}
		})
	}
	damaged := protected
	damaged.Ciphertext = append([]byte(nil), protected.Ciphertext...)
	damaged.Ciphertext[len(damaged.Ciphertext)-1] ^= 0xff
	if _, err := protector.Open(aad, damaged); !errors.Is(err, ErrCredentialProtectionFailed) {
		t.Fatal("damaged ciphertext was not rejected")
	}
	damaged = protected
	damaged.Nonce = append([]byte(nil), protected.Nonce...)
	damaged.Nonce[0] ^= 0xff
	if _, err := protector.Open(aad, damaged); !errors.Is(err, ErrCredentialProtectionFailed) {
		t.Fatal("damaged nonce was not rejected")
	}
	if _, err := protector.Open(aad, ProtectedCredential{KeyID: "unknown", CipherSuite: credentialCipherSuite, Nonce: make([]byte, 12), Ciphertext: make([]byte, 17)}); !errors.Is(err, ErrCredentialKeyUnavailable) {
		t.Fatal("unknown key was not reported as unavailable")
	}
	_, err = protector.Open(aad, ProtectedCredential{KeyID: "key-v1", CipherSuite: credentialCipherSuite, Nonce: make([]byte, 12), Ciphertext: secret})
	if err == nil || strings.Contains(err.Error(), string(secret)) {
		t.Fatal("protection error exposed secret material")
	}
	if _, err := protector.Seal(aad, bytes.Repeat([]byte("x"), maxCredentialSecretBytes+1)); !errors.Is(err, ErrCredentialProtectionFailed) {
		t.Fatal("oversized material secret was accepted")
	}
	if _, err := protector.Open(aad, ProtectedCredential{KeyID: "key-v1", CipherSuite: credentialCipherSuite, Nonce: make([]byte, 12), Ciphertext: make([]byte, maxCredentialSecretBytes+17)}); !errors.Is(err, ErrCredentialProtectionFailed) {
		t.Fatal("oversized material ciphertext was accepted")
	}
}

func TestCredentialReplayResponseAADRequiresAndBindsScopeHash(t *testing.T) {
	protector := testCredentialProtector(t)
	aad := testCredentialAAD()
	aad.Use = CredentialUseReplayResponse
	aad.ReplayBinding = strings.Repeat("a", 64)
	protected, err := protector.Seal(aad, []byte("protected response"))
	if err != nil {
		t.Fatal("seal replay response")
	}
	if opened, err := protector.Open(aad, protected); err != nil || string(opened) != "protected response" {
		t.Fatal("replay response did not round-trip")
	}
	changed := aad
	changed.ReplayBinding = strings.Repeat("b", 64)
	if _, err := protector.Open(changed, protected); !errors.Is(err, ErrCredentialProtectionFailed) {
		t.Fatal("replay response opened under a different valid replay binding")
	}
	if _, err := protector.Seal(CredentialAAD{TenantID: aad.TenantID, OwnerKind: aad.OwnerKind, OwnerID: aad.OwnerID, CredentialID: aad.CredentialID, Purpose: aad.Purpose, Use: aad.Use}, []byte("response")); !errors.Is(err, ErrCredentialProtectionFailed) {
		t.Fatal("replay response without scope hash was accepted")
	}
	material := testCredentialAAD()
	material.ReplayBinding = strings.Repeat("a", 64)
	if _, err := protector.Seal(material, []byte("material")); !errors.Is(err, ErrCredentialProtectionFailed) {
		t.Fatal("material with replay binding was accepted")
	}
	changed = aad
	changed.ReplayBinding = strings.Repeat("A", 64)
	if _, err := protector.Seal(changed, []byte("response")); !errors.Is(err, ErrCredentialProtectionFailed) {
		t.Fatal("uppercase replay scope hash was accepted")
	}
}

func TestCredentialSecretProtectorKeyringConfigurationAndRotation(t *testing.T) {
	t.Setenv(credentialKeyringEnv, "")
	t.Setenv(credentialActiveKeyEnv, "")
	if _, err := NewCredentialSecretProtectorFromEnv(); !errors.Is(err, ErrCredentialKeyUnavailable) {
		t.Fatal("missing keyring should fail closed")
	}
	oldKey := bytes.Repeat([]byte{0x11}, 32)
	newKey := bytes.Repeat([]byte{0x22}, 32)
	oldProtector, err := NewCredentialSecretProtector("old", map[string][]byte{"old": oldKey})
	if err != nil {
		t.Fatal("create old-key protector")
	}
	aad := testCredentialAAD()
	oldRecord, err := oldProtector.Seal(aad, []byte("old-key-material"))
	if err != nil {
		t.Fatal("seal old-key material")
	}
	rotated, err := NewCredentialSecretProtector("new", map[string][]byte{"old": oldKey, "new": newKey})
	if err != nil {
		t.Fatal("create rotated protector")
	}
	if plaintext, err := rotated.Open(aad, oldRecord); err != nil || string(plaintext) != "old-key-material" {
		t.Fatal("rotated keyring could not open old material")
	}
	newRecord, err := rotated.Seal(aad, []byte("new-key-material"))
	if err != nil || newRecord.KeyID != "new" {
		t.Fatal("rotated keyring did not seal with active key")
	}
	if _, err := oldProtector.Open(aad, newRecord); !errors.Is(err, ErrCredentialKeyUnavailable) {
		t.Fatal("old-only keyring unexpectedly opened new material")
	}
	if _, err := NewCredentialSecretProtector("missing", map[string][]byte{"old": oldKey}); !errors.Is(err, ErrCredentialKeyUnavailable) {
		t.Fatal("unknown active key was accepted")
	}

	encoded, _ := json.Marshal(map[string]string{"new": base64.StdEncoding.EncodeToString(newKey)})
	t.Setenv(credentialKeyringEnv, base64.StdEncoding.EncodeToString(encoded))
	t.Setenv(credentialActiveKeyEnv, "new")
	fromEnv, err := NewCredentialSecretProtectorFromEnv()
	if err != nil {
		t.Fatal("valid explicit keyring was rejected")
	}
	if _, err := fromEnv.Seal(aad, []byte("configured")); err != nil {
		t.Fatal("explicit keyring could not seal")
	}
}

func TestCredentialFingerprintUsesContextAndIsVerifiable(t *testing.T) {
	protector := testCredentialProtector(t)
	aad := testCredentialAAD()
	secret := []byte("low-entropy-secret")
	fingerprint, err := protector.Fingerprint(aad, secret)
	if err != nil || len(fingerprint.Fingerprint) != 32 || fingerprint.KeyID != "key-v1" {
		t.Fatal("invalid credential fingerprint")
	}
	if ok, err := VerifyCredentialFingerprint(aad, secret, fingerprint, map[string][]byte{"key-v1": bytes.Repeat([]byte{0x5a}, 32)}); err != nil || !ok {
		t.Fatal("credential fingerprint verification failed")
	}
	changed := aad
	changed.TenantID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if ok, err := VerifyCredentialFingerprint(changed, secret, fingerprint, map[string][]byte{"key-v1": bytes.Repeat([]byte{0x5a}, 32)}); err != nil || ok {
		t.Fatal("fingerprint matched under a different tenant")
	}
	if _, err := VerifyCredentialFingerprint(aad, secret, fingerprint, nil); !errors.Is(err, ErrCredentialKeyUnavailable) {
		t.Fatal("fingerprint verification did not fail closed without key")
	}
}

func TestGenerateCredentialSecretReturns32RandomBytes(t *testing.T) {
	first, err := GenerateCredentialSecret()
	if err != nil {
		t.Fatal("generate credential secret")
	}
	second, err := GenerateCredentialSecret()
	if err != nil {
		t.Fatal("generate second credential secret")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(decoded) != 32 || first == second {
		t.Fatal("generated secret is not 32 random bytes encoded as base64url")
	}
}
