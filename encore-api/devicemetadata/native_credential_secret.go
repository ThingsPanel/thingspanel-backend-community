// Credential cryptography is shared with the offline converter. The existing
// service API and stored ciphertext/AAD format remain unchanged.
package devicemetadata

import "encore.app/devicemetadata/credentialsecret"

type CredentialPurpose = credentialsecret.CredentialPurpose
type CredentialUse = credentialsecret.CredentialUse
type CredentialOwnerKind = credentialsecret.CredentialOwnerKind
type CredentialAAD = credentialsecret.CredentialAAD
type ProtectedCredential = credentialsecret.ProtectedCredential
type CredentialFingerprint = credentialsecret.CredentialFingerprint
type CredentialSecretProtector = credentialsecret.CredentialSecretProtector

const (
	CredentialPurposeDeviceAccess       = credentialsecret.CredentialPurposeDeviceAccess
	CredentialPurposeTemplateEnrollment = credentialsecret.CredentialPurposeTemplateEnrollment
	CredentialUseMaterial               = credentialsecret.CredentialUseMaterial
	CredentialUseReplayResponse         = credentialsecret.CredentialUseReplayResponse
	CredentialOwnerDevice               = credentialsecret.CredentialOwnerDevice
	CredentialOwnerTemplate             = credentialsecret.CredentialOwnerTemplate

	credentialCipherSuite    = "AES-256-GCM"
	maxCredentialSecretBytes = 8192
	credentialKeyringEnv     = "METADATA_CREDENTIAL_KEYRING_B64"
	credentialActiveKeyEnv   = "METADATA_CREDENTIAL_ACTIVE_KEY_REF"
)

var (
	ErrCredentialKeyUnavailable   = credentialsecret.ErrCredentialKeyUnavailable
	ErrCredentialProtectionFailed = credentialsecret.ErrCredentialProtectionFailed
)

func NewCredentialSecretProtector(ref string, keys map[string][]byte) (*CredentialSecretProtector, error) {
	return credentialsecret.NewCredentialSecretProtector(ref, keys)
}
func NewCredentialSecretProtectorFromEnv() (*CredentialSecretProtector, error) {
	return credentialsecret.NewCredentialSecretProtectorFromEnv()
}
func VerifyCredentialFingerprint(aad CredentialAAD, secret []byte, stored CredentialFingerprint, keys map[string][]byte) (bool, error) {
	return credentialsecret.VerifyCredentialFingerprint(aad, secret, stored, keys)
}
func GenerateCredentialSecret() (string, error) { return credentialsecret.GenerateCredentialSecret() }
func validateCredentialAAD(aad CredentialAAD) error {
	return credentialsecret.ValidateCredentialAAD(aad)
}
func validCredentialUUID(value string) bool { return credentialsecret.ValidCredentialUUID(value) }
