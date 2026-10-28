package notification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/notification/contract"
	"sort"
	"strings"
	"time"
)

var (
	ErrPluginEndpoint = errors.New("notification plugin endpoint is unavailable")
	ErrPluginManifest = errors.New("notification plugin manifest is invalid")
)

type PluginHTTPOptions struct {
	Resolver            *net.Resolver
	Timeout             time.Duration
	MaxManifestBytes    int64
	MaxResponseBytes    int64
	AllowedOrigins      []string
	AllowedCIDRs        []netip.Prefix
	AllowHTTPTestOrigin bool
	TestMode            bool
}

type SafePluginClient struct {
	options PluginHTTPOptions
}

func NewSafePluginClient(options PluginHTTPOptions) (*SafePluginClient, error) {
	if options.Timeout <= 0 || options.Timeout > 30*time.Second {
		options.Timeout = 8 * time.Second
	}
	if options.MaxManifestBytes <= 0 || options.MaxManifestBytes > 1<<20 {
		options.MaxManifestBytes = 256 << 10
	}
	if options.MaxResponseBytes <= 0 || options.MaxResponseBytes > 1<<20 {
		options.MaxResponseBytes = 256 << 10
	}
	if options.AllowHTTPTestOrigin && !options.TestMode {
		return nil, ErrPluginEndpoint
	}
	if !options.TestMode && len(options.AllowedOrigins) == 0 {
		return nil, ErrPluginEndpoint
	}
	for _, origin := range options.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || !validPluginOriginShape(origin) {
			return nil, ErrPluginEndpoint
		}
		switch parsed.Scheme {
		case "https":
		case "http":
			if !options.TestMode || !options.AllowHTTPTestOrigin || !loopbackHost(parsed.Hostname()) {
				return nil, ErrPluginEndpoint
			}
		default:
			return nil, ErrPluginEndpoint
		}
	}
	for _, prefix := range options.AllowedCIDRs {
		if !prefix.IsValid() || prefix != prefix.Masked() {
			return nil, ErrPluginEndpoint
		}
	}
	if options.Resolver == nil {
		options.Resolver = net.DefaultResolver
	}
	return &SafePluginClient{options: options}, nil
}

func (c *SafePluginClient) FetchManifest(ctx context.Context, origin, bearer string) ([]byte, contract.PluginManifestV1, error) {
	endpoint, err := pluginEndpoint(origin, "/v1/manifest", c.options)
	if err != nil {
		return nil, contract.PluginManifestV1{}, ErrPluginEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, contract.PluginManifestV1{}, ErrPluginEndpoint
	}
	request.Header.Set("Accept", "application/json")
	if err := setPluginBearer(request, bearer); err != nil {
		return nil, contract.PluginManifestV1{}, ErrPluginEndpoint
	}
	client := c.client(endpoint)
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return nil, contract.PluginManifestV1{}, ErrPluginEndpoint
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !contentTypeJSON(response.Header.Get("Content-Type")) {
		return nil, contract.PluginManifestV1{}, ErrPluginEndpoint
	}
	raw, err := readBounded(response.Body, c.options.MaxManifestBytes)
	if err != nil {
		return nil, contract.PluginManifestV1{}, ErrPluginEndpoint
	}
	var manifest contract.PluginManifestV1
	if contract.DecodeStrictJSON(raw, &manifest) != nil || !validateIdentityFieldsPayload(raw) || validatePluginManifest(manifest) != nil {
		return nil, contract.PluginManifestV1{}, ErrPluginManifest
	}
	return raw, manifest, nil
}

func (c *SafePluginClient) ValidateInstance(ctx context.Context, runtime PluginRuntime, request contract.PluginValidateRequestV1) (contract.PluginValidateResultV1, error) {
	endpoint, err := pluginEndpoint(runtime.Origin, "/v1/validate", c.options)
	if err != nil {
		return contract.PluginValidateResultV1{}, ErrPluginEndpoint
	}
	body, err := json.Marshal(request)
	if err != nil || int64(len(body)) > c.options.MaxResponseBytes {
		return contract.PluginValidateResultV1{}, ErrPluginEndpoint
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return contract.PluginValidateResultV1{}, ErrPluginEndpoint
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	if err := setPluginBearer(httpRequest, runtime.AuthSecret); err != nil {
		return contract.PluginValidateResultV1{}, ErrPluginEndpoint
	}
	client := c.client(endpoint)
	defer client.CloseIdleConnections()
	response, err := client.Do(httpRequest)
	if err != nil {
		return contract.PluginValidateResultV1{}, ErrPluginEndpoint
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !contentTypeJSON(response.Header.Get("Content-Type")) {
		return contract.PluginValidateResultV1{}, ErrPluginEndpoint
	}
	raw, err := readBounded(response.Body, c.options.MaxResponseBytes)
	if err != nil {
		return contract.PluginValidateResultV1{}, ErrPluginEndpoint
	}
	var result contract.PluginValidateResultV1
	if contract.DecodeStrictJSON(raw, &result) != nil || result.Valid != (len(result.Errors) == 0) {
		return contract.PluginValidateResultV1{}, ErrPluginEndpoint
	}
	for _, fieldError := range result.Errors {
		if !validPluginFieldError(fieldError) {
			return contract.PluginValidateResultV1{}, ErrPluginEndpoint
		}
	}
	return result, nil
}

func (c *SafePluginClient) client(endpoint *url.URL) *http.Client {
	dialer := &net.Dialer{Timeout: c.options.Timeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   c.options.Timeout,
		ResponseHeaderTimeout: c.options.Timeout,
		IdleConnTimeout:       30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(strings.Trim(host, "[]"), endpoint.Hostname()) || port != endpoint.Port() && !(port == "443" && endpoint.Scheme == "https") && !(port == "80" && endpoint.Scheme == "http") {
				return nil, ErrPluginEndpoint
			}
			ips, err := c.options.Resolver.LookupNetIP(ctx, "ip", endpoint.Hostname())
			if err != nil || len(ips) == 0 {
				return nil, ErrPluginEndpoint
			}
			// Reject a mixed answer set so DNS rotation cannot choose an unsafe address.
			for _, ip := range ips {
				if !pluginIPAllowed(ip, endpoint, c.options) {
					return nil, ErrPluginEndpoint
				}
			}
			var last error
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			if last != nil {
				return nil, ErrPluginEndpoint
			}
			return nil, ErrPluginEndpoint
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   c.options.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func pluginEndpoint(origin, path string, options PluginHTTPOptions) (*url.URL, error) {
	if origin == "" || strings.TrimSpace(origin) != origin || strings.ContainsAny(origin, "?#\\") {
		return nil, ErrPluginEndpoint
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Path != "" || parsed.RawPath != "" {
		return nil, ErrPluginEndpoint
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.ForceQuery = false
	if parsed.Hostname() == "" || parsed.Port() == "" && strings.HasSuffix(parsed.Host, ":") {
		return nil, ErrPluginEndpoint
	}
	host := parsed.Hostname()
	if strings.Contains(host, "%") {
		return nil, ErrPluginEndpoint
	}
	switch parsed.Scheme {
	case "https":
		if !pluginOriginAllowed(origin, options.AllowedOrigins) {
			return nil, ErrPluginEndpoint
		}
	case "http":
		if !options.TestMode || !options.AllowHTTPTestOrigin || !loopbackHost(host) || (len(options.AllowedOrigins) > 0 && !pluginOriginAllowed(origin, options.AllowedOrigins)) {
			return nil, ErrPluginEndpoint
		}
	default:
		return nil, ErrPluginEndpoint
	}
	if parsed.Port() == "" {
		if parsed.Scheme == "https" {
			parsed.Host = net.JoinHostPort(host, "443")
		} else {
			parsed.Host = net.JoinHostPort(host, "80")
		}
	}
	parsed.Path = path
	parsed.RawPath = ""
	return parsed, nil
}

func pluginOriginAllowed(origin string, allowed []string) bool {
	for _, candidate := range allowed {
		if origin == candidate {
			return true
		}
	}
	return false
}

func pluginIPAllowed(ip netip.Addr, endpoint *url.URL, options PluginHTTPOptions) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if options.TestMode && options.AllowHTTPTestOrigin && endpoint.Scheme == "http" && loopbackHost(endpoint.Hostname()) {
		return ip.IsLoopback()
	}
	if ip.IsLoopback() {
		return false
	}
	if ip.IsPrivate() {
		for _, prefix := range options.AllowedCIDRs {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	return ip.IsGlobalUnicast() && !blockedPluginIP(ip)
}

func blockedPluginIP(ip netip.Addr) bool {
	blocked := []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"2001:db8::/32", "2001::/23", "2002::/16",
	}
	for _, raw := range blocked {
		prefix, _ := netip.ParsePrefix(raw)
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func setPluginBearer(request *http.Request, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") || strings.ContainsAny(value, " \t") {
		return ErrPluginEndpoint
	}
	request.Header.Set("Authorization", "Bearer "+value)
	return nil
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	if maximum <= 0 {
		return nil, ErrPluginEndpoint
	}
	limited := io.LimitReader(reader, maximum+1)
	body, err := io.ReadAll(limited)
	if err != nil || int64(len(body)) > maximum {
		return nil, ErrPluginEndpoint
	}
	return body, nil
}

func contentTypeJSON(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func validatePluginManifest(manifest contract.PluginManifestV1) error {
	if manifest.APIVersion != "1.0" || !validManifestToken(manifest.PluginID) || !validManifestToken(manifest.PluginVersion) || strings.TrimSpace(manifest.Name) == "" || len(manifest.Name) > 200 || len(manifest.Channels) == 0 || len(manifest.Channels) > 8 || len(manifest.ContentModes) == 0 || len(manifest.ContentModes) > 8 {
		return ErrPluginManifest
	}
	if !validIdentityFieldDeclarations(manifest.IdentityFields) {
		return ErrPluginManifest
	}
	allowedChannels := map[contract.Channel]bool{contract.ChannelEmail: true, contract.ChannelSMS: true, contract.ChannelVoice: true, contract.ChannelIM: true, contract.ChannelWebhook: true}
	seenChannels := make(map[contract.Channel]bool, len(manifest.Channels))
	for _, channel := range manifest.Channels {
		if !allowedChannels[channel] || seenChannels[channel] {
			return ErrPluginManifest
		}
		seenChannels[channel] = true
	}
	allowedModes := map[string]bool{"text": true, "template": true}
	seenModes := make(map[string]bool, len(manifest.ContentModes))
	for _, mode := range manifest.ContentModes {
		if !allowedModes[mode] || seenModes[mode] {
			return ErrPluginManifest
		}
		seenModes[mode] = true
	}
	configSchema, err := CompileNotificationSchema(manifest.ConfigSchema)
	if err != nil || configSchema == nil {
		return ErrPluginManifest
	}
	if _, err := CompileNotificationSchema(manifest.RecipientSchema); err != nil {
		return ErrPluginManifest
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(manifest.ConfigSchema, &envelope) != nil {
		return ErrPluginManifest
	}
	var properties map[string]json.RawMessage
	if json.Unmarshal(envelope["properties"], &properties) != nil || properties == nil {
		return ErrPluginManifest
	}
	if len(manifest.SecretFields) > len(properties) {
		return ErrPluginManifest
	}
	secretSet := make(map[string]bool, len(manifest.SecretFields))
	for _, field := range manifest.SecretFields {
		if field == "" || field != strings.TrimSpace(field) || !validManifestToken(field) || properties[field] == nil || secretSet[field] {
			return ErrPluginManifest
		}
		secretSet[field] = true
	}
	return nil
}

func validManifestToken(value string) bool {
	if len(value) == 0 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func validPluginFieldError(value contract.PluginFieldErrorV1) bool {
	return len(value.Field) <= 256 && validManifestToken(value.Code) && len(value.Message) <= 512 && !strings.ContainsAny(value.Message, "\r\n\x00")
}

func pluginManifestDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func compareManifestDigest(want string, raw []byte) bool {
	actual := pluginManifestDigest(raw)
	if len(want) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(actual)) == 1
}

// CanonicalPluginChannels is kept local to the identity boundary so response
// ordering remains deterministic without changing the frozen contract.
func CanonicalPluginChannels(values []contract.Channel) []contract.Channel {
	out := append([]contract.Channel(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *ManagementService) RegisterPlugin(ctx context.Context, data *authn.Data, bearer, idempotencyKey, requestDigest string, request contract.PluginRegistration) (contract.PluginView, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return contract.PluginView{}, ErrIdentityUnauthorized
	}
	if err := requireUserCapability(principal, "plugin_manage"); err != nil {
		return contract.PluginView{}, err
	}
	if !containsRole(principal.Roles, "SYS_ADMIN") {
		return contract.PluginView{}, ErrIdentityForbidden
	}
	if s.platformPlugins == nil {
		return contract.PluginView{}, ErrManagementUnavailable
	}
	if !validManagementRequest(idempotencyKey, requestDigest) || !validManifestToken(request.PluginID) || !validManifestToken(request.PluginVersion) || len(request.AuthSecret) == 0 || len(request.AuthSecret) > 8192 || !validPluginOriginShape(request.Origin) {
		return contract.PluginView{}, ErrManagementInvalid
	}
	if s.privacy.Check() != nil {
		return contract.PluginView{}, ErrPrivacyGuardClosed
	}
	actorScope := platformActorScope(principal)
	if actorScope == "" {
		return contract.PluginView{}, ErrIdentityForbidden
	}
	// This check precedes every network operation: a successful registration
	// retry returns the immutable version already committed for the same digest.
	if replay, found, replayErr := s.platformPlugins.FindPlatformPluginRegistrationReplay(ctx, actorScope, idempotencyKey, requestDigest); replayErr != nil {
		return contract.PluginView{}, mapManagementError(replayErr)
	} else if found {
		return replay, nil
	}
	raw, manifest, err := s.pluginHTTP.FetchManifest(ctx, request.Origin, request.AuthSecret)
	if err != nil || !compareManifestDigest(request.ManifestDigest, raw) || manifest.PluginID != request.PluginID || manifest.PluginVersion != request.PluginVersion || validatePluginManifest(manifest) != nil {
		return contract.PluginView{}, ErrPluginManifest
	}
	view, err := s.platformPlugins.RegisterPlatformPlugin(ctx, actorScope, idempotencyKey, requestDigest, request, manifest)
	if err != nil {
		return contract.PluginView{}, mapManagementError(err)
	}
	return view, nil
}

func (s *ManagementService) GetPlugin(ctx context.Context, data *authn.Data, bearer, registrationID string) (contract.PluginView, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return contract.PluginView{}, ErrIdentityUnauthorized
	}
	readCapability := "read"
	if containsRole(principal.Roles, "SYS_ADMIN") {
		readCapability = "platform_read"
	}
	if err := requireUserCapability(principal, readCapability); err != nil {
		return contract.PluginView{}, err
	}
	if registrationID == "" {
		return contract.PluginView{}, ErrManagementNotFound
	}
	var view contract.PluginView
	if containsRole(principal.Roles, "SYS_ADMIN") {
		if s.platformPlugins == nil {
			return contract.PluginView{}, ErrManagementUnavailable
		}
		view, err = s.platformPlugins.GetPlatformPlugin(ctx, registrationID)
	} else {
		view, err = s.plugins.GetPlugin(ctx, principal.TenantID, registrationID)
	}
	if err != nil {
		return contract.PluginView{}, mapManagementError(err)
	}
	return view, nil
}

func (s *ManagementService) ListPlugins(ctx context.Context, data *authn.Data, bearer string) ([]contract.PluginView, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return nil, ErrIdentityUnauthorized
	}
	readCapability := "read"
	if containsRole(principal.Roles, "SYS_ADMIN") {
		readCapability = "platform_read"
	}
	if err := requireUserCapability(principal, readCapability); err != nil {
		return nil, err
	}
	var views []contract.PluginView
	if containsRole(principal.Roles, "SYS_ADMIN") {
		if s.platformPlugins == nil {
			return nil, ErrManagementUnavailable
		}
		views, err = s.platformPlugins.ListPlatformPlugins(ctx)
	} else {
		views, err = s.plugins.ListPlugins(ctx, principal.TenantID)
	}
	if err != nil {
		return nil, mapManagementError(err)
	}
	return views, nil
}

func (s *ManagementService) UpdatePlugin(ctx context.Context, data *authn.Data, bearer, registrationID, idempotencyKey, requestDigest string, update contract.PluginUpdate) (contract.PluginView, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return contract.PluginView{}, ErrIdentityUnauthorized
	}
	if err := requireUserCapability(principal, "plugin_manage"); err != nil {
		return contract.PluginView{}, err
	}
	if !containsRole(principal.Roles, "SYS_ADMIN") {
		return contract.PluginView{}, ErrIdentityForbidden
	}
	actorScope := platformActorScope(principal)
	if actorScope == "" {
		return contract.PluginView{}, ErrIdentityForbidden
	}
	if s.platformPlugins == nil {
		return contract.PluginView{}, ErrManagementUnavailable
	}
	if registrationID == "" || !validManagementRequest(idempotencyKey, requestDigest) || update.ExpectedVersion < 1 {
		return contract.PluginView{}, ErrManagementInvalid
	}
	view, err := s.platformPlugins.UpdatePlatformPlugin(ctx, actorScope, registrationID, idempotencyKey, requestDigest, update)
	if err != nil {
		return contract.PluginView{}, mapManagementError(err)
	}
	return view, nil
}

// SetPluginGrant is a platform-only operation. Exact idempotency replay is
// resolved before consulting the tenant directory, which may be temporarily
// unavailable after the original transaction committed.
func (s *ManagementService) SetPluginGrant(ctx context.Context, data *authn.Data, bearer, idempotencyKey, requestDigest string, update contract.PluginGrantUpdate) (contract.PluginGrantView, bool, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return contract.PluginGrantView{}, false, ErrIdentityUnauthorized
	}
	if err := requireUserCapability(principal, "plugin_manage"); err != nil {
		return contract.PluginGrantView{}, false, err
	}
	if !containsRole(principal.Roles, "SYS_ADMIN") {
		return contract.PluginGrantView{}, false, ErrIdentityForbidden
	}
	if s.pluginGrants == nil || s.platformPlugins == nil || s.tenantDirectory == nil {
		return contract.PluginGrantView{}, false, ErrManagementUnavailable
	}
	if !validManagementRequest(idempotencyKey, requestDigest) || !validManifestToken(update.PluginRegistrationID) || len(update.TenantID) == 0 || len(update.TenantID) > 128 || update.ExpectedVersion < 0 {
		return contract.PluginGrantView{}, false, ErrManagementInvalid
	}
	if s.privacy == nil || s.privacy.Check() != nil {
		return contract.PluginGrantView{}, false, ErrPrivacyGuardClosed
	}
	if replay, found, err := s.pluginGrants.FindPluginGrantReplay(ctx, update.TenantID, update.PluginRegistrationID, idempotencyKey, requestDigest); err != nil {
		return contract.PluginGrantView{}, false, mapManagementError(err)
	} else if found {
		return replay, replay.Version == 1, nil
	}
	exists, err := s.tenantDirectory.Exists(ctx, update.TenantID)
	if err != nil {
		return contract.PluginGrantView{}, false, ErrManagementUnavailable
	}
	if !exists {
		return contract.PluginGrantView{}, false, ErrManagementNotFound
	}
	view, created, err := s.pluginGrants.SetPluginGrant(ctx, update.TenantID, update.PluginRegistrationID, idempotencyKey, requestDigest, update)
	if err != nil {
		return contract.PluginGrantView{}, false, mapManagementError(err)
	}
	return view, created, nil
}

func (s *ManagementService) ListPluginGrants(ctx context.Context, data *authn.Data, bearer, registrationID string, page, pageSize int) (contract.PluginGrantPage, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return contract.PluginGrantPage{}, ErrIdentityUnauthorized
	}
	if err := requireUserCapability(principal, "plugin_manage"); err != nil {
		return contract.PluginGrantPage{}, err
	}
	if !containsRole(principal.Roles, "SYS_ADMIN") {
		return contract.PluginGrantPage{}, ErrIdentityForbidden
	}
	if s.pluginGrants == nil {
		return contract.PluginGrantPage{}, ErrManagementUnavailable
	}
	if registrationID != "" && !validManifestToken(registrationID) {
		return contract.PluginGrantPage{}, ErrManagementInvalid
	}
	if s.privacy == nil || s.privacy.Check() != nil {
		return contract.PluginGrantPage{}, ErrPrivacyGuardClosed
	}
	pageResult, err := s.pluginGrants.ListPluginGrants(ctx, registrationID, page, pageSize)
	if err != nil {
		return contract.PluginGrantPage{}, mapManagementError(err)
	}
	return pageResult, nil
}

func validPluginOriginShape(origin string) bool {
	if origin == "" || strings.TrimSpace(origin) != origin || strings.ContainsAny(origin, "?#\\") {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Path != "" || parsed.Hostname() == "" {
		return false
	}
	return parsed.Scheme == "https" || parsed.Scheme == "http"
}
