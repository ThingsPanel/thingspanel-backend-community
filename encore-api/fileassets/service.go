package fileassets

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strings"

	"encore.dev/storage/objects"
	"encore.dev/storage/sqldb"
)

// Service owns file asset catalog metadata and its independent database.
// Object storage and API workflows are implemented by separate slices.
//
//encore:service
type Service struct {
	objects ObjectStore
	box     *SecretBox
}

var db = sqldb.NewDatabase("file_storage", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})

// Private bucket; callers receive only short-lived, authorized capabilities.
var imageBucket = objects.NewBucket("template-image-assets", objects.BucketConfig{})
var imageBucketAPI = objects.BucketRef[BucketObjectAPI](imageBucket)

func initService() (*Service, error) {
	box, err := secretBoxFromConfig(os.Getenv("TP_FILE_ASSET_KEYRING"))
	if err != nil {
		return nil, err
	}
	return &Service{objects: NewBucketStore(imageBucketAPI), box: box}, nil
}

// Empty configuration leaves this feature unavailable. There is no generated
// fallback key: persisted leases and receipts must survive process restarts.
func secretBoxFromConfig(raw string) (*SecretBox, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 16*1024 {
		return nil, secretBoxError(SecretBoxConfigurationError)
	}
	var cfg struct {
		ActiveKeyID string            `json:"activeKeyId"`
		Keys        map[string]string `json:"keys"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cfg) != nil {
		return nil, secretBoxError(SecretBoxConfigurationError)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, secretBoxError(SecretBoxConfigurationError)
	}
	keys := make(map[string][]byte, len(cfg.Keys))
	for id, encoded := range cfg.Keys {
		key, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return nil, secretBoxError(SecretBoxConfigurationError)
		}
		keys[id] = key
	}
	return NewSecretBox(cfg.ActiveKeyID, keys)
}
