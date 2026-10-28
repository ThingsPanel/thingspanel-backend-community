package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/l3-core/notification/contract"
)

type groupHTTPTestStore struct {
	groups                  map[string]contract.GroupView
	tenantByID              map[string]string
	replays                 map[string]ManagementReplay
	replayFound             bool
	createCalls             int
	updateCalls             int
	lastTenant              string
	lastProjectionPrincipal SourceProjectionPrincipal
	lastProjectionKey       string
	projection              contract.SourceGroupProjection
	instance                contract.InstanceView
	plugin                  contract.PluginView
}

func newGroupHTTPTestStore() *groupHTTPTestStore {
	return &groupHTTPTestStore{
		groups: map[string]contract.GroupView{}, tenantByID: map[string]string{}, replays: map[string]ManagementReplay{},
		instance: contract.InstanceView{ID: "instance-1", PluginRegistrationID: "plugin-1", Channel: contract.ChannelSMS, Enabled: true},
		plugin: contract.PluginView{ID: "plugin-1", Enabled: true, Manifest: contract.PluginManifestSummary{
			Channels: []contract.Channel{contract.ChannelSMS}, ContentModes: []string{"text"},
			RecipientSchema: json.RawMessage(`{"type":"object","required":["kind","address"],"properties":{"kind":{"const":"phone"},"address":{"type":"string"}}}`),
		}},
	}
}

func (s *groupHTTPTestStore) CreateGroup(_ context.Context, tenant, _, _ string, request contract.GroupCreate) (contract.GroupView, error) {
	s.createCalls++
	view := contract.GroupView{ID: "group-new", Name: request.Name, Enabled: request.Enabled, Bindings: request.Bindings, Version: 1, Revision: 1, MigrationState: "native"}
	s.groups[view.ID], s.tenantByID[view.ID] = view, tenant
	return view, nil
}

func (s *groupHTTPTestStore) UpdateGroup(_ context.Context, tenant, id, _, _ string, request contract.GroupUpdate) (contract.GroupView, error) {
	s.updateCalls++
	if s.tenantByID[id] != tenant {
		return contract.GroupView{}, storeErr(StoreErrorNotFound)
	}
	view := s.groups[id]
	view.Name, view.Enabled, view.Bindings = request.Name, request.Enabled, request.Bindings
	view.Version, view.Revision = view.Version+1, view.Revision+1
	s.groups[id] = view
	return view, nil
}

func (s *groupHTTPTestStore) GetGroup(_ context.Context, tenant, id string) (contract.GroupView, error) {
	s.lastTenant = tenant
	if s.tenantByID[id] != tenant {
		return contract.GroupView{}, storeErr(StoreErrorNotFound)
	}
	return s.groups[id], nil
}

func (s *groupHTTPTestStore) GetGroupRevision(_ context.Context, tenant, id string, _ int64) (contract.GroupView, error) {
	if s.tenantByID[id] != tenant {
		return contract.GroupView{}, storeErr(StoreErrorNotFound)
	}
	return s.groups[id], nil
}

func (s *groupHTTPTestStore) ListGroups(_ context.Context, tenant string) ([]contract.GroupView, error) {
	s.lastTenant = tenant
	items := make([]contract.GroupView, 0)
	for id, group := range s.groups {
		if s.tenantByID[id] == tenant {
			items = append(items, group)
		}
	}
	return items, nil
}

func (s *groupHTTPTestStore) FindManagementReplay(_ context.Context, tenant, operation, key, _ string) (ManagementReplay, bool, error) {
	s.lastTenant = tenant
	if s.replayFound {
		return s.replays[operation+":"+key], true, nil
	}
	return ManagementReplay{}, false, nil
}

func (s *groupHTTPTestStore) ListGroupProjections(_ context.Context, tenant, id string) ([]contract.SourceGroupProjection, error) {
	if s.tenantByID[id] != tenant {
		return nil, storeErr(StoreErrorNotFound)
	}
	view := s.groups[id]
	if view.SourceProjections == nil {
		return []contract.SourceGroupProjection{}, nil
	}
	return view.SourceProjections, nil
}

func (s *groupHTTPTestStore) GetInstance(_ context.Context, _, id string) (contract.InstanceView, error) {
	if id != s.instance.ID {
		return contract.InstanceView{}, storeErr(StoreErrorNotFound)
	}
	return s.instance, nil
}

func (s *groupHTTPTestStore) GetPlugin(_ context.Context, _, id string) (contract.PluginView, error) {
	if id != s.plugin.ID {
		return contract.PluginView{}, storeErr(StoreErrorNotFound)
	}
	return s.plugin, nil
}

func (s *groupHTTPTestStore) RegisterGroupProjectionIdempotent(_ context.Context, principal SourceProjectionPrincipal, key, _ string, request contract.SourceGroupProjectionRequest) (contract.SourceGroupProjection, error) {
	s.lastProjectionPrincipal, s.lastProjectionKey = principal, key
	s.projection = contract.SourceGroupProjection{
		SourceDeploymentID: request.SourceDeploymentID, TenantID: request.TenantID, LegacyGroupID: request.LegacyGroupID,
		NotificationGroupID: request.NotificationGroupID, GroupRevision: request.GroupRevision, CreatedAt: "2030-01-01T00:00:00Z",
	}
	return s.projection, nil
}

type groupHTTPTestVerifier struct {
	projectionPrincipal SourceProjectionPrincipal
	projectionErr       error
}

func (v groupHTTPTestVerifier) VerifyUser(context.Context, string) (UserPrincipal, error) {
	return UserPrincipal{}, errors.New("unused")
}
func (v groupHTTPTestVerifier) VerifySource(context.Context, string) (SourcePrincipal, error) {
	return SourcePrincipal{}, errors.New("unused")
}
func (v groupHTTPTestVerifier) VerifySourceProjection(context.Context, string) (SourceProjectionPrincipal, error) {
	return v.projectionPrincipal, v.projectionErr
}
func (v groupHTTPTestVerifier) VerifyReceipt(context.Context, string, string) (ReceiptPrincipal, error) {
	return ReceiptPrincipal{}, errors.New("unused")
}

func TestGroupCreateReplaysBeforeCurrentBindingValidation(t *testing.T) {
	store := newGroupHTTPTestStore()
	store.groups["group-1"] = contract.GroupView{ID: "group-1", Name: "Original", Enabled: false, Bindings: []contract.GroupBinding{}, Version: 1, Revision: 1, MigrationState: "native"}
	store.tenantByID["group-1"] = "tenant-1"
	store.replayFound = true
	store.replays["group.create:group-create-key"] = ManagementReplay{ResourceType: "group", ResourceID: "group-1", Version: 1}
	body := `{"name":"Changed but currently invalid","enabled":true,"bindings":[]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v2/notification-groups", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "group-create-key")
	response := httptest.NewRecorder()

	serveNotificationGroupRoutes(response, request, UserPrincipal{TenantID: "tenant-1", UserID: "admin", Scopes: []string{"notification:manage"}}, store)

	if response.Code != http.StatusCreated || store.createCalls != 0 {
		t.Fatalf("replay status/calls = %d/%d, want 201/0: %s", response.Code, store.createCalls, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"id":"group-1"`) {
		t.Fatalf("replay did not return original group: %s", response.Body.String())
	}
}

func TestGroupHTTPRejectsSourceMappingAndRequiresNotificationManage(t *testing.T) {
	store := newGroupHTTPTestStore()
	body := `{"name":"group","enabled":false,"bindings":[],"sourceMapping":{}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v2/notification-groups", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "group-create-key")
	response := httptest.NewRecorder()
	serveNotificationGroupRoutes(response, request, UserPrincipal{TenantID: "tenant-1", UserID: "reader", Scopes: []string{"notification:read"}}, store)
	if response.Code != http.StatusForbidden || store.createCalls != 0 {
		t.Fatalf("missing manage scope status/calls = %d/%d", response.Code, store.createCalls)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v2/notification-groups", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "group-create-key")
	serveNotificationGroupRoutes(response, request, UserPrincipal{TenantID: "tenant-1", UserID: "admin", Scopes: []string{"notification:manage"}}, store)
	if response.Code != http.StatusBadRequest || store.createCalls != 0 {
		t.Fatalf("sourceMapping was not rejected as an unknown wire field: status/calls=%d/%d body=%s", response.Code, store.createCalls, response.Body.String())
	}
}

func TestGroupGetIsTenantScopedAndReturnsProjectionHistory(t *testing.T) {
	store := newGroupHTTPTestStore()
	store.groups["group-1"] = contract.GroupView{
		ID: "group-1", Name: "Native", Bindings: []contract.GroupBinding{}, Version: 1, Revision: 1,
		SourceProjections: []contract.SourceGroupProjection{{SourceDeploymentID: "deploy-1", TenantID: "tenant-1", LegacyGroupID: "old-1", NotificationGroupID: "group-1", GroupRevision: 1}},
	}
	store.tenantByID["group-1"] = "tenant-1"
	request := httptest.NewRequest(http.MethodGet, "/api/v2/notification-groups/group-1", nil)
	response := httptest.NewRecorder()
	serveNotificationGroupRoutes(response, request, UserPrincipal{TenantID: "tenant-1", UserID: "reader", Scopes: []string{"notification:read"}}, store)
	if response.Code != http.StatusOK || store.lastTenant != "tenant-1" || !strings.Contains(response.Body.String(), `"sourceProjections"`) || !strings.Contains(response.Body.String(), `"migrationState":"native"`) {
		t.Fatalf("tenant-scoped projection read failed: status=%d tenant=%q body=%s", response.Code, store.lastTenant, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v2/notification-groups/group-1", nil)
	serveNotificationGroupRoutes(response, request, UserPrincipal{TenantID: "tenant-2", UserID: "reader", Scopes: []string{"notification:read"}}, store)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant group lookup status = %d, want 404", response.Code)
	}
}

func TestNativeGroupUpdateAllowsRecordedSourceProjectionHistory(t *testing.T) {
	store := newGroupHTTPTestStore()
	store.groups["group-1"] = contract.GroupView{
		ID: "group-1", Name: "Native", Enabled: false, Bindings: []contract.GroupBinding{}, Version: 1, Revision: 1,
		SourceProjections: []contract.SourceGroupProjection{{SourceDeploymentID: "deploy-1", TenantID: "tenant-1", LegacyGroupID: "old-1", NotificationGroupID: "group-1", GroupRevision: 1}},
	}
	store.tenantByID["group-1"] = "tenant-1"
	body := `{"expectedVersion":1,"name":"Renamed native group","enabled":false,"bindings":[]}`
	request := httptest.NewRequest(http.MethodPut, "/api/v2/notification-groups/group-1", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "group-update-key-0001")
	response := httptest.NewRecorder()
	serveNotificationGroupRoutes(response, request, UserPrincipal{TenantID: "tenant-1", UserID: "admin", Scopes: []string{"notification:manage"}}, store)
	if response.Code != http.StatusOK || store.updateCalls != 1 || !strings.Contains(response.Body.String(), `"name":"Renamed native group"`) {
		t.Fatalf("native group update with projection history failed: status=%d calls=%d body=%s", response.Code, store.updateCalls, response.Body.String())
	}
}

func TestProjectionRouteUsesIndependentCredentialAndStableIdempotencyKey(t *testing.T) {
	store := newGroupHTTPTestStore()
	principal := SourceProjectionPrincipal{DeploymentID: "deploy-1", TenantIDs: []string{"tenant-1"}}
	verifier := groupHTTPTestVerifier{projectionPrincipal: principal}
	body := `{"sourceDeploymentId":"deploy-1","tenantId":"tenant-1","legacyGroupId":"old-1","notificationGroupId":"group-1","groupRevision":4}`
	request := httptest.NewRequest(http.MethodPost, "/api/v2/notification-group-projections", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer source-projection-secret")
	request.Header.Set("Idempotency-Key", "projection-key-0001")
	response := httptest.NewRecorder()
	serveNotificationGroupProjectionRoute(response, request, verifier, store)
	if response.Code != http.StatusOK || store.lastProjectionPrincipal.DeploymentID != "deploy-1" || store.lastProjectionKey != "projection-key-0001" {
		t.Fatalf("source projection was not authenticated/idempotent: status=%d body=%s", response.Code, response.Body.String())
	}

	store.lastProjectionKey = ""
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v2/notification-group-projections", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "projection-key-0001")
	serveNotificationGroupProjectionRoute(response, request, groupHTTPTestVerifier{projectionErr: ErrIdentityUnauthorized}, store)
	if response.Code != http.StatusUnauthorized || store.lastProjectionKey != "" {
		t.Fatalf("invalid source credential reached store: status=%d key=%q", response.Code, store.lastProjectionKey)
	}
}

func TestNotificationGroupPageQueryRejectsUnknownDuplicateAndOverflow(t *testing.T) {
	for _, query := range []string{"page=1&page=2", "page=1&tenantId=other", "page=999999999999999999999999&pageSize=10"} {
		if _, _, err := parseNotificationGroupPage(httptest.NewRequest(http.MethodGet, "/?"+query, nil).URL.Query()); err == nil {
			t.Fatalf("invalid group page query accepted: %s", query)
		}
	}
	page, pageSize, err := parseNotificationGroupPage(httptest.NewRequest(http.MethodGet, "/?page=2&pageSize=10", nil).URL.Query())
	if err != nil || page != 2 || pageSize != 10 {
		t.Fatalf("valid group page = %d/%d (%v)", page, pageSize, err)
	}
}

func TestProjectionRouteRejectsCredentialScopeBeforeStore(t *testing.T) {
	for _, principal := range []SourceProjectionPrincipal{
		{DeploymentID: "other-deploy", TenantIDs: []string{"tenant-1"}},
		{DeploymentID: "deploy-1", TenantIDs: []string{"other-tenant"}},
	} {
		store := newGroupHTTPTestStore()
		request := httptest.NewRequest(http.MethodPost, "/api/v2/notification-group-projections", strings.NewReader(`{"sourceDeploymentId":"deploy-1","tenantId":"tenant-1","legacyGroupId":"old-1","notificationGroupId":"group-1","groupRevision":4}`))
		request.Header.Set("Idempotency-Key", "scope-projection-key")
		response := httptest.NewRecorder()
		serveNotificationGroupProjectionRoute(response, request, groupHTTPTestVerifier{projectionPrincipal: principal}, store)
		if response.Code != http.StatusForbidden || store.lastProjectionKey != "" {
			t.Fatalf("scope denial reached store: %d %q", response.Code, store.lastProjectionKey)
		}
	}
}
