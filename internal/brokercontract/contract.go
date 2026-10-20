// Package brokercontract defines the community Mosquitto ingress contract v1.
// It contains no broker, network, database, or business-mapping dependencies.
package brokercontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ContractVersion      = "1"
	IngressPrefix        = "tp-internal/ingress/v1"
	MaxIdentityBytes     = 256
	MaxWireTopicBytes    = 65535
	MaxEnvelopeBytes     = 2 << 20 // bounds JSON/base64 expansion; payload limit is 1 MiB
	MaxPayloadBytes      = 1 << 20
	MaxIngressTopicBytes = 65535
)

var (
	ErrInvalidPrincipal = errors.New("brokercontract: invalid principal")
	ErrInvalidTopic     = errors.New("brokercontract: invalid topic")
	ErrInvalidEnvelope  = errors.New("brokercontract: invalid envelope")
	ErrInvalidMetadata  = errors.New("brokercontract: invalid metadata")
	ErrInvalidPolicy    = errors.New("brokercontract: invalid policy")
	ErrUnauthorized     = errors.New("brokercontract: unauthorized")
	ErrTooLarge         = errors.New("brokercontract: size limit exceeded")
	ErrUnsupported      = errors.New("brokercontract: unsupported capability")
)

type PrincipalKind string

const (
	PrincipalDevice  PrincipalKind = "device"
	PrincipalGateway PrincipalKind = "gateway"
	PrincipalService PrincipalKind = "service"
)

type Principal struct {
	TenantID string        `json:"tenant_id"`
	DeviceID string        `json:"device_id"`
	Kind     PrincipalKind `json:"principal_kind"`
}

// Metadata is minted by the trusted broker boundary. Device supplied JSON is
// always treated as payload bytes and can never populate these fields.
type Metadata struct {
	PluginContextID  string        `json:"plugin_context_id,omitempty"`
	ContractVersion  string        `json:"contract_version"`
	TenantID         string        `json:"tenant_id"`
	PrincipalKind    PrincipalKind `json:"principal_kind"`
	BrokerCell       string        `json:"broker_cell"`
	BrokerInstance   string        `json:"broker_instance"`
	ConnectionID     string        `json:"connection_id,omitempty"`
	IngestionID      string        `json:"ingestion_id,omitempty"`
	RouteEpoch       uint64        `json:"route_epoch"`
	PolicyVersion    string        `json:"policy_version"`
	MappingVersion   string        `json:"mapping_version"`
	OriginalTopicB64 string        `json:"original_topic_b64"`
	OriginalQoS      byte          `json:"original_qos"`
	OriginalRetain   bool          `json:"original_retain"`
	RetainedReplay   bool          `json:"retained_replay"`
	ObservedAt       time.Time     `json:"observed_at"`
}

// Envelope keeps Values as bytes in Go and encodes it exactly once as standard
// padded Base64 in the compatibility JSON field "values".
type Envelope struct {
	DeviceID string   `json:"device_id"`
	Values   []byte   `json:"values"`
	Metadata Metadata `json:"_tp"`
}

type wireEnvelope struct {
	DeviceID string          `json:"device_id"`
	Values   string          `json:"values"`
	Metadata json.RawMessage `json:"_tp"`
}

// TrustedMetadata is supplied by the authenticated broker adapter. Every
// identity/authorization field in the body must agree with this context.
type TrustedMetadata struct {
	Principal      Principal
	BrokerCell     string
	BrokerInstance string
	ConnectionID   string
	RouteEpoch     *uint64
	PolicyVersion  string
	MappingVersion string
}

// AuthRequest and Grant are the bounded authentication transport contract
// between the Mosquitto shim and the existing identity/policy owner.
type AuthRequest struct {
	RequestID       string `json:"request_id"`
	Provider        string `json:"provider"`
	BrokerCell      string `json:"broker_cell"`
	BrokerInstance  string `json:"broker_instance"`
	ConnectionNonce string `json:"connection_nonce,omitempty"`
	WireClientID    string `json:"wire_client_id"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	Protocol        int    `json:"protocol"`
	Peer            string `json:"peer,omitempty"`
}

type Grant struct {
	PluginContextID string         `json:"plugin_context_id,omitempty"`
	PluginTargets   []PluginTarget `json:"plugin_targets,omitempty"`
	// ProtectedDownlink filters require a private command frame even when their
	// device wire topic is not one of the built-in canonical command families.
	ProtectedDownlink []string  `json:"protected_downlink,omitempty"`
	Allow             bool      `json:"allow"`
	Principal         Principal `json:"principal,omitempty"`
	Policy            Policy    `json:"policy,omitempty"`
	PolicyVersion     string    `json:"policy_version,omitempty"`
	MappingVersion    string    `json:"mapping_version,omitempty"`
	ConnectionID      string    `json:"connection_id,omitempty"`
	PolicyHash        string    `json:"policy_hash,omitempty"`
	RouteEpoch        uint64    `json:"route_epoch,omitempty"`
	BindingPhase      string    `json:"binding_phase,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	DenyCode          string    `json:"deny_code,omitempty"`
	Retryable         bool      `json:"retryable,omitempty"`
}

func (g Grant) MarshalJSON() ([]byte, error) {
	type grantJSON struct {
		PluginContextID   string         `json:"plugin_context_id,omitempty"`
		PluginTargets     []PluginTarget `json:"plugin_targets,omitempty"`
		ProtectedDownlink []string       `json:"protected_downlink,omitempty"`
		Allow             bool           `json:"allow"`
		Principal         *Principal     `json:"principal,omitempty"`
		Policy            *Policy        `json:"policy,omitempty"`
		PolicyVersion     string         `json:"policy_version,omitempty"`
		MappingVersion    string         `json:"mapping_version,omitempty"`
		ConnectionID      string         `json:"connection_id,omitempty"`
		PolicyHash        string         `json:"policy_hash,omitempty"`
		RouteEpoch        *uint64        `json:"route_epoch,omitempty"`
		BindingPhase      string         `json:"binding_phase,omitempty"`
		ExpiresAt         *time.Time     `json:"expires_at,omitempty"`
		DenyCode          string         `json:"deny_code,omitempty"`
		Retryable         bool           `json:"retryable,omitempty"`
	}
	out := grantJSON{Allow: g.Allow, DenyCode: g.DenyCode, Retryable: g.Retryable}
	if g.Allow {
		out.PluginContextID = g.PluginContextID
		out.PluginTargets = g.PluginTargets
		out.ProtectedDownlink = g.ProtectedDownlink
		out.Principal = &g.Principal
		out.Policy = &g.Policy
		out.PolicyVersion = g.PolicyVersion
		out.MappingVersion = g.MappingVersion
		out.ConnectionID = g.ConnectionID
		out.PolicyHash = g.PolicyHash
		out.BindingPhase = g.BindingPhase
		if g.RouteEpoch != 0 {
			out.RouteEpoch = &g.RouteEpoch
		}
		if !g.ExpiresAt.IsZero() {
			out.ExpiresAt = &g.ExpiresAt
		}
	}
	return json.Marshal(out)
}

func ValidatePrincipal(p Principal) error {
	if !validIdentity(p.TenantID) || !validIdentity(p.DeviceID) {
		return ErrInvalidPrincipal
	}
	switch p.Kind {
	case PrincipalDevice, PrincipalGateway, PrincipalService:
	default:
		return ErrInvalidPrincipal
	}
	return nil
}

func ValidateAuthRequest(r AuthRequest) error {
	for _, value := range []string{r.RequestID, r.Provider, r.BrokerCell, r.BrokerInstance, r.WireClientID} {
		if !validIdentity(value) {
			return ErrInvalidMetadata
		}
	}
	if r.Protocol < 3 || r.Protocol > 5 || (r.ConnectionNonce != "" && !validIdentity(r.ConnectionNonce)) ||
		(r.Peer != "" && (!utf8.ValidString(r.Peer) || len(r.Peer) > MaxIdentityBytes || hasControl(r.Peer))) ||
		len(r.Username) > MaxIdentityBytes || len(r.Password) > MaxPayloadBytes {
		return ErrInvalidMetadata
	}
	return nil
}

func ValidateGrant(g Grant) error {
	if err := validatePluginGrant(g); g.Allow && err != nil {
		return err
	}
	if !g.Allow {
		return nil
	}
	if ValidatePrincipal(g.Principal) != nil || ValidatePolicy(g.Policy) != nil ||
		(g.BindingPhase != "" && g.BindingPhase != "active" && g.BindingPhase != "connecting") ||
		!validIdentity(g.PolicyVersion) || !validIdentity(g.MappingVersion) || g.ExpiresAt.IsZero() ||
		!validIdentity(g.ConnectionID) || g.RouteEpoch == 0 {
		return ErrInvalidMetadata
	}
	for _, filter := range g.ProtectedDownlink {
		if ValidateSubscribeFilter(filter) != nil || IsInternalResource(filter) {
			return ErrInvalidMetadata
		}
		policyFilters := g.Policy.Subscribe
		if g.Principal.Kind == PrincipalService {
			policyFilters = g.Policy.Publish
		}
		found := false
		for _, allowed := range policyFilters {
			if allowed == filter {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidMetadata
		}
	}
	return nil
}

func validIdentity(s string) bool {
	if s == "" || len(s) > MaxIdentityBytes || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}

func validateWireTopic(topic []byte) error {
	if len(topic) == 0 || len(topic) > MaxWireTopicBytes || !utf8.Valid(topic) {
		return ErrInvalidTopic
	}
	for _, r := range string(topic) {
		if r == '+' || r == '#' || hasControl(string(r)) {
			return ErrInvalidTopic
		}
	}
	return nil
}

func EncodeEnvelope(p Principal, m Metadata, payload []byte) ([]byte, error) {
	if err := ValidatePrincipal(p); err != nil {
		return nil, err
	}
	if len(payload) > MaxPayloadBytes {
		return nil, ErrTooLarge
	}
	if err := validateMetadata(p, m); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = []byte{}
	}
	b, err := json.Marshal(Envelope{DeviceID: p.DeviceID, Values: payload, Metadata: m})
	if err != nil {
		return nil, err
	}
	if len(b) > MaxEnvelopeBytes {
		return nil, ErrTooLarge
	}
	return b, nil
}

func validateMetadata(p Principal, m Metadata) error {
	if (m.PluginContextID != "" && !validPluginContextID(m.PluginContextID)) || m.ContractVersion != ContractVersion || m.TenantID != p.TenantID || m.PrincipalKind != p.Kind ||
		!validIdentity(m.BrokerCell) || !validIdentity(m.BrokerInstance) ||
		(m.ConnectionID != "" && !validIdentity(m.ConnectionID)) ||
		(m.IngestionID != "" && (!validIdentity(m.IngestionID) || len(m.IngestionID) > 128)) ||
		!validIdentity(m.PolicyVersion) || !validIdentity(m.MappingVersion) || m.OriginalQoS > 2 || m.ObservedAt.IsZero() {
		return ErrInvalidMetadata
	}
	if len(m.OriginalTopicB64) > base64.StdEncoding.EncodedLen(MaxWireTopicBytes) {
		return ErrTooLarge
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(m.OriginalTopicB64)
	if err != nil || validateWireTopic(raw) != nil {
		return ErrInvalidMetadata
	}
	return nil
}

func EncodeOriginalTopic(topic []byte) string { return base64.StdEncoding.EncodeToString(topic) }

// IngressTopic encodes canonical IDs and the original wire topic without
// separator ambiguity. Long wire topics use a typed SHA-256 key.
func IngressTopic(tenantID, deviceID string, wireTopic []byte) (string, error) {
	p := Principal{TenantID: tenantID, DeviceID: deviceID, Kind: PrincipalDevice}
	if err := ValidatePrincipal(p); err != nil {
		return "", err
	}
	if err := validateWireTopic(wireTopic); err != nil {
		return "", err
	}
	tenant := base64.RawURLEncoding.EncodeToString([]byte(tenantID))
	device := base64.RawURLEncoding.EncodeToString([]byte(deviceID))
	wire := "b-" + base64.RawURLEncoding.EncodeToString(wireTopic)
	build := func(k string) string { return IngressPrefix + "/" + tenant + "/" + device + "/" + k }
	out := build(wire)
	if len(out) > MaxIngressTopicBytes {
		sum := sha256.Sum256(wireTopic)
		out = build("h-" + hex.EncodeToString(sum[:]))
	}
	if len(out) > MaxIngressTopicBytes {
		return "", ErrTooLarge
	}
	return out, nil
}

// ValidateIngressTopic checks the canonical identity and recomputes the topic
// key from original wire bytes. Hash keys are integrity checked, not reversible.
func ValidateIngressTopic(topic, tenantID, deviceID string, originalWireTopic []byte) error {
	expected, err := IngressTopic(tenantID, deviceID, originalWireTopic)
	if err != nil {
		return err
	}
	if topic != expected {
		return ErrInvalidTopic
	}
	return nil
}

func ParseIngress(topic string, body []byte, trusted TrustedMetadata) (Envelope, error) {
	var out Envelope
	if len(topic) > MaxIngressTopicBytes || len(body) > MaxEnvelopeBytes {
		return out, ErrTooLarge
	}
	if ValidateUniqueJSON(body) != nil {
		return out, ErrInvalidEnvelope
	}
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var wire wireEnvelope
	if err := dec.Decode(&wire); err != nil {
		return out, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return out, ErrInvalidEnvelope
	}
	// Re-decode to require the exact three top-level keys; duplicates were
	// rejected by the contract JSON validator above.
	if err := json.Unmarshal(body, &raw); err != nil || len(raw) != 3 {
		return out, ErrInvalidEnvelope
	}
	for k := range raw {
		if k != "device_id" && k != "values" && k != "_tp" {
			return out, ErrInvalidEnvelope
		}
	}
	var encoded string
	if err := json.Unmarshal(raw["device_id"], &out.DeviceID); err != nil {
		return out, ErrInvalidEnvelope
	}
	if err := json.Unmarshal(raw["values"], &encoded); err != nil {
		return out, ErrInvalidEnvelope
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(MaxPayloadBytes) {
		return out, ErrTooLarge
	}
	metadataDecoder := json.NewDecoder(bytes.NewReader(raw["_tp"]))
	metadataDecoder.DisallowUnknownFields()
	if err := metadataDecoder.Decode(&out.Metadata); err != nil {
		return out, ErrInvalidMetadata
	}
	m := out.Metadata
	p := Principal{TenantID: m.TenantID, DeviceID: out.DeviceID, Kind: m.PrincipalKind}
	if err := ValidatePrincipal(p); err != nil {
		return out, err
	}
	if trusted.Principal.TenantID != "" || trusted.Principal.DeviceID != "" || trusted.Principal.Kind != "" {
		if err := ValidatePrincipal(trusted.Principal); err != nil {
			return out, err
		}
		if trusted.Principal != p {
			return Envelope{}, ErrInvalidMetadata
		}
	}
	if err := validateMetadata(p, m); err != nil {
		return out, err
	}
	if out.DeviceID != p.DeviceID || m.BrokerCell != trusted.BrokerCell || m.BrokerInstance != trusted.BrokerInstance ||
		(trusted.ConnectionID != "" && m.ConnectionID != trusted.ConnectionID) ||
		(trusted.RouteEpoch != nil && m.RouteEpoch != *trusted.RouteEpoch) ||
		(trusted.PolicyVersion != "" && m.PolicyVersion != trusted.PolicyVersion) ||
		(trusted.MappingVersion != "" && m.MappingVersion != trusted.MappingVersion) {
		return Envelope{}, ErrInvalidMetadata
	}
	original, _ := base64.StdEncoding.Strict().DecodeString(m.OriginalTopicB64)
	if err := ValidateIngressTopic(topic, p.TenantID, p.DeviceID, original); err != nil {
		return Envelope{}, err
	}
	values, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return Envelope{}, ErrInvalidEnvelope
	}
	if len(values) > MaxPayloadBytes {
		return Envelope{}, ErrTooLarge
	}
	out.Values = values
	return out, nil
}

type Action string

const (
	ActionPublish   Action = "publish"
	ActionSubscribe Action = "subscribe"
	ActionReceive   Action = "receive"
)

// Policy is a finite ACL snapshot. Publish entries and subscribe entries are
// MQTT filters. Publish checks an actual topic against the publish filters;
// subscription requests are granted only when listed verbatim.
type Policy struct {
	Publish       []string        `json:"publish"`
	Subscribe     []string        `json:"subscribe"`
	MaxPayload    int             `json:"max_payload"`
	MaxQoS        byte            `json:"max_qos"`
	RetainProfile RetainedProfile `json:"retain_profile"`
}

func ValidatePolicy(p Policy) error {
	for _, t := range p.Publish {
		if ValidateSubscribeFilter(t) != nil {
			return ErrInvalidPolicy
		}
	}
	for _, f := range p.Subscribe {
		if ValidateSubscribeFilter(f) != nil {
			return ErrInvalidPolicy
		}
	}
	if p.MaxPayload < 0 || p.MaxPayload > MaxPayloadBytes || p.MaxQoS > 2 {
		return ErrInvalidPolicy
	}
	if p.RetainProfile != "" && p.RetainProfile != RetainedLiveIngress && p.RetainProfile != RetainedPlatform && p.RetainProfile != RetainedWire {
		return ErrInvalidPolicy
	}
	return nil
}
func ValidatePublishTopic(topic string) error {
	if topic == "" || len(topic) > MaxWireTopicBytes || !utf8.ValidString(topic) || hasControl(topic) || strings.ContainsAny(topic, "+#") {
		return ErrInvalidTopic
	}
	return nil
}
func ValidateSubscribeFilter(filter string) error {
	if filter == "" || len(filter) > MaxWireTopicBytes || !utf8.ValidString(filter) || hasControl(filter) {
		return ErrInvalidTopic
	}
	levels := strings.Split(filter, "/")
	for i, l := range levels {
		if strings.Contains(l, "#") && !(l == "#" && i == len(levels)-1) {
			return ErrInvalidTopic
		}
		if strings.Contains(l, "+") && l != "+" {
			return ErrInvalidTopic
		}
	}
	return nil
}
func AuthorizeTopic(p Principal, policy Policy, action Action, resource string) error {
	if ValidatePrincipal(p) != nil || ValidatePolicy(policy) != nil {
		return ErrInvalidPolicy
	}
	if p.Kind != PrincipalService && IsInternalResource(resource) {
		return ErrUnauthorized
	}
	switch action {
	case ActionPublish:
		if ValidatePublishTopic(resource) != nil {
			return ErrUnauthorized
		}
		for _, t := range policy.Publish {
			if FilterMatches(t, resource) {
				return nil
			}
		}
	case ActionSubscribe:
		if ValidateSubscribeFilter(resource) != nil {
			return ErrUnauthorized
		}
		for _, f := range policy.Subscribe {
			if FilterContains(f, resource) {
				return nil
			}
		}
	case ActionReceive:
		if ValidatePublishTopic(resource) != nil {
			return ErrUnauthorized
		}
		for _, f := range policy.Subscribe {
			if FilterMatches(UnsharedFilter(f), resource) {
				return nil
			}
		}
	default:
		return ErrUnauthorized
	}
	return ErrUnauthorized
}

// FilterMatches follows MQTT level wildcards, including MQTT's rule that a
// leading wildcard does not match a system topic beginning with '$'.
func FilterMatches(filter, topic string) bool {
	if ValidateSubscribeFilter(filter) != nil || ValidatePublishTopic(topic) != nil {
		return false
	}
	if strings.HasPrefix(topic, "$") && (strings.HasPrefix(filter, "#") || strings.HasPrefix(filter, "+")) {
		return false
	}
	f := strings.Split(filter, "/")
	t := strings.Split(topic, "/")
	for i, l := range f {
		if l == "#" {
			return true
		}
		if i >= len(t) {
			return false
		}
		if l != "+" && l != t[i] {
			return false
		}
	}
	return len(f) == len(t)
}

// FilterMatchesInternal reports whether an MQTT publish filter can match any
// reserved internal resource. It evaluates MQTT levels directly so forms such
// as +/# and tp-internal/+ cannot bypass a string-prefix check.
func FilterMatchesInternal(filter string) bool {
	if ValidateSubscribeFilter(filter) != nil {
		return true
	}
	f := UnsharedFilter(filter)
	levels := strings.Split(f, "/")
	if len(levels) == 0 {
		return true
	}
	first := levels[0]
	// The current namespace is an ordinary MQTT topic, so leading +/# can match it.
	if first == "+" || first == "#" || first == "tp-internal" {
		return true
	}
	// The legacy alias begins with '$'; MQTT leading wildcards intentionally do
	// not match it, but a literal $tp prefix is still reserved.
	return strings.HasPrefix(first, "$tp")
}

type RetainedProfile string

const (
	RetainedLiveIngress RetainedProfile = "live-ingress"
	RetainedPlatform    RetainedProfile = "platform-retained"
	RetainedWire        RetainedProfile = "wire-retained"
)

// EncodeRetained applies only the transport-level retained contract. A clear
// is a true zero-byte PUBLISH; replay is metadata only and never an action.
func EncodeRetained(p Principal, m Metadata, profile RetainedProfile, retained bool, payload []byte) ([]byte, error) {
	switch profile {
	case RetainedLiveIngress:
		if retained {
			return nil, ErrUnauthorized
		}
	case RetainedPlatform:
	case RetainedWire:
		return nil, ErrUnsupported
	default:
		return nil, ErrUnsupported
	}
	if profile == RetainedPlatform && retained && len(payload) == 0 {
		return []byte{}, nil
	}
	return EncodeEnvelope(p, m, payload)
}

type CapabilityStatus string

const (
	CapabilitySupported   CapabilityStatus = "supported"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityNotTested   CapabilityStatus = "not_tested"
)

type CapabilityProfile struct {
	Broker                     string           `json:"broker"`
	ContractVersion            string           `json:"contract_version"`
	Scope                      string           `json:"scope"`
	ExactConnectionEvents      CapabilityStatus `json:"exact_connection_events"`
	TargetedDisconnect         CapabilityStatus `json:"targeted_disconnect"`
	SharedSubscriptions        CapabilityStatus `json:"shared_subscriptions"`
	PersistentQueueReplication CapabilityStatus `json:"persistent_queue_replication"`
	Notes                      []string         `json:"notes,omitempty"`
}

func CommunityMosquittoCapabilities() CapabilityProfile {
	return CapabilityProfile{Broker: "mosquitto", ContractVersion: ContractVersion, Scope: "community-only", ExactConnectionEvents: CapabilityNotTested, TargetedDisconnect: CapabilityNotTested, SharedSubscriptions: CapabilityNotTested, PersistentQueueReplication: CapabilityUnsupported, Notes: []string{"Capabilities must be replaced by evidence from a pinned, tested Mosquitto profile."}}
}

// FilterContains proves that every topic selected by requested is covered by
// allowed. It never tests a filter as though it were an ordinary topic.
func FilterContains(allowed, requested string) bool {
	if ValidateSubscribeFilter(allowed) != nil || ValidateSubscribeFilter(requested) != nil {
		return false
	}
	if strings.HasPrefix(requested, "$") && (strings.HasPrefix(allowed, "+") || strings.HasPrefix(allowed, "#")) {
		return false
	}
	a, r := strings.Split(allowed, "/"), strings.Split(requested, "/")
	for i, v := range a {
		if v == "#" {
			return true
		}
		if i >= len(r) {
			return false
		}
		if r[i] == "#" {
			return false
		}
		if v != "+" && v != r[i] {
			return false
		}
	}
	return len(a) == len(r)
}

// Shared subscription delivery carries the underlying topic, not the group.
// SUBSCRIBE itself must still be authorized against the explicit group filter.
func UnsharedFilter(filter string) string {
	if strings.HasPrefix(filter, "$share/") {
		parts := strings.SplitN(filter, "/", 3)
		if len(parts) == 3 && parts[1] != "" && !strings.ContainsAny(parts[1], "+#") {
			return parts[2]
		}
	}
	return filter
}

// IsInternalResource reserves the protected v1 namespace as well as the earlier
// PoC alias. Security relies on explicit ACL checks, not MQTT's '$' wildcard rule.
func IsInternalResource(resource string) bool {
	return resource == "tp-internal" || strings.HasPrefix(resource, "tp-internal/") || strings.HasPrefix(resource, "$tp")
}
