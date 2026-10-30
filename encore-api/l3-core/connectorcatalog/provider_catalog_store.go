package connectorcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/l3-core/connectorcatalog/providercatalog"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

var providerCatalogDB = sqldb.NewDatabase("connector_runtime", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})

var (
	ErrProviderReleaseConflict = errors.New("provider release version already exists with different content")
	ErrProviderReleaseNotFound = errors.New("provider release not found")
	ErrProviderCatalogCorrupt  = errors.New("provider catalog record is invalid")
	ErrProviderCatalogInput    = errors.New("provider catalog input is invalid")
)

const providerManifestDigestDomain = "thingspanel-provider-release-manifest-v1\n"

// ProviderRelease is a validated public provider release. ArtifactDigest and
// schema bytes are internal catalog data and must not be copied into OAS output
// unless a later API contract explicitly permits them.
type ProviderRelease struct {
	ProviderID                    string          `json:"providerId"`
	Name                          string          `json:"name"`
	Version                       string          `json:"version"`
	Protocols                     []string        `json:"protocols"`
	DeviceKinds                   []string        `json:"deviceKinds"`
	SchemaDigest                  string          `json:"schemaDigest"`
	ArtifactDigest                string          `json:"-"`
	ManifestDigest                string          `json:"-"`
	OriginalConfigurationSchema   json.RawMessage `json:"-"`
	OriginalDeviceParameterSchema json.RawMessage `json:"-"`
	PublishedAt                   time.Time       `json:"-"`
}

type providerReleaseManifestWire struct {
	FormatVersion         int             `json:"formatVersion"`
	ProviderID            string          `json:"providerId"`
	Name                  string          `json:"name"`
	Version               string          `json:"version"`
	ArtifactDigest        string          `json:"artifactDigest"`
	Protocols             []string        `json:"protocols"`
	DeviceKinds           []string        `json:"deviceKinds"`
	ConfigurationSchema   json.RawMessage `json:"configurationSchema"`
	DeviceParameterSchema json.RawMessage `json:"deviceParameterSchema"`
}

// publishProviderRelease persists an immutable platform-wide release. It
// serializes same-key publishers with a transaction advisory lock; exact
// replays are idempotent and changed content for the same provider/version
// returns ErrProviderReleaseConflict.
func publishProviderRelease(ctx context.Context, input *providercatalog.ValidatedManifest, actorID, requestID string) (*ProviderRelease, error) {
	if !validProviderPublisherID(actorID) || !validProviderPublisherID(requestID) {
		return nil, ErrProviderCatalogInput
	}
	manifest, err := revalidateProviderManifest(input)
	if err != nil {
		return nil, ErrProviderCatalogInput
	}
	release, protocolsJSON, deviceKindsJSON := releaseFromManifest(manifest)
	if !providerReleaseCanPersist(release) {
		return nil, ErrProviderCatalogInput
	}
	release.ManifestDigest = providerReleaseManifestDigest(release)

	tx, err := providerCatalogDB.Begin(ctx)
	if err != nil {
		return nil, errors.New("provider catalog unavailable")
	}
	defer tx.Rollback()
	lockKey := fmt.Sprintf("%s:%d:%s", release.ProviderID, len(release.Version), release.Version)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return nil, errors.New("provider catalog unavailable")
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO provider_releases (
			provider_id, version, name, protocols, device_kinds,
			schema_digest, artifact_digest, manifest_digest,
			configuration_schema_raw, device_parameter_schema_raw
		) VALUES ($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8,$9,$10)
		ON CONFLICT (provider_id, version) DO NOTHING
	`, release.ProviderID, release.Version, release.Name, string(protocolsJSON), string(deviceKindsJSON),
		release.SchemaDigest, release.ArtifactDigest, release.ManifestDigest,
		[]byte(release.OriginalConfigurationSchema), []byte(release.OriginalDeviceParameterSchema))
	if err != nil {
		return nil, errors.New("provider catalog unavailable")
	}
	persisted, err := loadProviderReleaseTx(ctx, tx, release.ProviderID, release.Version)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		if !sameProviderRelease(persisted, release) {
			return nil, ErrProviderReleaseConflict
		}
	}
	if result.RowsAffected() == 1 {
		if _, err := tx.Exec(ctx, `INSERT INTO provider_publication_events
   (id,provider_id,version,actor_id,request_id,schema_digest,artifact_digest)
   VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.NewString(), release.ProviderID, release.Version,
			actorID, requestID, release.SchemaDigest, release.ArtifactDigest); err != nil {
			return nil, errors.New("provider catalog unavailable")
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.New("provider catalog unavailable")
	}
	return cloneProviderRelease(persisted), nil
}

// loadProviderReleaseTx loads exactly one immutable release and revalidates
// both schemas and the stored digests before returning any public fields.
func loadProviderReleaseTx(ctx context.Context, tx *sqldb.Tx, providerID, version string) (*ProviderRelease, error) {
	if tx == nil || !validProviderReleaseKey(providerID, version) {
		return nil, ErrProviderCatalogInput
	}
	var raw providerReleaseRawRow
	if err := tx.QueryRow(ctx, providerReleaseSelect+`
		WHERE provider_id=$1 AND version=$2
	`, providerID, version).Scan(
		&raw.ProviderID, &raw.Version, &raw.Name, &raw.Protocols, &raw.DeviceKinds,
		&raw.SchemaDigest, &raw.ArtifactDigest, &raw.ManifestDigest,
		&raw.ConfigurationSchema, &raw.DeviceParameterSchema, &raw.PublishedAt,
	); err != nil {
		if errors.Is(err, sqldb.ErrNoRows) {
			return nil, ErrProviderReleaseNotFound
		}
		return nil, errors.New("provider catalog unavailable")
	}
	return validateProviderReleaseRow(raw)
}

// listProviderReleasesTx returns a stable provider/version order. An empty
// catalog returns a non-nil empty slice and total zero.
func listProviderReleasesTx(ctx context.Context, tx *sqldb.Tx, page, pageSize int) ([]ProviderRelease, int64, error) {
	if tx == nil || page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, 0, ErrProviderCatalogInput
	}
	if int64(page-1) > (1<<63-1)/int64(pageSize) {
		return nil, 0, ErrProviderCatalogInput
	}
	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM provider_releases`).Scan(&total); err != nil {
		return nil, 0, errors.New("provider catalog unavailable")
	}
	offset := int64(page-1) * int64(pageSize)
	rows, err := tx.Query(ctx, providerReleaseSelect+`
		ORDER BY provider_id::text COLLATE "C" ASC, version COLLATE "C" ASC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		return nil, 0, errors.New("provider catalog unavailable")
	}
	defer rows.Close()
	out := make([]ProviderRelease, 0)
	for rows.Next() {
		var raw providerReleaseRawRow
		if err := rows.Scan(
			&raw.ProviderID, &raw.Version, &raw.Name, &raw.Protocols, &raw.DeviceKinds,
			&raw.SchemaDigest, &raw.ArtifactDigest, &raw.ManifestDigest,
			&raw.ConfigurationSchema, &raw.DeviceParameterSchema, &raw.PublishedAt,
		); err != nil {
			return nil, 0, errors.New("provider catalog unavailable")
		}
		release, err := validateProviderReleaseRow(raw)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *release)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, errors.New("provider catalog unavailable")
	}
	return out, total, nil
}

type providerReleaseRawRow struct {
	ProviderID            string
	Version               string
	Name                  string
	Protocols             []byte
	DeviceKinds           []byte
	SchemaDigest          string
	ArtifactDigest        string
	ManifestDigest        string
	ConfigurationSchema   []byte
	DeviceParameterSchema []byte
	PublishedAt           time.Time
}

const providerReleaseSelect = `
	SELECT provider_id::text, version, name, protocols, device_kinds,
	       schema_digest, artifact_digest, manifest_digest,
	       configuration_schema_raw, device_parameter_schema_raw, published_at
	  FROM provider_releases
`

func revalidateProviderManifest(input *providercatalog.ValidatedManifest) (*providercatalog.ValidatedManifest, error) {
	if input == nil {
		return nil, ErrProviderCatalogInput
	}
	wire := providerReleaseManifestWire{
		FormatVersion:         input.FormatVersion(),
		ProviderID:            input.ProviderID(),
		Name:                  input.Name(),
		Version:               input.Version(),
		ArtifactDigest:        input.ArtifactDigest(),
		Protocols:             input.Protocols(),
		DeviceKinds:           input.DeviceKinds(),
		ConfigurationSchema:   input.OriginalConfigurationSchema(),
		DeviceParameterSchema: input.OriginalDeviceParameterSchema(),
	}
	encoded, err := marshalProviderReleaseManifest(wire)
	if err != nil {
		return nil, ErrProviderCatalogInput
	}
	validated, err := providercatalog.ValidateManifest(encoded)
	if err != nil {
		return nil, ErrProviderCatalogInput
	}
	return validated, nil
}

func releaseFromManifest(manifest *providercatalog.ValidatedManifest) (*ProviderRelease, []byte, []byte) {
	protocols := manifest.Protocols()
	deviceKinds := manifest.DeviceKinds()
	protocolsJSON, _ := json.Marshal(protocols)
	deviceKindsJSON, _ := json.Marshal(deviceKinds)
	return &ProviderRelease{
		ProviderID:                    manifest.ProviderID(),
		Name:                          manifest.Name(),
		Version:                       manifest.Version(),
		Protocols:                     protocols,
		DeviceKinds:                   deviceKinds,
		SchemaDigest:                  manifest.SchemaDigest(),
		ArtifactDigest:                manifest.ArtifactDigest(),
		OriginalConfigurationSchema:   manifest.OriginalConfigurationSchema(),
		OriginalDeviceParameterSchema: manifest.OriginalDeviceParameterSchema(),
	}, protocolsJSON, deviceKindsJSON
}

func validateProviderReleaseRow(raw providerReleaseRawRow) (*ProviderRelease, error) {
	var protocols, deviceKinds []string
	if json.Unmarshal(raw.Protocols, &protocols) != nil || protocols == nil || json.Unmarshal(raw.DeviceKinds, &deviceKinds) != nil || deviceKinds == nil {
		return nil, ErrProviderCatalogCorrupt
	}
	release := &ProviderRelease{
		ProviderID:                    raw.ProviderID,
		Name:                          raw.Name,
		Version:                       raw.Version,
		Protocols:                     protocols,
		DeviceKinds:                   deviceKinds,
		SchemaDigest:                  raw.SchemaDigest,
		ArtifactDigest:                raw.ArtifactDigest,
		ManifestDigest:                raw.ManifestDigest,
		OriginalConfigurationSchema:   append(json.RawMessage(nil), raw.ConfigurationSchema...),
		OriginalDeviceParameterSchema: append(json.RawMessage(nil), raw.DeviceParameterSchema...),
		PublishedAt:                   raw.PublishedAt,
	}
	if !providerReleaseCanPersist(release) {
		return nil, ErrProviderCatalogCorrupt
	}
	validated, err := validateProviderReleaseContent(release)
	if err != nil || validated.SchemaDigest() != release.SchemaDigest || providerReleaseManifestDigest(release) != release.ManifestDigest {
		return nil, ErrProviderCatalogCorrupt
	}
	return release, nil
}

func validateProviderReleaseContent(release *ProviderRelease) (*providercatalog.ValidatedManifest, error) {
	wire := providerReleaseManifestWire{
		FormatVersion:         providercatalog.FormatVersion,
		ProviderID:            release.ProviderID,
		Name:                  release.Name,
		Version:               release.Version,
		ArtifactDigest:        release.ArtifactDigest,
		Protocols:             append([]string(nil), release.Protocols...),
		DeviceKinds:           append([]string(nil), release.DeviceKinds...),
		ConfigurationSchema:   append(json.RawMessage(nil), release.OriginalConfigurationSchema...),
		DeviceParameterSchema: append(json.RawMessage(nil), release.OriginalDeviceParameterSchema...),
	}
	encoded, err := marshalProviderReleaseManifest(wire)
	if err != nil {
		return nil, ErrProviderCatalogCorrupt
	}
	manifest, err := providercatalog.ValidateManifest(encoded)
	if err != nil {
		return nil, ErrProviderCatalogCorrupt
	}
	return manifest, nil
}

// marshalProviderReleaseManifest preserves the schema RawMessage values byte
// for byte. encoding/json compacts Marshaler output, which would otherwise
// silently change the schema identity digest.
func marshalProviderReleaseManifest(wire providerReleaseManifestWire) ([]byte, error) {
	header, err := json.Marshal(struct {
		FormatVersion  int      `json:"formatVersion"`
		ProviderID     string   `json:"providerId"`
		Name           string   `json:"name"`
		Version        string   `json:"version"`
		ArtifactDigest string   `json:"artifactDigest"`
		Protocols      []string `json:"protocols"`
		DeviceKinds    []string `json:"deviceKinds"`
	}{
		FormatVersion:  wire.FormatVersion,
		ProviderID:     wire.ProviderID,
		Name:           wire.Name,
		Version:        wire.Version,
		ArtifactDigest: wire.ArtifactDigest,
		Protocols:      wire.Protocols,
		DeviceKinds:    wire.DeviceKinds,
	})
	if err != nil || len(header) == 0 || header[len(header)-1] != '}' {
		return nil, ErrProviderCatalogInput
	}
	encoded := append([]byte(nil), header[:len(header)-1]...)
	encoded = append(encoded, `,"configurationSchema":`...)
	encoded = append(encoded, wire.ConfigurationSchema...)
	encoded = append(encoded, `,"deviceParameterSchema":`...)
	encoded = append(encoded, wire.DeviceParameterSchema...)
	encoded = append(encoded, '}')
	return encoded, nil
}

func providerReleaseCanPersist(release *ProviderRelease) bool {
	if release == nil || !validProviderReleaseKey(release.ProviderID, release.Version) || !utf8.ValidString(release.Name) || strings.ContainsRune(release.Name, 0) {
		return false
	}
	for _, protocol := range release.Protocols {
		if strings.ContainsRune(protocol, 0) {
			return false
		}
	}
	return true
}

func validProviderReleaseKey(providerID, version string) bool {
	if !utf8.ValidString(version) || utf8.RuneCountInString(version) < 1 || utf8.RuneCountInString(version) > providercatalog.MaxVersionCharacters || strings.ContainsRune(version, 0) {
		return false
	}
	parsed, err := uuidParseCanonical(providerID)
	return err == nil && parsed
}

func uuidParseCanonical(value string) (bool, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return false, errors.New("invalid provider id")
	}
	return true, nil
}

func providerReleaseManifestDigest(release *ProviderRelease) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(providerManifestDigestDomain))
	writeDigestString := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	writeDigestBytes := func(value []byte) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(value)
	}
	var formatVersion [8]byte
	binary.BigEndian.PutUint64(formatVersion[:], uint64(providercatalog.FormatVersion))
	_, _ = hash.Write(formatVersion[:])
	writeDigestString(release.ProviderID)
	writeDigestString(release.Name)
	writeDigestString(release.Version)
	writeDigestString(release.ArtifactDigest)
	writeDigestStrings := func(values []string) {
		var count [8]byte
		binary.BigEndian.PutUint64(count[:], uint64(len(values)))
		_, _ = hash.Write(count[:])
		for _, value := range values {
			writeDigestString(value)
		}
	}
	writeDigestStrings(release.Protocols)
	writeDigestStrings(release.DeviceKinds)
	writeDigestBytes(release.OriginalConfigurationSchema)
	writeDigestBytes(release.OriginalDeviceParameterSchema)
	return hex.EncodeToString(hash.Sum(nil))
}

func sameProviderRelease(left, right *ProviderRelease) bool {
	if left == nil || right == nil || left.ManifestDigest != right.ManifestDigest {
		return false
	}
	leftProtocols, _ := json.Marshal(left.Protocols)
	rightProtocols, _ := json.Marshal(right.Protocols)
	leftKinds, _ := json.Marshal(left.DeviceKinds)
	rightKinds, _ := json.Marshal(right.DeviceKinds)
	return left.ProviderID == right.ProviderID && left.Name == right.Name && left.Version == right.Version &&
		left.SchemaDigest == right.SchemaDigest && left.ArtifactDigest == right.ArtifactDigest &&
		string(leftProtocols) == string(rightProtocols) && string(leftKinds) == string(rightKinds) &&
		string(left.OriginalConfigurationSchema) == string(right.OriginalConfigurationSchema) &&
		string(left.OriginalDeviceParameterSchema) == string(right.OriginalDeviceParameterSchema)
}

func cloneProviderRelease(release *ProviderRelease) *ProviderRelease {
	if release == nil {
		return nil
	}
	clone := *release
	clone.Protocols = append([]string(nil), release.Protocols...)
	clone.DeviceKinds = append([]string(nil), release.DeviceKinds...)
	clone.OriginalConfigurationSchema = append(json.RawMessage(nil), release.OriginalConfigurationSchema...)
	clone.OriginalDeviceParameterSchema = append(json.RawMessage(nil), release.OriginalDeviceParameterSchema...)
	return &clone
}

func validProviderPublisherID(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return false
		}
	}
	return true
}
