package northbound

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"encore.dev/beta/errs"
)

const secretKeyVersion = 1

// subtleCompare is constant time; a token comparison must not leak length or
// prefix through timing.
func subtleCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func secretAEAD() (cipher.AEAD, error) {
	if cfg.SecretKey == "" {
		return nil, errNoServiceToken
	}
	// The configured value is key material, not a password to store. Hashing it
	// gives a fixed 32-byte AES-256 key regardless of the input length.
	sum := sha256.Sum256([]byte("northbound-secret-v1\x00" + cfg.SecretKey))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encryptSecrets serialises the secret map and seals it. The plaintext never
// touches the database, a log line, or an API response.
func encryptSecrets(tenantID, connectionID string, secrets map[string]string) ([]byte, error) {
	if len(secrets) == 0 {
		return nil, nil
	}
	aead, err := secretAEAD()
	if err != nil {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "northbound secret key is not configured"}
	}
	keys := make([]string, 0, len(secrets))
	for k := range secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	clean := make(map[string]string, len(secrets))
	for _, k := range keys {
		if v := secrets[k]; v != "" {
			clean[k] = v
		}
	}
	if len(clean) == 0 {
		return nil, nil
	}
	plain, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// The additional data binds the ciphertext to one tenant and one
	// connection, so a copied row cannot be replayed against another target.
	aad := []byte(tenantID + "\x00" + connectionID)
	return aead.Seal(nonce, nonce, plain, aad), nil
}

func decryptSecrets(tenantID, connectionID string, ciphertext []byte) (map[string]string, error) {
	if len(ciphertext) == 0 {
		return map[string]string{}, nil
	}
	aead, err := secretAEAD()
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("stored secret is truncated")
	}
	nonce, body := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	aad := []byte(tenantID + "\x00" + connectionID)
	plain, err := aead.Open(nil, nonce, body, aad)
	if err != nil {
		return nil, errors.New("stored secret cannot be opened with the configured key")
	}
	out := map[string]string{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// storeSecrets replaces the connection's secret set. A nil map means "leave
// unchanged"; an explicit empty map clears it.
func storeSecrets(ctx context.Context, tenantID, connectionID string, secrets map[string]string) error {
	if secrets == nil {
		return nil
	}
	sealed, err := encryptSecrets(tenantID, connectionID, secrets)
	if err != nil {
		return err
	}
	if sealed == nil {
		_, err = db.Exec(ctx, `DELETE FROM nb_connection_secrets WHERE connection_id = $1::uuid AND tenant_id = $2`, connectionID, tenantID)
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO nb_connection_secrets (tenant_id, connection_id, secret_ref, key_version, ciphertext, updated_at)
		VALUES ($1, $2::uuid, $3, $4, $5, now())
		ON CONFLICT (connection_id) DO UPDATE
		SET tenant_id = excluded.tenant_id, secret_ref = excluded.secret_ref,
		    key_version = excluded.key_version, ciphertext = excluded.ciphertext, updated_at = now()`,
		tenantID, connectionID, "nb-conn/"+connectionID, secretKeyVersion, sealed)
	return err
}

func loadSecrets(ctx context.Context, tenantID, connectionID string) (map[string]string, error) {
	var ciphertext []byte
	err := db.QueryRow(ctx, `SELECT ciphertext FROM nb_connection_secrets WHERE connection_id = $1::uuid AND tenant_id = $2`,
		connectionID, tenantID).Scan(&ciphertext)
	if err != nil {
		// No stored secret is a valid state for connectors that need none.
		return map[string]string{}, nil
	}
	return decryptSecrets(tenantID, connectionID, ciphertext)
}

// secretFieldNames returns only the names that are set. Values are never
// returned by any API.
func secretFieldNames(ctx context.Context, tenantID, connectionID string) []string {
	secrets, err := loadSecrets(ctx, tenantID, connectionID)
	if err != nil || len(secrets) == 0 {
		return []string{}
	}
	names := make([]string, 0, len(secrets))
	for k := range secrets {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
