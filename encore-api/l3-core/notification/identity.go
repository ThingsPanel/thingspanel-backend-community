package notification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encore.app/internal/authcore"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/notification/contract"
)

type CredentialPurpose string

const (
	CredentialPurposeSource     CredentialPurpose = "source"
	CredentialPurposeProjection CredentialPurpose = "projection"
	CredentialPurposeReceipt    CredentialPurpose = "receipt"
)

var (
	ErrIdentityUnauthorized  = errors.New("notification identity is not authorized")
	ErrIdentityForbidden     = errors.New("notification identity lacks required permission")
	ErrTenantContextRequired = errors.New("notification tenant context is required")
	ErrCredentialInvalid     = errors.New("notification credential is invalid")
)

// NotificationCredentialRecord is stored with TokenHash only. The opaque
// token is returned once at issuance and is never persisted.
type NotificationCredentialRecord struct {
	ID               string
	Purpose          CredentialPurpose
	TokenHash        string
	TenantID         string
	DeploymentID     string
	AllowedTenantIDs []string
	InstanceID       string
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	RotatedFrom      string
}

type NotificationCredentialRepository interface {
	CreateNotificationCredential(ctx context.Context, record NotificationCredentialRecord) error
	CreateSourceProjectionCredentialPair(ctx context.Context, source, projection NotificationCredentialRecord) error
	RevokeSourceProjectionCredentialPair(ctx context.Context, sourceID, projectionID string) error
	FindNotificationCredential(ctx context.Context, purpose CredentialPurpose, tokenHash string) (NotificationCredentialRecord, error)
	RevokeNotificationCredential(ctx context.Context, purpose CredentialPurpose, credentialID string) error
	// Rotation must revoke the previous row and insert the replacement in one
	// transaction, preserving exactly the same purpose and resource scope.
	RotateNotificationCredential(ctx context.Context, purpose CredentialPurpose, previousID string, next NotificationCredentialRecord) error
	// RotateReceiptCredential atomically rotates/creates the credential and
	// stores its one-time response encrypted for exact idempotent replay.
	RotateReceiptCredential(ctx context.Context, tenantID, idempotencyKey, requestDigest, previousID string, next NotificationCredentialRecord, responseSnapshot json.RawMessage) (json.RawMessage, bool, error)
	RevokeReceiptCredential(ctx context.Context, tenantID, instanceID, credentialID string) error
}

// SourceProjectionCredentialPairRepository creates the two independent
// deployment credentials in one transaction. Provisioning is an operator
// operation and is deliberately not exposed as a notification HTTP route.
type SourceProjectionCredentialPairRepository interface {
	CreateSourceProjectionCredentialPair(ctx context.Context, source, projection NotificationCredentialRecord) error
}

type PluginManagementRepository interface {
	RegisterPlugin(ctx context.Context, tenantID, idempotencyKey, requestDigest string, request contract.PluginRegistration, manifest contract.PluginManifestV1) (contract.PluginView, error)
	FindPluginRegistrationReplay(ctx context.Context, tenantID, idempotencyKey, requestDigest string) (contract.PluginView, bool, error)
	GetPlugin(ctx context.Context, tenantID, registrationID string) (contract.PluginView, error)
	ListPlugins(ctx context.Context, tenantID string) ([]contract.PluginView, error)
	UpdatePlugin(ctx context.Context, tenantID, registrationID, idempotencyKey, requestDigest string, update contract.PluginUpdate) (contract.PluginView, error)
}

// PlatformPluginManagementRepository operates on the global registry. Its
// methods take no tenant scope so callers cannot accidentally turn an empty
// tenant into an authorization bypass.
type PlatformPluginManagementRepository interface {
	RegisterPlatformPlugin(ctx context.Context, actorTenantID, idempotencyKey, requestDigest string, request contract.PluginRegistration, manifest contract.PluginManifestV1) (contract.PluginView, error)
	FindPlatformPluginRegistrationReplay(ctx context.Context, actorTenantID, idempotencyKey, requestDigest string) (contract.PluginView, bool, error)
	GetPlatformPlugin(ctx context.Context, registrationID string) (contract.PluginView, error)
	ListPlatformPlugins(ctx context.Context) ([]contract.PluginView, error)
	UpdatePlatformPlugin(ctx context.Context, actorTenantID, registrationID, idempotencyKey, requestDigest string, update contract.PluginUpdate) (contract.PluginView, error)
}

type PluginGrantRepository interface {
	FindPluginGrantReplay(ctx context.Context, tenantID, registrationID, idempotencyKey, requestDigest string) (contract.PluginGrantView, bool, error)
	SetPluginGrant(ctx context.Context, tenantID, registrationID, idempotencyKey, requestDigest string, update contract.PluginGrantUpdate) (contract.PluginGrantView, bool, error)
	ListPluginGrants(ctx context.Context, registrationID string, page, pageSize int) (contract.PluginGrantPage, error)
}

type TenantDirectory interface {
	Exists(ctx context.Context, tenantID string) (bool, error)
}

type InstanceManagementRepository interface {
	CreateInstance(ctx context.Context, tenantID, idempotencyKey, requestDigest string, request contract.InstanceCreate, secretValues map[string]string) (contract.InstanceView, error)
	GetInstance(ctx context.Context, tenantID, instanceID string) (contract.InstanceView, error)
	ListInstances(ctx context.Context, tenantID string) ([]contract.InstanceView, error)
	UpdateInstance(ctx context.Context, tenantID, instanceID, idempotencyKey, requestDigest string, update contract.InstanceUpdate) (contract.InstanceView, error)
}

type ManagementReplayStore interface {
	FindManagementReplay(ctx context.Context, tenantID, operation, idempotencyKey, requestDigest string) (ManagementReplay, bool, error)
	FindPluginRegistrationReplay(ctx context.Context, tenantID, idempotencyKey, requestDigest string) (contract.PluginView, bool, error)
	FindInstanceReplay(ctx context.Context, tenantID, instanceID, operation, idempotencyKey, requestDigest string) (contract.InstanceView, bool, error)
}

type PluginRuntimeResolver interface {
	ResolvePluginRuntime(ctx context.Context, tenantID, instanceID string, configVersion int64) (PluginRuntime, error)
}

// ValidationRuntimeResolver exposes decrypted configuration only for the
// synchronous validate call. Implementations must enforce tenant grants and
// never persist or log the returned values.
type ValidationRuntimeResolver interface {
	ResolveInstanceConfig(ctx context.Context, tenantID, instanceID string, version int64) (json.RawMessage, error)
	ResolveRegistrationRuntime(ctx context.Context, tenantID, registrationID string) (PluginRuntime, error)
}

// UserSessionChecker asks the legacy service to validate the original user
// token against its JWT + Redis session middleware. It fails closed.
type UserSessionChecker interface {
	CheckUserSession(ctx context.Context, bearer string, expected UserPrincipal) error
}

type PluginManifestClient interface {
	FetchManifest(ctx context.Context, origin, bearer string) (raw []byte, manifest contract.PluginManifestV1, err error)
	ValidateInstance(ctx context.Context, runtime PluginRuntime, request contract.PluginValidateRequestV1) (contract.PluginValidateResultV1, error)
}

type ManagementOptions struct {
	Clock                func() time.Time
	CredentialTTL        time.Duration
	ReceiptCredentialTTL time.Duration
	ManifestMaxBytes     int64
	AllowHTTPTestOrigin  bool
	TestMode             bool
	TenantDirectory      TenantDirectory
}

type ManagementService struct {
	plugins         PluginManagementRepository
	platformPlugins PlatformPluginManagementRepository
	pluginGrants    PluginGrantRepository
	instances       InstanceManagementRepository
	replays         ManagementReplayStore
	credentials     NotificationCredentialRepository
	runtimes        PluginRuntimeResolver
	validation      ValidationRuntimeResolver
	sessions        UserSessionChecker
	pluginHTTP      PluginManifestClient
	protector       SnapshotProtector
	privacy         *PrivacyGuard
	clock           func() time.Time
	options         ManagementOptions
	tenantDirectory TenantDirectory
}

func NewManagementService(plugins PluginManagementRepository, instances InstanceManagementRepository, replays ManagementReplayStore, credentials NotificationCredentialRepository, runtimes PluginRuntimeResolver, sessions UserSessionChecker, pluginHTTP PluginManifestClient, protector SnapshotProtector, privacy *PrivacyGuard, options ManagementOptions) (*ManagementService, error) {
	if plugins == nil || instances == nil || replays == nil || credentials == nil || runtimes == nil || sessions == nil || pluginHTTP == nil || protector == nil || privacy == nil {
		return nil, ErrPrivacyGuardClosed
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.CredentialTTL <= 0 || options.CredentialTTL > 30*24*time.Hour {
		options.CredentialTTL = 24 * time.Hour
	}
	if options.ReceiptCredentialTTL <= 0 || options.ReceiptCredentialTTL > 365*24*time.Hour {
		options.ReceiptCredentialTTL = 90 * 24 * time.Hour
	}
	if options.ManifestMaxBytes <= 0 || options.ManifestMaxBytes > 1<<20 {
		options.ManifestMaxBytes = 256 << 10
	}
	if err := privacy.Check(); err != nil {
		return nil, ErrPrivacyGuardClosed
	}
	return &ManagementService{
		plugins: plugins, platformPlugins: platformPluginRepository(plugins), pluginGrants: pluginGrantRepository(plugins),
		instances: instances, replays: replays, credentials: credentials,
		runtimes: runtimes, sessions: sessions, pluginHTTP: pluginHTTP, protector: protector,
		privacy: privacy, clock: options.Clock, options: options, tenantDirectory: options.TenantDirectory,
		validation: validationResolver(runtimes),
	}, nil
}

func platformPluginRepository(value any) PlatformPluginManagementRepository {
	repository, _ := value.(PlatformPluginManagementRepository)
	return repository
}

func pluginGrantRepository(value any) PluginGrantRepository {
	repository, _ := value.(PluginGrantRepository)
	return repository
}

func validationResolver(value any) ValidationRuntimeResolver {
	resolver, _ := value.(ValidationRuntimeResolver)
	return resolver
}

func principalFromAuth(data *authn.Data) (UserPrincipal, error) {
	if data == nil || strings.TrimSpace(data.UserID) == "" || data.UserID == "unit-test" {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	roles := make([]string, 0, len(data.Roles))
	for _, role := range data.Roles {
		switch canonicalAuthority(role) {
		case "SYS_ADMIN", "TENANT_ADMIN", "TENANT_USER":
			roles = append(roles, canonicalAuthority(role))
		}
	}
	if len(roles) == 0 {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	if strings.TrimSpace(data.TenantID) == "" && !containsRole(roles, "SYS_ADMIN") {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	sort.Strings(roles)
	return UserPrincipal{UserID: data.UserID, TenantID: data.TenantID, Roles: roles, Scopes: scopesForAuthority(roles[0])}, nil
}

func requireUserCapability(principal UserPrincipal, capability string) error {
	if principal.UserID == "" {
		return ErrIdentityUnauthorized
	}
	if principal.TenantID == "" {
		if !containsRole(principal.Roles, "SYS_ADMIN") {
			return ErrIdentityUnauthorized
		}
		if capability == "plugin_manage" || capability == "platform_read" {
			return nil
		}
		return ErrTenantContextRequired
	}
	allowed := false
	for _, role := range principal.Roles {
		switch strings.ToUpper(role) {
		case "SYS_ADMIN":
			if capability == "read" || capability == "manage" || capability == "send_test" || capability == "send" || capability == "plugin_manage" {
				allowed = true
			}
		case "TENANT_ADMIN":
			if capability == "read" || capability == "manage" || capability == "send_test" || capability == "send" {
				allowed = true
			}
		case "TENANT_USER":
			if capability == "read" {
				allowed = true
			}
		}
	}
	if !allowed {
		return ErrIdentityForbidden
	}
	return nil
}

// platformActorScope separates global plugin-registry idempotency from tenant
// resource namespaces while preserving the authenticated platform actor.
func platformActorScope(principal UserPrincipal) string {
	if principal.UserID == "" || !containsRole(principal.Roles, "SYS_ADMIN") {
		return ""
	}
	return "platform-user:" + principal.UserID
}

func normalizeBearer(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len(value) < 8 || len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") {
		return "", ErrIdentityUnauthorized
	}
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		value = strings.TrimSpace(value[7:])
	}
	if value == "" || strings.ContainsAny(value, " \t") {
		return "", ErrIdentityUnauthorized
	}
	return value, nil
}

type CredentialIssue struct {
	Credential string    `json:"credential" encore:"sensitive"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type ProvisionedSourceCredentials struct {
	DeploymentID           string    `json:"deploymentId"`
	AllowedTenantIDs       []string  `json:"allowedTenantIds"`
	SourceCredential       string    `json:"sourceCredential" encore:"sensitive"`
	ProjectionCredential   string    `json:"projectionCredential" encore:"sensitive"`
	ExpiresAt              time.Time `json:"expiresAt"`
	SourceCredentialID     string    `json:"-"`
	ProjectionCredentialID string    `json:"-"`
}

// ProvisionSourceProjectionCredentials is for the local operator CLI only.
// It verifies both the actual platform administrator and a current tenant
// administrator session, then grants exactly that tenant for one deployment.
func (s *ManagementService) ProvisionSourceProjectionCredentials(ctx context.Context, adminBearer, tenantBearer, deploymentID string, ttl time.Duration) (ProvisionedSourceCredentials, error) {
	return s.provisionSourceCredentials(ctx, adminBearer, tenantBearer, deploymentID, ttl, nil)
}

// RenewSourceProjectionCredentials renews an existing pair without restarting
// callers. Both live operator identities and the exact original scope are
// checked again; expired or revoked credentials cannot be revived.
func (s *ManagementService) RenewSourceProjectionCredentials(ctx context.Context, adminBearer, tenantBearer string, previous ProvisionedSourceCredentials, ttl time.Duration) (ProvisionedSourceCredentials, error) {
	return s.provisionSourceCredentials(ctx, adminBearer, tenantBearer, previous.DeploymentID, ttl, &previous)
}

func (s *ManagementService) provisionSourceCredentials(ctx context.Context, adminBearer, tenantBearer, deploymentID string, ttl time.Duration, previous *ProvisionedSourceCredentials) (ProvisionedSourceCredentials, error) {
	if s == nil || s.credentials == nil || s.privacy == nil || s.privacy.Check() != nil || s.sessions == nil || s.tenantDirectory == nil {
		return ProvisionedSourceCredentials{}, ErrPrivacyGuardClosed
	}
	if !authcore.Enabled() && (strings.TrimSpace(os.Getenv("TP_AUTH_MODE")) != "legacy-jwt" || strings.TrimSpace(os.Getenv("TP_JWT_KEY")) == "") {
		return ProvisionedSourceCredentials{}, ErrIdentityUnauthorized
	}
	if ttl <= 0 || ttl > 8*time.Hour || deploymentID == "" || deploymentID != strings.TrimSpace(os.Getenv("NOTIFICATION_SOURCE_DEPLOYMENT_ID")) {
		return ProvisionedSourceCredentials{}, ErrCredentialInvalid
	}
	admin, err := s.VerifyUser(ctx, adminBearer)
	if err != nil || !containsRole(admin.Roles, "SYS_ADMIN") || admin.TenantID != "" || requireUserCapability(admin, "plugin_manage") != nil {
		return ProvisionedSourceCredentials{}, ErrIdentityForbidden
	}
	adminToken, err := normalizeBearer(adminBearer)
	if err != nil || s.sessions.CheckUserSession(ctx, adminToken, admin) != nil {
		return ProvisionedSourceCredentials{}, ErrIdentityUnauthorized
	}
	tenant, err := s.VerifyUser(ctx, tenantBearer)
	if err != nil || !containsRole(tenant.Roles, "TENANT_ADMIN") || tenant.TenantID == "" || tenant.UserID == admin.UserID || requireUserCapability(tenant, "manage") != nil {
		return ProvisionedSourceCredentials{}, ErrIdentityForbidden
	}
	tenantToken, err := normalizeBearer(tenantBearer)
	if err != nil || s.sessions.CheckUserSession(ctx, tenantToken, tenant) != nil {
		return ProvisionedSourceCredentials{}, ErrIdentityUnauthorized
	}
	exists, err := s.tenantDirectory.Exists(ctx, tenant.TenantID)
	if err != nil {
		return ProvisionedSourceCredentials{}, ErrLegacyDirectoryUnavailable
	}
	if !exists {
		return ProvisionedSourceCredentials{}, ErrManagementNotFound
	}
	if err := s.privacy.Check(); err != nil {
		return ProvisionedSourceCredentials{}, ErrPrivacyGuardClosed
	}

	if previous != nil {
		if !sameTenantAllowlist(previous.AllowedTenantIDs, []string{tenant.TenantID}) || len(previous.SourceCredential) < 32 || len(previous.ProjectionCredential) < 32 {
			return ProvisionedSourceCredentials{}, ErrCredentialInvalid
		}
		repository, ok := s.credentials.(interface {
			RenewSourceProjectionCredentialPair(context.Context, string, []string, string, string, time.Time) error
		})
		if !ok {
			return ProvisionedSourceCredentials{}, ErrManagementUnavailable
		}
		sourceHash := sha256.Sum256([]byte(previous.SourceCredential))
		projectionHash := sha256.Sum256([]byte(previous.ProjectionCredential))
		expires := s.clock().Add(ttl).UTC()
		if err := repository.RenewSourceProjectionCredentialPair(ctx, deploymentID, []string{tenant.TenantID}, hex.EncodeToString(sourceHash[:]), hex.EncodeToString(projectionHash[:]), expires); err != nil {
			return ProvisionedSourceCredentials{}, mapManagementError(err)
		}
		result := *previous
		result.ExpiresAt = expires
		return result, nil
	}
	repository, ok := s.credentials.(SourceProjectionCredentialPairRepository)
	if !ok {
		return ProvisionedSourceCredentials{}, ErrManagementUnavailable
	}
	allowed := []string{tenant.TenantID}
	now := s.clock()
	source, sourceToken, err := newDeploymentCredential(CredentialPurposeSource, deploymentID, allowed, now, ttl)
	if err != nil {
		return ProvisionedSourceCredentials{}, ErrCredentialInvalid
	}
	projection, projectionToken, err := newDeploymentCredential(CredentialPurposeProjection, deploymentID, allowed, now, ttl)
	if err != nil {
		return ProvisionedSourceCredentials{}, ErrCredentialInvalid
	}
	if err := repository.CreateSourceProjectionCredentialPair(ctx, source, projection); err != nil {
		return ProvisionedSourceCredentials{}, mapManagementError(err)
	}
	return ProvisionedSourceCredentials{
		DeploymentID: deploymentID, AllowedTenantIDs: allowed,
		SourceCredential: sourceToken, ProjectionCredential: projectionToken,
		ExpiresAt: source.ExpiresAt.UTC(), SourceCredentialID: source.ID, ProjectionCredentialID: projection.ID,
	}, nil
}

func newDeploymentCredential(purpose CredentialPurpose, deploymentID string, allowedTenantIDs []string, now time.Time, ttl time.Duration) (NotificationCredentialRecord, string, error) {
	if (purpose != CredentialPurposeSource && purpose != CredentialPurposeProjection) || !validCredentialScope(purpose, "", deploymentID, allowedTenantIDs, "") || ttl <= 0 || ttl > 8*time.Hour {
		return NotificationCredentialRecord{}, "", ErrCredentialInvalid
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return NotificationCredentialRecord{}, "", ErrCredentialInvalid
	}
	prefix := "ntf_src_"
	if purpose == CredentialPurposeProjection {
		prefix = "ntf_proj_"
	}
	token := prefix + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return NotificationCredentialRecord{
		ID: newStableID(), Purpose: purpose, TokenHash: hex.EncodeToString(hash[:]),
		DeploymentID: deploymentID, AllowedTenantIDs: uniqueSortedStrings(allowedTenantIDs),
		ExpiresAt: now.UTC().Add(ttl),
	}, token, nil
}

func (s *ManagementService) issueCredential(ctx context.Context, purpose CredentialPurpose, tenantID, deploymentID string, allowedTenantIDs []string, instanceID string, ttl time.Duration, previousID string) (CredentialIssue, error) {
	if s == nil || s.credentials == nil || s.privacy == nil || s.privacy.Check() != nil || !validCredentialScope(purpose, tenantID, deploymentID, allowedTenantIDs, instanceID) {
		return CredentialIssue{}, ErrCredentialInvalid
	}
	if ttl <= 0 || ttl > 365*24*time.Hour {
		return CredentialIssue{}, ErrCredentialInvalid
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return CredentialIssue{}, ErrCredentialInvalid
	}
	token := "ntf_" + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	record := NotificationCredentialRecord{
		ID:               newStableID(),
		Purpose:          purpose,
		TokenHash:        hex.EncodeToString(hash[:]),
		TenantID:         tenantID,
		DeploymentID:     deploymentID,
		AllowedTenantIDs: uniqueSortedStrings(allowedTenantIDs),
		InstanceID:       instanceID,
		ExpiresAt:        s.clock().UTC().Add(ttl),
		RotatedFrom:      previousID,
	}
	if previousID != "" {
		if err := s.credentials.RotateNotificationCredential(ctx, purpose, previousID, record); err != nil {
			return CredentialIssue{}, ErrCredentialInvalid
		}
	} else if err := s.credentials.CreateNotificationCredential(ctx, record); err != nil {
		return CredentialIssue{}, ErrCredentialInvalid
	}
	return CredentialIssue{Credential: token, ExpiresAt: record.ExpiresAt}, nil
}

func (s *ManagementService) RotateReceiptCredential(ctx context.Context, data *authn.Data, bearer, instanceID, idempotencyKey, requestDigest string) (contract.ReceiptCredentialResponse, error) {
	return contract.ReceiptCredentialResponse{}, ErrIdentityForbidden
}

func (s *ManagementService) rotateReceiptCredentialInTenant(ctx context.Context, tenantID, instanceID, idempotencyKey, requestDigest string) (contract.ReceiptCredentialResponse, error) {
	var err error
	if instanceID == "" || !validManagementRequest(idempotencyKey, requestDigest) || s.privacy == nil || s.privacy.Check() != nil {
		return contract.ReceiptCredentialResponse{}, ErrManagementInvalid
	}
	if _, err := s.instances.GetInstance(ctx, tenantID, instanceID); err != nil {
		return contract.ReceiptCredentialResponse{}, mapManagementError(err)
	}
	if s.options.ReceiptCredentialTTL <= 0 || s.options.ReceiptCredentialTTL > 365*24*time.Hour {
		return contract.ReceiptCredentialResponse{}, ErrCredentialInvalid
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return contract.ReceiptCredentialResponse{}, ErrCredentialInvalid
	}
	token := "ntf_rcpt_" + base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))
	record := NotificationCredentialRecord{
		ID:         newStableID(),
		Purpose:    CredentialPurposeReceipt,
		TokenHash:  hex.EncodeToString(tokenHash[:]),
		TenantID:   tenantID,
		InstanceID: instanceID,
		ExpiresAt:  s.clock().UTC().Add(s.options.ReceiptCredentialTTL),
	}
	if record.ID == "" {
		return contract.ReceiptCredentialResponse{}, ErrCredentialInvalid
	}
	response := contract.ReceiptCredentialResponse{Credential: token, ExpiresAt: record.ExpiresAt.UTC().Format(time.RFC3339)}
	responseSnapshot, err := json.Marshal(response)
	if err != nil {
		return contract.ReceiptCredentialResponse{}, ErrCredentialInvalid
	}
	stored, _, err := s.credentials.RotateReceiptCredential(ctx, tenantID, idempotencyKey, requestDigest, "", record, responseSnapshot)
	if err != nil {
		return contract.ReceiptCredentialResponse{}, mapManagementError(err)
	}
	var replay contract.ReceiptCredentialResponse
	if contract.DecodeStrictJSON(stored, &replay) != nil || replay.Credential == "" || replay.ExpiresAt == "" {
		return contract.ReceiptCredentialResponse{}, ErrManagementUnavailable
	}
	return replay, nil
}

func (s *ManagementService) RevokeReceiptCredential(ctx context.Context, data *authn.Data, bearer, instanceID, credentialID string) error {
	return ErrIdentityForbidden
}

func validCredentialScope(purpose CredentialPurpose, tenantID, deploymentID string, allowedTenantIDs []string, instanceID string) bool {
	switch purpose {
	case CredentialPurposeSource, CredentialPurposeProjection:
		if tenantID != "" || deploymentID == "" || len(allowedTenantIDs) == 0 || instanceID != "" {
			return false
		}
		return len(uniqueSortedStrings(allowedTenantIDs)) == len(allowedTenantIDs)
	case CredentialPurposeReceipt:
		return tenantID != "" && deploymentID == "" && len(allowedTenantIDs) == 0 && instanceID != ""
	default:
		return false
	}
}

func (s *ManagementService) VerifySource(ctx context.Context, bearer string) (SourcePrincipal, error) {
	record, err := s.lookupCredential(ctx, bearer, CredentialPurposeSource)
	if err != nil || !validCredentialScope(record.Purpose, record.TenantID, record.DeploymentID, record.AllowedTenantIDs, record.InstanceID) {
		return SourcePrincipal{}, ErrCredentialInvalid
	}
	return SourcePrincipal{DeploymentID: record.DeploymentID, TenantIDs: uniqueSortedStrings(record.AllowedTenantIDs)}, nil
}

func (s *ManagementService) VerifySourceProjection(ctx context.Context, bearer string) (SourceProjectionPrincipal, error) {
	record, err := s.lookupCredential(ctx, bearer, CredentialPurposeProjection)
	if err != nil || !validCredentialScope(record.Purpose, record.TenantID, record.DeploymentID, record.AllowedTenantIDs, record.InstanceID) {
		return SourceProjectionPrincipal{}, ErrCredentialInvalid
	}
	return SourceProjectionPrincipal{DeploymentID: record.DeploymentID, TenantIDs: uniqueSortedStrings(record.AllowedTenantIDs)}, nil
}

func (s *ManagementService) VerifyReceipt(ctx context.Context, bearer, pathInstanceID string) (ReceiptPrincipal, error) {
	record, err := s.lookupCredential(ctx, bearer, CredentialPurposeReceipt)
	if err != nil || record.InstanceID == "" || pathInstanceID == "" || record.InstanceID != pathInstanceID || record.TenantID == "" {
		return ReceiptPrincipal{}, ErrCredentialInvalid
	}
	return ReceiptPrincipal{TenantID: record.TenantID, InstanceID: record.InstanceID}, nil
}

func (s *ManagementService) lookupCredential(ctx context.Context, bearer string, purpose CredentialPurpose) (NotificationCredentialRecord, error) {
	token, err := normalizeBearer(bearer)
	if err != nil || s == nil || s.credentials == nil || s.privacy == nil || s.privacy.Check() != nil {
		return NotificationCredentialRecord{}, ErrCredentialInvalid
	}
	hash := sha256.Sum256([]byte(token))
	record, err := s.credentials.FindNotificationCredential(ctx, purpose, hex.EncodeToString(hash[:]))
	if err != nil || record.Purpose != purpose || record.RevokedAt != nil || !record.ExpiresAt.After(s.clock()) {
		return NotificationCredentialRecord{}, ErrCredentialInvalid
	}
	return record, nil
}

func newStableID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ""
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	hexID := hex.EncodeToString(id[:])
	return hexID[:8] + "-" + hexID[8:12] + "-" + hexID[12:16] + "-" + hexID[16:20] + "-" + hexID[20:]
}

func uniqueSortedStrings(values []string) []string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	out := copyValues[:0]
	for _, value := range copyValues {
		value = strings.TrimSpace(value)
		if value != "" && (len(out) == 0 || out[len(out)-1] != value) {
			out = append(out, value)
		}
	}
	return out
}
