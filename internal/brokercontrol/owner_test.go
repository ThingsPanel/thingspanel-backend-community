package brokercontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bc "project/internal/brokercontract"
	"project/internal/brokermapper"
)

type fakeRepository struct {
	devices        []DeviceRecord
	children       []DeviceRecord
	lookupErr      error
	childErr       error
	mappingConfigs map[string]bool
	mappingErr     error
	snapshots      map[string]*brokermapper.Snapshot
	serviceTargets map[string]DeviceRecord
	clientOwners   map[string][2]string
	bindings       map[string]TransportBinding
	lastCredential string
}

func (f *fakeRepository) FindCredentialMatches(_ context.Context, credential string) ([]DeviceRecord, error) {
	f.lastCredential = credential
	return append([]DeviceRecord(nil), f.devices...), f.lookupErr
}
func (f *fakeRepository) ResolveServiceTargets(_ context.Context, ids []string) ([]DeviceRecord, error) {
	rows := make([]DeviceRecord, 0, len(ids))
	for _, id := range ids {
		if target, ok := f.serviceTargets[id]; ok {
			rows = append(rows, target)
		}
	}
	return rows, nil
}
func (f *fakeRepository) ListEnabledChildren(_ context.Context, _, _ string) ([]DeviceRecord, error) {
	return append([]DeviceRecord(nil), f.children...), f.childErr
}
func (f *fakeRepository) HasEnabledTopicMappings(_ context.Context, _, configID string) (bool, error) {
	return f.mappingConfigs[configID], f.mappingErr
}
func (f *fakeRepository) CompileMappingSnapshot(_ context.Context, _, configID string) (*brokermapper.Snapshot, error) {
	if f.mappingErr != nil {
		return nil, f.mappingErr
	}
	if s := f.snapshots[configID]; s != nil {
		return s, nil
	}
	if f.mappingConfigs[configID] {
		return nil, brokermapper.ErrUnsupportedMapping
	}
	return brokermapper.Standard(), nil
}
func (f *fakeRepository) ClaimClientID(_ context.Context, cell, wire, tenant, device string) (bool, error) {
	if f.clientOwners == nil {
		f.clientOwners = map[string][2]string{}
	}
	key := cell + "\x00" + wire
	owner, ok := f.clientOwners[key]
	if ok {
		return owner == [2]string{tenant, device}, nil
	}
	f.clientOwners[key] = [2]string{tenant, device}
	return true, nil
}
func (f *fakeRepository) ResolveActiveBinding(_ context.Context, tenant, device, cell string) (TransportBinding, error) {
	if f.bindings == nil {
		f.bindings = map[string]TransportBinding{}
	}
	key := tenant + "\x00" + device
	b, ok := f.bindings[key]
	if !ok {
		b = TransportBinding{TenantID: tenant, DeviceID: device, BrokerCell: cell, RouteEpoch: 1, Phase: "active"}
		f.bindings[key] = b
	}
	if b.BrokerCell != cell || b.Phase != "active" || b.RouteEpoch == 0 {
		return TransportBinding{}, ErrBindingConflict
	}
	return b, nil
}

func testToken() string { return strings.Repeat("t", 40) }
func testOwner(t *testing.T, repo Repository, services []ServiceCredential) *Owner {
	t.Helper()
	o, err := NewOwner(repo, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: services, GrantTTL: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func authRequest() bc.AuthRequest {
	return bc.AuthRequest{RequestID: "req-1", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "nonce-1", WireClientID: "client-1", Username: "device-user", Password: "device-pass", Protocol: 4}
}
func activeDevice() DeviceRecord {
	return DeviceRecord{ID: "d1", TenantID: "t1", DeviceNumber: "number-1", IsEnabled: "enabled", ActivateFlag: "active"}
}

func TestAuthenticateCreatesNarrowDeviceGrantAndStableConnectionID(t *testing.T) {
	repo := &fakeRepository{devices: []DeviceRecord{activeDevice()}}
	owner := testOwner(t, repo, nil)
	req := authRequest()
	first, err := owner.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Allow {
		t.Fatalf("valid device denied: %#v", first)
	}
	if err := bc.ValidateGrant(first); err != nil {
		t.Fatal(err)
	}
	second, err := owner.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if second.ConnectionID != first.ConnectionID || second.PolicyHash != first.PolicyHash || second.PolicyVersion != first.PolicyVersion || second.RouteEpoch != 1 {
		t.Fatalf("refresh grant drifted: first=%#v second=%#v", first, second)
	}
	if repo.lastCredential != `{"username":"device-user","password":"device-pass"}` {
		t.Fatalf("credential query must be a strict two-field JSON value: %s", repo.lastCredential)
	}
	if !hasExact(first.Policy.Subscribe, "devices/command/number-1/+") {
		t.Fatalf("missing own command subscription: %#v", first.Policy.Subscribe)
	}
	if hasExact(first.Policy.Subscribe, "ota/devices/inform/number-1") {
		t.Fatalf("community device policy must not grant OTA inform: %#v", first.Policy.Subscribe)
	}
	if err := bc.AuthorizeTopic(first.Principal, first.Policy, bc.ActionReceive, "devices/command/number-1/set"); err != nil {
		t.Fatal(err)
	}
	if err := bc.AuthorizeTopic(first.Principal, first.Policy, bc.ActionReceive, "devices/command/other-device/set"); err == nil {
		t.Fatal("device can receive another device command")
	}
	if err := bc.AuthorizeTopic(first.Principal, first.Policy, bc.ActionReceive, "$tp/ingress/v1/t1/d1/b-eA"); err == nil {
		t.Fatal("device can read internal ingress")
	}
}

func hasExact(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestAuthenticateGatewayExpandsOnlyActiveChildNumbers(t *testing.T) {
	root := activeDevice()
	children := []DeviceRecord{{ID: "c1", TenantID: "t1", DeviceNumber: "child-1", IsEnabled: "enabled", ActivateFlag: "active"}, {ID: "c2", TenantID: "t1", DeviceNumber: "child-2", IsEnabled: "enabled", ActivateFlag: "active"}}
	repo := &fakeRepository{devices: []DeviceRecord{root}, children: children}
	grant, err := testOwner(t, repo, nil).Authenticate(context.Background(), authRequest())
	if err != nil || !grant.Allow {
		t.Fatalf("gateway auth failed: %#v %v", grant, err)
	}
	if grant.Principal.Kind != bc.PrincipalGateway || !hasExact(grant.Policy.Subscribe, "gateway/command/child-1/+") || !hasExact(grant.Policy.Subscribe, "gateway/command/child-2/+") {
		t.Fatalf("child scopes not expanded: %#v", grant)
	}
	if hasExact(grant.Policy.Subscribe, "gateway/command/other/+") {
		t.Fatal("gateway ACL expanded an unrelated device")
	}
}

func TestAuthenticateFailsClosedForDuplicatesInactiveRoutesAndClientIDTheft(t *testing.T) {
	t.Run("duplicate voucher semantic matches", func(t *testing.T) {
		r := &fakeRepository{devices: []DeviceRecord{activeDevice(), activeDevice()}}
		g, e := testOwner(t, r, nil).Authenticate(context.Background(), authRequest())
		if e != nil || g.Allow {
			t.Fatalf("duplicate matched credential accepted: %#v %v", g, e)
		}
	})
	t.Run("disabled device", func(t *testing.T) {
		d := activeDevice()
		d.IsEnabled = "disabled"
		r := &fakeRepository{devices: []DeviceRecord{d}}
		g, e := testOwner(t, r, nil).Authenticate(context.Background(), authRequest())
		if e != nil || g.Allow {
			t.Fatalf("disabled device accepted: %#v %v", g, e)
		}
	})
	t.Run("inactive device", func(t *testing.T) {
		d := activeDevice()
		d.ActivateFlag = "inactive"
		r := &fakeRepository{devices: []DeviceRecord{d}}
		g, e := testOwner(t, r, nil).Authenticate(context.Background(), authRequest())
		if e != nil || g.Allow {
			t.Fatalf("inactive device accepted: %#v %v", g, e)
		}
	})
	t.Run("cross tenant client takeover", func(t *testing.T) {
		r := &fakeRepository{devices: []DeviceRecord{activeDevice()}, clientOwners: map[string][2]string{"community\x00client-1": {"tenant-other", "d9"}}}
		g, e := testOwner(t, r, nil).Authenticate(context.Background(), authRequest())
		if e != nil || g.Allow {
			t.Fatalf("cross tenant ClientID takeover accepted: %#v %v", g, e)
		}
	})
	t.Run("draining binding", func(t *testing.T) {
		r := &fakeRepository{devices: []DeviceRecord{activeDevice()}, bindings: map[string]TransportBinding{"t1\x00d1": {TenantID: "t1", DeviceID: "d1", BrokerCell: "community", RouteEpoch: 1, Phase: "draining"}}}
		g, e := testOwner(t, r, nil).Authenticate(context.Background(), authRequest())
		if e != nil || g.Allow {
			t.Fatalf("draining route renewed: %#v %v", g, e)
		}
	})
	t.Run("database failure", func(t *testing.T) {
		r := &fakeRepository{lookupErr: errors.New("db password must not escape")}
		g, e := testOwner(t, r, nil).Authenticate(context.Background(), authRequest())
		if g.Allow || !errors.Is(e, ErrUnavailable) {
			t.Fatalf("database error did not fail closed: %#v %v", g, e)
		}
	})
	t.Run("uncertified custom topic mapping", func(t *testing.T) {
		d := activeDevice()
		configID := "config-1"
		d.DeviceConfigID = &configID
		r := &fakeRepository{devices: []DeviceRecord{d}, mappingConfigs: map[string]bool{configID: true}}
		g, e := testOwner(t, r, nil).Authenticate(context.Background(), authRequest())
		if e != nil || g.Allow || g.DenyCode != "mapping_not_certified" {
			t.Fatalf("custom mapping must be denied explicitly: %#v %v", g, e)
		}
	})
}

func TestExplicitServiceAllowlistAndNoDeviceFallback(t *testing.T) {
	service := ServiceCredential{Username: "svc-user", Password: "svc-pass", BrokerCell: "community", BrokerInstance: "node-1", AllowedClientIDs: []string{"svc-client"}, AllowedDeviceIDs: []string{"target-1"}, Principal: bc.Principal{TenantID: "t1", DeviceID: "service-1", Kind: bc.PrincipalService}, Policy: bc.Policy{Publish: []string{"devices/event/response/+/+"}, Subscribe: []string{"tp-internal/ingress/v1/#"}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}, PolicyVersion: "service-policy-1", MappingVersion: MappingVersion}
	repo := &fakeRepository{devices: []DeviceRecord{activeDevice()}, serviceTargets: map[string]DeviceRecord{"target-1": {ID: "target-1", TenantID: "t1", DeviceNumber: "number-1", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}}}
	owner := testOwner(t, repo, []ServiceCredential{service})
	req := authRequest()
	req.Username = "svc-user"
	req.Password = "svc-pass"
	req.WireClientID = "svc-client"
	g, err := owner.Authenticate(context.Background(), req)
	if err != nil || !g.Allow || g.Principal.Kind != bc.PrincipalService {
		t.Fatalf("configured service denied: %#v %v", g, err)
	}
	if err := bc.AuthorizeTopic(g.Principal, g.Policy, bc.ActionReceive, "tp-internal/ingress/v1/d1/tenant/key"); err != nil {
		t.Fatal(err)
	}
	if err := bc.AuthorizeTopic(g.Principal, g.Policy, bc.ActionPublish, "devices/event/response/number-1/message-1"); err != nil {
		t.Fatalf("finite canonical response publish denied: %v", err)
	}
	if err := bc.AuthorizeTopic(g.Principal, g.Policy, bc.ActionPublish, "devices/event/response/other-device/message-1"); err == nil {
		t.Fatal("service grant escaped its explicit device target")
	}
	req.Password = "wrong"
	g, err = owner.Authenticate(context.Background(), req)
	if err != nil || g.Allow {
		t.Fatalf("service bad password fell through to device voucher: %#v %v", g, err)
	}
	req.Password = "svc-pass"
	req.WireClientID = "unlisted"
	g, err = owner.Authenticate(context.Background(), req)
	if err != nil || g.Allow {
		t.Fatalf("unlisted service ClientID accepted: %#v %v", g, err)
	}
}

func TestServicePublishPolicyRejectsInternalAndBroadSpoofFilters(t *testing.T) {
	base := ServiceCredential{Username: "svc-user", Password: "svc-pass", BrokerCell: "community", BrokerInstance: "node-1", AllowedClientIDs: []string{"svc-client"}, Principal: bc.Principal{TenantID: "t1", DeviceID: "service-1", Kind: bc.PrincipalService}, Policy: bc.Policy{MaxPayload: 1024, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}, PolicyVersion: "service-policy-1", MappingVersion: MappingVersion}
	for _, filter := range []string{"tp-internal/#", "tp-internal/+", "$tp/#", "$tp", "#", "+/#", "+/devices/#", "devices/#", "devices/event/+", ".+"} {
		service := base
		service.Policy.Publish = []string{filter}
		_, err := NewOwner(&fakeRepository{}, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{service}})
		if err == nil {
			t.Errorf("unsafe service publish filter accepted: %q", filter)
		}
	}
}

func serviceFixture() ServiceCredential {
	return ServiceCredential{Username: "svc-user", Password: "svc-pass", BrokerCell: "community", BrokerInstance: "node-1", AllowedClientIDs: []string{"svc-client"}, AllowedDeviceIDs: []string{"target-1"}, Principal: bc.Principal{TenantID: "tenant-a", DeviceID: "service-1", Kind: bc.PrincipalService}, Policy: bc.Policy{Publish: []string{"devices/event/response/+/+"}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}, PolicyVersion: "service-policy-1", MappingVersion: MappingVersion}
}

func serviceRequest() bc.AuthRequest {
	return bc.AuthRequest{RequestID: "service-request", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "service-nonce", WireClientID: "svc-client", Username: "svc-user", Password: "svc-pass", Protocol: 4}
}

func TestServicePublishRequiresTenantScopedTargetsAndCompilesOnlyThoseDevices(t *testing.T) {
	t.Run("read-only service may omit targets", func(t *testing.T) {
		s := serviceFixture()
		s.AllowedDeviceIDs = nil
		s.Policy.Publish = nil
		_, err := NewOwner(&fakeRepository{}, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{s}})
		if err != nil {
			t.Fatalf("read-only service required publish targets: %v", err)
		}
	})
	t.Run("publish without explicit targets is rejected at configuration", func(t *testing.T) {
		s := serviceFixture()
		s.AllowedDeviceIDs = nil
		_, err := NewOwner(&fakeRepository{}, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{s}})
		if err == nil {
			t.Fatal("unscoped service publish configuration accepted")
		}
	})
	t.Run("target identifiers cannot contain MQTT wildcards", func(t *testing.T) {
		s := serviceFixture()
		s.AllowedDeviceIDs = []string{"#"}
		_, err := NewOwner(&fakeRepository{}, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{s}})
		if err == nil {
			t.Fatal("wildcard device identifier accepted")
		}
	})
	t.Run("target from another tenant is denied by default scope", func(t *testing.T) {
		s := serviceFixture()
		repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{"target-1": {ID: "target-1", TenantID: "tenant-b", DeviceNumber: "number-b", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}}}
		g, err := testOwner(t, repo, []ServiceCredential{s}).Authenticate(context.Background(), serviceRequest())
		if err != nil || g.Allow {
			t.Fatalf("foreign tenant target accepted: %#v %v", g, err)
		}
	})
	t.Run("explicit multi-tenant scope still compiles only selected devices", func(t *testing.T) {
		s := serviceFixture()
		s.AllowedTenantIDs = []string{"tenant-a", "tenant-b"}
		repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{"target-1": {ID: "target-1", TenantID: "tenant-b", DeviceNumber: "number-b", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}}}
		g, err := testOwner(t, repo, []ServiceCredential{s}).Authenticate(context.Background(), serviceRequest())
		if err != nil || !g.Allow {
			t.Fatalf("explicit tenant scope denied: %#v %v", g, err)
		}
		if !hasExact(g.Policy.Publish, "devices/event/response/number-b/+") || len(g.Policy.Publish) != 1 {
			t.Fatalf("grant not narrowed to one device: %#v", g.Policy.Publish)
		}
		if err := bc.AuthorizeTopic(g.Principal, g.Policy, bc.ActionPublish, "devices/event/response/other/+"); err == nil {
			t.Fatal("other device publish passed")
		}
	})
	t.Run("duplicate device number is rejected", func(t *testing.T) {
		s := serviceFixture()
		s.AllowedDeviceIDs = []string{"target-1", "target-2"}
		repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{
			"target-1": {ID: "target-1", TenantID: "tenant-a", DeviceNumber: "duplicate", DeviceNumberCount: 2, IsEnabled: "enabled", ActivateFlag: "active"},
			"target-2": {ID: "target-2", TenantID: "tenant-a", DeviceNumber: "duplicate", DeviceNumberCount: 2, IsEnabled: "enabled", ActivateFlag: "active"},
		}}
		g, err := testOwner(t, repo, []ServiceCredential{s}).Authenticate(context.Background(), serviceRequest())
		if err != nil || g.Allow {
			t.Fatalf("duplicate device number accepted: %#v %v", g, err)
		}
	})
	t.Run("literal device number must belong to scope", func(t *testing.T) {
		s := serviceFixture()
		s.Policy.Publish = []string{"devices/event/response/outside/+"}
		repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{"target-1": {ID: "target-1", TenantID: "tenant-a", DeviceNumber: "number-a", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}}}
		g, err := testOwner(t, repo, []ServiceCredential{s}).Authenticate(context.Background(), serviceRequest())
		if err != nil || g.Allow {
			t.Fatalf("out-of-scope literal topic accepted: %#v %v", g, err)
		}
	})
}

func TestServiceGrantRejectsResponseOver64KiB(t *testing.T) {
	s := serviceFixture()
	s.Policy.Publish = []string{
		"devices/command/+/+", "gateway/command/+/+", "devices/telemetry/control/+", "gateway/telemetry/control/+",
		"devices/attributes/get/+", "gateway/attributes/get/+", "devices/attributes/set/+/+", "gateway/attributes/set/+/+",
		"devices/attributes/response/+/+", "gateway/attributes/response/+/+", "devices/event/response/+/+", "gateway/event/response/+/+", "ota/devices/inform/+",
	}
	s.AllowedDeviceIDs = make([]string, 0, MaxServiceTargets)
	targets := make(map[string]DeviceRecord, MaxServiceTargets)
	for i := 0; i < MaxServiceTargets; i++ {
		id := fmt.Sprintf("target-%03d", i)
		s.AllowedDeviceIDs = append(s.AllowedDeviceIDs, id)
		targets[id] = DeviceRecord{ID: id, TenantID: "tenant-a", DeviceNumber: fmt.Sprintf("device-number-%03d-1234567890123456789", i), DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}
	}
	repo := &fakeRepository{serviceTargets: targets}
	g, err := testOwner(t, repo, []ServiceCredential{s}).Authenticate(context.Background(), serviceRequest())
	if err != nil || g.Allow || g.DenyCode != "grant_too_large" {
		t.Fatalf("oversized scoped grant not denied explicitly: %#v %v", g, err)
	}
	if len(repo.clientOwners) != 0 {
		t.Fatal("oversized grant claimed ClientID before size validation")
	}
}

func TestHTTPBearerCellScopeBodyLimitAndGenericFailures(t *testing.T) {
	repo := &fakeRepository{devices: []DeviceRecord{activeDevice()}}
	owner := testOwner(t, repo, nil)
	handler := owner.Handler()
	body, _ := json.Marshal(authRequest())
	request := func(token string, data []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/authenticate", bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := request(testToken(), body)
	if w.Code != http.StatusOK {
		t.Fatalf("valid scoped token status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(strings.Repeat("x", 40), body)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status=%d", w.Code)
	}
	w = request(testToken(), bytes.Repeat([]byte("x"), MaxAuthBodyBytes+1))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status=%d", w.Code)
	}
	badRepo := &fakeRepository{lookupErr: errors.New("DB_SECRET_DO_NOT_RETURN")}
	badHandler := testOwner(t, badRepo, nil).Handler()
	r := httptest.NewRequest(http.MethodPost, "/v1/authenticate", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken())
	w = httptest.NewRecorder()
	badHandler.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "DB_SECRET_DO_NOT_RETURN") {
		t.Fatalf("database detail leaked: %d %s", w.Code, w.Body.String())
	}
}
