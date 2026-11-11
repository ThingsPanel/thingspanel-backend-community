package credentialsecret

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReplayFingerprintSurvivesKeyRotationAndRejectsTenantSubstitution(t *testing.T) {
	aad := CredentialAAD{TenantID: "11111111-1111-4111-8111-111111111111", OwnerKind: CredentialOwnerDevice, OwnerID: "22222222-2222-4222-8222-222222222222", CredentialID: "33333333-3333-4333-8333-333333333333", Purpose: CredentialPurposeDeviceAccess, Use: CredentialUseReplayResponse, ReplayBinding: strings.Repeat("a", 64)}
	keys := map[string][]byte{"old": bytes.Repeat([]byte{1}, 32), "new": bytes.Repeat([]byte{2}, 32)}
	old, err := NewCredentialSecretProtector("old", keys)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("replay-request")
	stored, err := old.Fingerprint(aad, secret)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewCredentialSecretProtector("new", keys)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := rotated.VerifyFingerprint(aad, secret, stored); err != nil || !ok {
		t.Fatal("retained key cannot verify prior request")
	}
	aad.TenantID = "44444444-4444-4444-8444-444444444444"
	if ok, err := rotated.VerifyFingerprint(aad, secret, stored); err != nil || ok {
		t.Fatal("tenant substitution accepted")
	}
	var absent *CredentialSecretProtector
	if _, err := absent.VerifyFingerprint(aad, secret, stored); !errors.Is(err, ErrCredentialKeyUnavailable) {
		t.Fatal("missing protector did not fail closed")
	}
}
