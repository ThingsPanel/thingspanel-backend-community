package brokercontrol

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"project/internal/brokercontract"
	"project/internal/brokermapper"
)

const (
	CommunityProvider     = "mosquitto"
	MappingVersion        = "standard-v1"
	MaxAuthBodyBytes      = 16 << 10
	DefaultGrantTTL       = 60 * time.Second
	MaxServiceTargets     = 256
	MaxGrantResponseBytes = 1 << 20
)

var (
	ErrDenied      = errors.New("brokercontrol: denied")
	ErrUnavailable = errors.New("brokercontrol: unavailable")
)

type TokenBinding struct {
	Token          string `json:"token"`
	BrokerCell     string `json:"broker_cell"`
	BrokerInstance string `json:"broker_instance"`
}

type ServiceCredential struct {
	Username         string                   `json:"username"`
	Password         string                   `json:"password"`
	BrokerCell       string                   `json:"broker_cell"`
	BrokerInstance   string                   `json:"broker_instance"`
	AllowedClientIDs []string                 `json:"allowed_client_ids"`
	AllowedDeviceIDs []string                 `json:"allowed_device_ids,omitempty"`
	AllowedTenantIDs []string                 `json:"allowed_tenant_ids,omitempty"`
	PluginSocket     bool                     `json:"plugin_socket,omitempty"`
	AllowAnyClientID bool                     `json:"allow_any_client_id,omitempty"`
	Principal        brokercontract.Principal `json:"principal"`
	Policy           brokercontract.Policy    `json:"policy"`
	PolicyVersion    string                   `json:"policy_version"`
	MappingVersion   string                   `json:"mapping_version"`
}

type Config struct {
	Tokens   []TokenBinding
	Services []ServiceCredential
	GrantTTL time.Duration
}

type tokenRecord struct {
	digest         [32]byte
	cell, instance string
}
type Owner struct {
	repo     Repository
	tokens   []tokenRecord
	services []ServiceCredential
	ttl      time.Duration
}

func NewOwner(repo Repository, cfg Config) (*Owner, error) {
	if repo == nil || len(cfg.Tokens) == 0 {
		return nil, errors.New("brokercontrol: repository and owner tokens are required")
	}
	o := &Owner{repo: repo, ttl: cfg.GrantTTL}
	if o.ttl <= 0 {
		o.ttl = DefaultGrantTTL
	}
	if o.ttl > time.Minute {
		return nil, errors.New("brokercontrol: grant TTL exceeds limit")
	}
	for _, t := range cfg.Tokens {
		if len(t.Token) < 32 || len(t.Token) > 4096 || !validConfigString(t.BrokerCell) || !validConfigString(t.BrokerInstance) {
			return nil, errors.New("brokercontrol: invalid token binding")
		}
		o.tokens = append(o.tokens, tokenRecord{digest: sha256.Sum256([]byte(t.Token)), cell: t.BrokerCell, instance: t.BrokerInstance})
	}
	serviceKeys := map[string]struct{}{}
	for _, service := range cfg.Services {
		if !validConfigString(service.Username) || service.Password == "" || !utf8.ValidString(service.Password) || len(service.Password) > 256 ||
			!validConfigString(service.BrokerCell) || !validConfigString(service.BrokerInstance) || service.Principal.Kind != brokercontract.PrincipalService ||
			brokercontract.ValidatePrincipal(service.Principal) != nil || brokercontract.ValidatePolicy(service.Policy) != nil ||
			!validConfigString(service.PolicyVersion) || !validConfigString(service.MappingVersion) ||
			(len(service.AllowedClientIDs) == 0 && !service.AllowAnyClientID) || (service.AllowAnyClientID && !service.PluginSocket) ||
			(service.PluginSocket && (len(service.AllowedDeviceIDs) == 0 || len(service.AllowedTenantIDs) == 0 || service.MappingVersion != MappingVersion)) {
			return nil, errors.New("brokercontrol: invalid service allowlist entry")
		}
		key := service.Username + "\x00" + service.BrokerCell + "\x00" + service.BrokerInstance
		if _, exists := serviceKeys[key]; exists {
			return nil, errors.New("brokercontrol: duplicate service allowlist entry")
		}
		serviceKeys[key] = struct{}{}
		service.Policy = normalizePolicy(service.Policy)
		if len(service.AllowedDeviceIDs) > MaxServiceTargets || len(service.AllowedTenantIDs) > MaxServiceTargets ||
			(len(service.Policy.Publish) > 0 && len(service.AllowedDeviceIDs) == 0) ||
			!validUniqueScopeIDs(service.AllowedDeviceIDs) || !validUniqueScopeIDs(service.AllowedTenantIDs) {
			return nil, errors.New("brokercontrol: invalid service target scope")
		}
		for _, filter := range service.Policy.Publish {
			if !service.PluginSocket && !validServicePublishFilter(filter) {
				return nil, errors.New("brokercontrol: service publish filter is outside the finite community allowlist")
			}
		}
		for _, clientID := range service.AllowedClientIDs {
			if !validConfigString(clientID) {
				return nil, errors.New("brokercontrol: invalid service client id")
			}
		}
		service.AllowedClientIDs = append([]string(nil), service.AllowedClientIDs...)
		service.AllowedDeviceIDs = append([]string(nil), service.AllowedDeviceIDs...)
		service.AllowedTenantIDs = append([]string(nil), service.AllowedTenantIDs...)
		sort.Strings(service.AllowedClientIDs)
		o.services = append(o.services, service)
	}
	return o, nil
}

// Service publish permissions use finite canonical reply and control families;
// scopeServicePublishPolicy later expands them to explicit target device numbers.
func validServicePublishFilter(filter string) bool {
	if brokercontract.ValidateSubscribeFilter(filter) != nil || brokercontract.FilterMatchesInternal(filter) {
		return false
	}
	_, ok := servicePublishNumberIndex(filter)
	return ok
}

func validUniqueScopeIDs(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validConfigString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "+#/\\") {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func servicePublishNumberIndex(filter string) (int, bool) {
	p := strings.Split(filter, "/")
	if len(p) == 4 && (p[0] == "devices" || p[0] == "gateway") && p[1] == "command" && p[3] == "+" && validDeviceNumberFilter(p[2]) {
		return 2, true
	}
	if len(p) == 4 && (p[0] == "devices" || p[0] == "gateway") && p[1] == "telemetry" && p[2] == "control" && validDeviceNumberFilter(p[3]) {
		return 3, true
	}
	if len(p) == 4 && (p[0] == "devices" || p[0] == "gateway") && p[1] == "attributes" && p[2] == "get" && validDeviceNumberFilter(p[3]) {
		return 3, true
	}
	if len(p) == 5 && (p[0] == "devices" || p[0] == "gateway") && p[1] == "attributes" && p[2] == "set" && validDeviceNumberFilter(p[3]) && p[4] == "+" {
		return 3, true
	}
	if len(p) == 5 && (p[0] == "devices" || p[0] == "gateway") && (p[1] == "attributes" || p[1] == "event") && p[2] == "response" && validDeviceNumberFilter(p[3]) && p[4] == "+" {
		return 3, true
	}
	if len(p) == 4 && p[0] == "ota" && p[1] == "devices" && p[2] == "inform" && validDeviceNumberFilter(p[3]) {
		return 3, true
	}
	return 0, false
}

func validDeviceNumberFilter(number string) bool {
	if number == "+" {
		return true
	}
	return brokercontract.ValidatePublishTopic(number) == nil && !strings.Contains(number, "/")
}

func scopeServicePublishPolicy(service ServiceCredential, targets []DeviceRecord) (brokercontract.Policy, error) {
	if len(service.Policy.Publish) == 0 {
		return normalizePolicy(service.Policy), nil
	}
	if len(targets) == 0 || len(targets) != len(service.AllowedDeviceIDs) || len(targets) > MaxServiceTargets {
		return brokercontract.Policy{}, errors.New("service target resolution was incomplete")
	}
	allowedIDs := make(map[string]struct{}, len(service.AllowedDeviceIDs))
	for _, id := range service.AllowedDeviceIDs {
		allowedIDs[id] = struct{}{}
	}
	allowedTenants := make(map[string]struct{}, len(service.AllowedTenantIDs))
	if len(service.AllowedTenantIDs) == 0 {
		allowedTenants[service.Principal.TenantID] = struct{}{}
	} else {
		for _, tenantID := range service.AllowedTenantIDs {
			allowedTenants[tenantID] = struct{}{}
		}
	}
	byNumber := make(map[string]DeviceRecord, len(targets))
	seenIDs := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if _, ok := allowedIDs[target.ID]; !ok || target.ID == "" || target.TenantID == "" || target.DeviceNumber == "" || target.IsEnabled != "enabled" || target.ActivateFlag != "active" || !validDeviceNumberFilter(target.DeviceNumber) || target.DeviceNumber == "+" {
			return brokercontract.Policy{}, errors.New("service target is not an active scoped device")
		}
		if _, ok := allowedTenants[target.TenantID]; !ok {
			return brokercontract.Policy{}, errors.New("service target is outside allowed tenants")
		}
		if _, duplicate := seenIDs[target.ID]; duplicate {
			return brokercontract.Policy{}, errors.New("duplicate service target")
		}
		if target.DeviceNumberCount != 1 {
			return brokercontract.Policy{}, errors.New("service device number is not globally unique")
		}
		if _, duplicate := byNumber[target.DeviceNumber]; duplicate {
			return brokercontract.Policy{}, errors.New("device number is ambiguous across service targets")
		}
		seenIDs[target.ID] = struct{}{}
		byNumber[target.DeviceNumber] = target
	}
	policy := service.Policy
	policy.Publish = nil
	for _, filter := range service.Policy.Publish {
		index, ok := servicePublishNumberIndex(filter)
		if !ok {
			return brokercontract.Policy{}, errors.New("unsupported service publish family")
		}
		parts := strings.Split(filter, "/")
		number := parts[index]
		if number == "+" {
			for _, target := range targets {
				concrete := append([]string(nil), parts...)
				concrete[index] = target.DeviceNumber
				policy.Publish = append(policy.Publish, strings.Join(concrete, "/"))
			}
		} else {
			if _, ok := byNumber[number]; !ok {
				return brokercontract.Policy{}, errors.New("literal service device number is outside allowed targets")
			}
			policy.Publish = append(policy.Publish, filter)
		}
	}
	policy = normalizePolicy(policy)
	if brokercontract.ValidatePolicy(policy) != nil {
		return brokercontract.Policy{}, errors.New("scoped service policy is invalid")
	}
	return policy, nil
}

func validConfigString(s string) bool {
	if s == "" || len(s) > 256 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return false
		}
	}
	return true
}

func normalizePolicy(p brokercontract.Policy) brokercontract.Policy {
	p.Publish = uniqueSorted(p.Publish)
	p.Subscribe = uniqueSorted(p.Subscribe)
	return p
}
func uniqueSorted(values []string) []string {
	sort.Strings(values)
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, v := range values[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

func (o *Owner) Authenticate(ctx context.Context, req brokercontract.AuthRequest) (brokercontract.Grant, error) {
	deny := brokercontract.Grant{Allow: false, DenyCode: "not_authorized"}
	if brokercontract.ValidateAuthRequest(req) != nil || req.Provider != CommunityProvider || req.ConnectionNonce == "" || !validConfigString(req.Username) || !utf8.ValidString(req.Password) {
		return deny, nil
	}
	serviceNameMatched := false
	for _, s := range o.services {
		if !constantStringEqual(req.Username, s.Username) {
			continue
		}
		serviceNameMatched = true
		if req.BrokerCell != s.BrokerCell || req.BrokerInstance != s.BrokerInstance {
			continue
		}
		if !constantStringEqual(req.Password, s.Password) || (!s.AllowAnyClientID && !containsExact(s.AllowedClientIDs, req.WireClientID)) {
			return deny, nil
		}
		policy := normalizePolicy(s.Policy)
		var pluginTargets []brokercontract.PluginTarget
		if s.PluginSocket {
			targets, targetErr := o.repo.ResolveServiceTargets(ctx, s.AllowedDeviceIDs)
			if targetErr != nil {
				return deny, ErrUnavailable
			}
			if len(targets) != len(s.AllowedDeviceIDs) {
				return deny, nil
			}
			for _, target := range targets {
				if target.DeviceNumberCount != 1 || target.IsEnabled != "enabled" || target.ActivateFlag != "active" || !containsExact(s.AllowedTenantIDs, target.TenantID) {
					return deny, nil
				}
			}
			pluginTargets, targetErr = o.resolvePluginTargets(ctx, s.AllowedDeviceIDs, s.AllowedTenantIDs, s.BrokerCell)
			if targetErr != nil {
				return deny, targetErr
			}
			if len(pluginTargets) != len(s.AllowedDeviceIDs) {
				return deny, nil
			}
			policy, targetErr = buildPluginPolicy(pluginTargets)
			if targetErr != nil {
				return deny, nil
			}
		} else if len(policy.Publish) > 0 {
			targets, targetErr := o.repo.ResolveServiceTargets(ctx, s.AllowedDeviceIDs)
			if targetErr != nil {
				return deny, ErrUnavailable
			}
			policy, targetErr = scopeServicePublishPolicy(s, targets)
			if targetErr != nil {
				return deny, nil
			}
			pluginIDs := make([]string, 0, len(targets))
			for _, target := range targets {
				if pluginOwnedRecord(target) {
					pluginIDs = append(pluginIDs, target.ID)
				}
			}
			if len(pluginIDs) > 0 {
				if _, ok := o.repo.(pluginTargetResolver); !ok {
					return deny, ErrUnavailable
				}
				allowedTenants := s.AllowedTenantIDs
				if len(allowedTenants) == 0 {
					allowedTenants = []string{s.Principal.TenantID}
				}
				pluginTargets, targetErr = o.resolvePluginTargets(ctx, pluginIDs, allowedTenants, s.BrokerCell)
				if targetErr != nil {
					return deny, targetErr
				}
				if len(pluginTargets) != len(pluginIDs) {
					return deny, nil
				}
				policy, targetErr = addPluginPublishAliases(policy, pluginTargets)
				if targetErr != nil {
					return deny, nil
				}
			}
		}
		if !s.PluginSocket {
			pluginTargets = nil
		}
		return o.issueGrant(ctx, req, s.Principal, policy, s.PolicyVersion, s.MappingVersion, pluginTargets)
	}
	if serviceNameMatched {
		return deny, nil
	}
	var password *string
	if req.Password != "" {
		password = &req.Password
	}
	credential, _ := json.Marshal(struct {
		Username string  `json:"username"`
		Password *string `json:"password,omitempty"`
	}{Username: req.Username, Password: password})
	if len(credential) > 500 {
		return deny, nil
	}
	devices, err := o.repo.FindCredentialMatches(ctx, string(credential))
	if err != nil {
		return deny, ErrUnavailable
	}
	if len(devices) != 1 {
		return deny, nil
	}
	device := devices[0]
	if device.ID == "" || device.TenantID == "" || device.DeviceNumber == "" || device.IsEnabled != "enabled" || device.ActivateFlag != "active" {
		return deny, nil
	}
	pluginOwned := pluginOwnedRecord(device)
	if device.DeviceType == "3" && !pluginOwned { // Native gateway children still authenticate through their gateway.
		return deny, nil
	}
	var pluginTargets []brokercontract.PluginTarget
	if pluginOwned {
		resolver, ok := o.repo.(pluginTargetResolver)
		if !ok {
			return deny, ErrUnavailable
		}
		resolved, resolveErr := resolver.ResolvePluginTargets(ctx, []string{device.ID})
		if resolveErr != nil {
			return deny, ErrUnavailable
		}
		if len(resolved) > 1 {
			return deny, nil
		}
		if len(resolved) == 1 {
			pluginTargets, resolveErr = o.resolvePluginTargets(ctx, []string{device.ID}, []string{device.TenantID}, req.BrokerCell)
			if resolveErr != nil {
				return deny, resolveErr
			}
			if len(pluginTargets) != 1 {
				return deny, nil
			}
		}
	}
	children := []DeviceRecord(nil)
	if device.DeviceType == "2" || device.DeviceType == "" {
		children, err = o.repo.ListEnabledChildren(ctx, device.TenantID, device.ID)
		if err != nil {
			return deny, ErrUnavailable
		}
	}
	kind := brokercontract.PrincipalDevice
	if device.DeviceType == "2" || (device.DeviceType == "" && len(children) > 0) {
		kind = brokercontract.PrincipalGateway
	}
	for _, child := range children {
		if child.DeviceConfigID == nil {
			continue
		}
		hasMappings, mappingErr := o.repo.HasEnabledTopicMappings(ctx, device.TenantID, *child.DeviceConfigID)
		if mappingErr != nil {
			return deny, ErrUnavailable
		}
		if hasMappings {
			deny.DenyCode = "mapping_not_certified"
			return deny, nil
		}
	}
	principal := brokercontract.Principal{TenantID: device.TenantID, DeviceID: device.ID, Kind: kind}
	policy := buildDevicePolicy(device, children, kind)
	snapshot := brokermapper.Standard()
	if device.DeviceConfigID != nil {
		snapshot, err = o.repo.CompileMappingSnapshot(ctx, device.TenantID, *device.DeviceConfigID)
		if err != nil {
			if errors.Is(err, brokermapper.ErrUnsupportedMapping) {
				deny.DenyCode = "mapping_not_certified"
				return deny, nil
			}
			return deny, ErrUnavailable
		}
	}
	if err := addMappingFilters(&policy, snapshot, device.DeviceNumber); err != nil {
		deny.DenyCode = "mapping_not_certified"
		return deny, nil
	}
	if len(pluginTargets) > 0 {
		policy, err = buildPluginPolicy(pluginTargets)
		if err != nil {
			deny.DenyCode = "plugin_policy_not_certified"
			return deny, nil
		}
		principal.Kind = pluginTargets[0].Kind
	}
	return o.issueGrant(ctx, req, principal, policy, "", snapshot.Version, pluginTargets)
}

func addMappingFilters(policy *brokercontract.Policy, snapshot *brokermapper.Snapshot, deviceNumber string) error {
	if snapshot == nil || deviceNumber == "" {
		return errors.New("missing mapping snapshot scope")
	}
	for _, rule := range snapshot.Rules() {
		if !strings.Contains(rule.SourceTopic, "{device_number}") {
			return errors.New("mapping source lacks device scope")
		}
		for _, level := range strings.Split(rule.SourceTopic, "/") {
			if level == "+" || level == "#" {
				return errors.New("mapping source contains an unscoped wildcard")
			}
		}
		filter := strings.ReplaceAll(rule.SourceTopic, "{device_number}", deviceNumber)
		filter = strings.ReplaceAll(filter, "{message_id}", "+")
		if strings.ContainsAny(filter, "{}#") || brokercontract.ValidateSubscribeFilter(filter) != nil || brokercontract.IsInternalResource(filter) {
			return errors.New("invalid mapping source filter")
		}
		if rule.Direction == "up" {
			policy.Publish = append(policy.Publish, filter)
		} else if rule.Direction == "down" {
			policy.Subscribe = append(policy.Subscribe, filter)
		} else {
			return errors.New("invalid mapping direction")
		}
	}
	*policy = normalizePolicy(*policy)
	return brokercontract.ValidatePolicy(*policy)
}

func (o *Owner) issueGrant(ctx context.Context, req brokercontract.AuthRequest, p brokercontract.Principal, policy brokercontract.Policy, policyVersion, mappingVersion string, pluginTargetSets ...[]brokercontract.PluginTarget) (brokercontract.Grant, error) {
	deny := brokercontract.Grant{Allow: false, DenyCode: "not_authorized"}
	if brokercontract.ValidatePrincipal(p) != nil || brokercontract.ValidatePolicy(policy) != nil {
		return deny, nil
	}
	var binding TransportBinding
	var err error
	if len(pluginTargetSets) > 0 && len(pluginTargetSets[0]) > 0 {
		target := pluginTargetSets[0][0]
		binding = TransportBinding{TenantID: target.TenantID, DeviceID: target.DeviceID, BrokerCell: req.BrokerCell, RouteEpoch: target.RouteEpoch, Phase: target.BindingPhase}
	} else {
		if admission, ok := o.repo.(interface {
			ResolveAdmissionBinding(context.Context, string, string, string) (TransportBinding, error)
		}); ok && p.Kind != brokercontract.PrincipalService {
			binding, err = admission.ResolveAdmissionBinding(ctx, p.TenantID, p.DeviceID, req.BrokerCell)
		} else {
			binding, err = o.repo.ResolveActiveBinding(ctx, p.TenantID, p.DeviceID, req.BrokerCell)
		}
	}
	if err != nil {
		if errors.Is(err, ErrBindingConflict) {
			return deny, nil
		}
		return deny, ErrUnavailable
	}
	policy = normalizePolicy(policy)
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return deny, ErrUnavailable
	}
	sum := sha256.Sum256(policyJSON)
	policyHash := hex.EncodeToString(sum[:])
	if policyVersion == "" {
		policyVersion = "community-policy-v1-" + policyHash[:16]
	}
	connectionSum := sha256.Sum256([]byte("thingspanel/mosquitto/connection/v1\x00" + req.BrokerInstance + "\x00" + req.ConnectionNonce))
	connectionID := "mosq-" + base64.RawURLEncoding.EncodeToString(connectionSum[:18])
	grant := brokercontract.Grant{Allow: true, Principal: p, Policy: policy, PolicyVersion: policyVersion, MappingVersion: mappingVersion, ConnectionID: connectionID, PolicyHash: policyHash, RouteEpoch: binding.RouteEpoch, ExpiresAt: time.Now().UTC().Add(o.ttl)}
	grant.BindingPhase = binding.Phase
	response, err := json.Marshal(grant)
	if err != nil {
		return deny, ErrUnavailable
	}
	if len(response) > MaxGrantResponseBytes {
		deny.DenyCode = "grant_too_large"
		return deny, nil
	}
	owned, err := o.repo.ClaimClientID(ctx, req.BrokerCell, req.WireClientID, p.TenantID, p.DeviceID)
	if err != nil {
		return deny, ErrUnavailable
	}
	if !owned {
		return deny, nil
	}
	if len(pluginTargetSets) > 0 && len(pluginTargetSets[0]) > 0 {
		if err := o.persistPluginGrant(ctx, &grant, req.BrokerCell, req.BrokerInstance, pluginTargetSets[0]); err != nil {
			return deny, ErrUnavailable
		}
		response, err = json.Marshal(grant)
		if err != nil || len(response) > MaxGrantResponseBytes {
			return deny, ErrUnavailable
		}
	}
	return grant, nil
}

func constantStringEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
func containsExact(values []string, target string) bool {
	i := sort.SearchStrings(values, target)
	return i < len(values) && values[i] == target
}

func buildDevicePolicy(root DeviceRecord, children []DeviceRecord, kind brokercontract.PrincipalKind) brokercontract.Policy {
	prefix := "devices"
	if kind == brokercontract.PrincipalGateway {
		prefix = "gateway"
	}
	publish := []string{prefix + "/telemetry", prefix + "/attributes/+", prefix + "/event/+", prefix + "/command/response/+", prefix + "/attributes/set/response/+", prefix + "/event/response/+"}
	subscribe := make([]string, 0, 6*(1+len(children)))
	devices := append([]DeviceRecord{root}, children...)
	for _, d := range devices {
		if d.DeviceNumber == "" {
			continue
		}
		n := d.DeviceNumber
		subscribe = append(subscribe, prefix+"/telemetry/control/"+n, prefix+"/attributes/set/"+n+"/+", prefix+"/attributes/get/"+n,
			prefix+"/command/"+n+"/+", prefix+"/attributes/response/"+n+"/+", prefix+"/event/response/"+n+"/+")
	}
	return normalizePolicy(brokercontract.Policy{Publish: publish, Subscribe: subscribe, MaxPayload: brokercontract.MaxPayloadBytes, MaxQoS: 1, RetainProfile: brokercontract.RetainedLiveIngress})
}

type Handler struct {
	owner   *Owner
	maxBody int64
}

func (o *Owner) Handler() http.Handler { return &Handler{owner: o, maxBody: MaxAuthBodyBytes} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/authenticate" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req brokercontract.AuthRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if !h.owner.authorizedToken(token, req.BrokerCell, req.BrokerInstance) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if (r.Header.Get("X-Broker-Cell") != "" && req.BrokerCell != r.Header.Get("X-Broker-Cell")) || (r.Header.Get("X-Broker-Instance") != "" && req.BrokerInstance != r.Header.Get("X-Broker-Instance")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	grant, err := h.owner.Authenticate(r.Context(), req)
	if err != nil {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(grant); err != nil {
		return
	}
}

func bearerToken(header string) string {
	if !strings.HasPrefix(header, "Bearer ") || len(header) <= 7 {
		return ""
	}
	value := header[7:]
	if strings.ContainsAny(value, " \t\r\n") || len(value) > 4096 {
		return ""
	}
	return value
}

func (o *Owner) authorizedToken(token, cell, instance string) bool {
	if len(token) < 32 || len(token) > 4096 {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	matched := 0
	for _, candidate := range o.tokens {
		same := subtle.ConstantTimeCompare(sum[:], candidate.digest[:])
		scope := subtle.ConstantTimeCompare([]byte(candidate.cell), []byte(cell)) & subtle.ConstantTimeCompare([]byte(candidate.instance), []byte(instance))
		matched |= same & scope
	}
	return matched == 1
}

func (o *Owner) String() string { return fmt.Sprintf("broker owner (%d cell bindings)", len(o.tokens)) }
