// Package providercatalog validates immutable provider release manifests.
// It does not register providers or access a database, API, or connector.
package providercatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"encore.app/l3-core/connectorcatalog/providerschema"
	"github.com/google/uuid"
)

const (
	FormatVersion              = 1
	MaxManifestBytes           = 2 << 20
	MaxSchemaBytes             = 1 << 20
	MaxNameCharacters          = 128
	MaxVersionCharacters       = 128
	MaxProtocolCount           = 128
	MaxProtocolCharacters      = 64
	MaxDeviceKindCount         = 3
	schemaDigestDomain         = "thingspanel-connection-provider-schemas-v1\n"
	manifestFieldConfiguration = "configurationSchema"
	manifestFieldDeviceParams  = "deviceParameterSchema"
)

// ManifestError provides a stable, secret-safe validation classification. It
// intentionally does not contain input values, validator text, or raw schemas.
type ManifestError struct {
	Field string
	Kind  string
}

func (e *ManifestError) Error() string {
	if e == nil {
		return "invalid provider manifest"
	}
	return "invalid provider manifest " + e.Field + " (" + e.Kind + ")"
}

// ValidatedManifest owns copies of its input values. Accessors return copies
// so callers cannot mutate the validated representation through an alias.
type ValidatedManifest struct {
	formatVersion                 int
	providerID                    string
	name                          string
	version                       string
	artifactDigest                string
	protocols                     []string
	deviceKinds                   []string
	originalConfigurationSchema   json.RawMessage
	originalDeviceParameterSchema json.RawMessage
	schemaDigest                  string
}

func (m *ValidatedManifest) FormatVersion() int     { return m.formatVersion }
func (m *ValidatedManifest) ProviderID() string     { return m.providerID }
func (m *ValidatedManifest) Name() string           { return m.name }
func (m *ValidatedManifest) Version() string        { return m.version }
func (m *ValidatedManifest) ArtifactDigest() string { return m.artifactDigest }

// SchemaDigest is lowercase SHA-256 over the domain string followed by each
// exact schema byte sequence prefixed by its uint64 big-endian byte length.
// It is intentionally not a JCS/canonical-JSON digest.
func (m *ValidatedManifest) SchemaDigest() string { return m.schemaDigest }

func (m *ValidatedManifest) Protocols() []string {
	return append([]string{}, m.protocols...)
}

func (m *ValidatedManifest) DeviceKinds() []string {
	return append([]string{}, m.deviceKinds...)
}

func (m *ValidatedManifest) OriginalConfigurationSchema() json.RawMessage {
	return append(json.RawMessage(nil), m.originalConfigurationSchema...)
}

func (m *ValidatedManifest) OriginalDeviceParameterSchema() json.RawMessage {
	return append(json.RawMessage(nil), m.originalDeviceParameterSchema...)
}

type manifestWire struct {
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

// ValidateManifest strictly decodes and validates one immutable provider
// manifest. SchemaDigest hashes the exact schema JSON value bytes using this
// framing: domain string, uint64 big-endian configuration length, configuration
// bytes, uint64 big-endian device-parameter length, device-parameter bytes.
func ValidateManifest(input []byte) (*ValidatedManifest, error) {
	if len(input) == 0 || len(input) > MaxManifestBytes || !utf8.Valid(input) {
		return nil, manifestError("manifest", "invalid_size_or_encoding")
	}
	if _, err := parseUniqueJSON(input); err != nil {
		return nil, manifestError("manifest", "invalid_json")
	}

	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var wire manifestWire
	if err := decoder.Decode(&wire); err != nil {
		return nil, manifestError("manifest", "invalid_shape")
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, manifestError("manifest", "trailing_value")
	}

	var present map[string]json.RawMessage
	if err := json.Unmarshal(input, &present); err != nil || present == nil {
		return nil, manifestError("manifest", "invalid_shape")
	}
	for _, field := range []string{
		"formatVersion", "providerId", "name", "version", "artifactDigest",
		"protocols", "deviceKinds", manifestFieldConfiguration, manifestFieldDeviceParams,
	} {
		if _, ok := present[field]; !ok {
			return nil, manifestError(field, "required")
		}
	}

	if wire.FormatVersion != FormatVersion {
		return nil, manifestError("formatVersion", "unsupported")
	}
	providerID, err := uuid.Parse(wire.ProviderID)
	if err != nil || providerID == uuid.Nil || providerID.String() != wire.ProviderID {
		return nil, manifestError("providerId", "invalid_uuid")
	}
	if !boundedNonEmpty(wire.Name, MaxNameCharacters) {
		return nil, manifestError("name", "invalid_length")
	}
	if !boundedNonEmpty(wire.Version, MaxVersionCharacters) {
		return nil, manifestError("version", "invalid_length")
	}
	if !validArtifactDigest(wire.ArtifactDigest) {
		return nil, manifestError("artifactDigest", "invalid_sha256")
	}
	if bytes.Equal(bytes.TrimSpace(present["protocols"]), []byte("null")) {
		return nil, manifestError("protocols", "invalid_shape")
	}
	if bytes.Equal(bytes.TrimSpace(present["deviceKinds"]), []byte("null")) {
		return nil, manifestError("deviceKinds", "invalid_shape")
	}
	if err := validateProtocols(wire.Protocols); err != nil {
		return nil, err
	}
	if err := validateDeviceKinds(wire.DeviceKinds); err != nil {
		return nil, err
	}
	if len(wire.ConfigurationSchema) == 0 || len(wire.ConfigurationSchema) > MaxSchemaBytes {
		return nil, manifestError(manifestFieldConfiguration, "invalid_size")
	}
	if len(wire.DeviceParameterSchema) == 0 || len(wire.DeviceParameterSchema) > MaxSchemaBytes {
		return nil, manifestError(manifestFieldDeviceParams, "invalid_size")
	}
	if _, err := providerschema.CompilePair(wire.ConfigurationSchema, wire.DeviceParameterSchema); err != nil {
		var schemaErr *providerschema.Error
		if errors.As(err, &schemaErr) && schemaErr.Domain == providerschema.DeviceParametersDomain {
			return nil, manifestError(manifestFieldDeviceParams, "invalid_schema")
		}
		return nil, manifestError(manifestFieldConfiguration, "invalid_schema")
	}

	config := append(json.RawMessage(nil), wire.ConfigurationSchema...)
	deviceParams := append(json.RawMessage(nil), wire.DeviceParameterSchema...)
	return &ValidatedManifest{
		formatVersion:                 wire.FormatVersion,
		providerID:                    wire.ProviderID,
		name:                          wire.Name,
		version:                       wire.Version,
		artifactDigest:                wire.ArtifactDigest,
		protocols:                     append([]string(nil), wire.Protocols...),
		deviceKinds:                   append([]string(nil), wire.DeviceKinds...),
		originalConfigurationSchema:   config,
		originalDeviceParameterSchema: deviceParams,
		schemaDigest:                  computeSchemaDigest(config, deviceParams),
	}, nil
}

func manifestError(field, kind string) error {
	return &ManifestError{Field: field, Kind: kind}
}

func boundedNonEmpty(value string, maxCharacters int) bool {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return false
	}
	characters := utf8.RuneCountInString(value)
	return characters > 0 && characters <= maxCharacters
}

func validArtifactDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	for _, ch := range value[len("sha256:"):] {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

func validateProtocols(protocols []string) error {
	if len(protocols) > MaxProtocolCount {
		return manifestError("protocols", "too_many")
	}
	seen := make(map[string]struct{}, len(protocols))
	for _, protocol := range protocols {
		if !boundedNonEmpty(protocol, MaxProtocolCharacters) {
			return manifestError("protocols", "invalid_item")
		}
		if _, duplicate := seen[protocol]; duplicate {
			return manifestError("protocols", "duplicate_item")
		}
		seen[protocol] = struct{}{}
	}
	return nil
}

func validateDeviceKinds(kinds []string) error {
	if len(kinds) > MaxDeviceKindCount {
		return manifestError("deviceKinds", "too_many")
	}
	allowed := map[string]struct{}{"standalone": {}, "gateway": {}, "subdevice": {}}
	seen := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		if _, ok := allowed[kind]; !ok {
			return manifestError("deviceKinds", "invalid_item")
		}
		if _, duplicate := seen[kind]; duplicate {
			return manifestError("deviceKinds", "duplicate_item")
		}
		seen[kind] = struct{}{}
	}
	return nil
}

func computeSchemaDigest(configuration, deviceParameters []byte) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, schemaDigestDomain)
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(configuration)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write(configuration)
	binary.BigEndian.PutUint64(length[:], uint64(len(deviceParameters)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write(deviceParameters)
	return hex.EncodeToString(hash.Sum(nil))
}

// parseUniqueJSON validates JSON syntax and rejects duplicate keys at every
// depth without materializing numbers as float64.
func parseUniqueJSON(input []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	value, err := parseJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func parseJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("invalid object key")
			}
			if _, exists := object[key]; exists {
				return nil, errors.New("duplicate key")
			}
			value, err := parseJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if _, err := expectDelimiter(decoder, '}'); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := parseJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if _, err := expectDelimiter(decoder, ']'); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return nil, errors.New("unexpected delimiter")
	}
}

func expectDelimiter(decoder *json.Decoder, expected json.Delim) (json.Delim, error) {
	token, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != expected {
		return 0, errors.New("unexpected delimiter")
	}
	return delimiter, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("trailing JSON value")
	}
	return err
}
