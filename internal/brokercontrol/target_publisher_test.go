package brokercontrol

import (
	"context"
	"errors"
	bc "project/internal/brokercontract"
	"testing"
	"time"
)

type unavailableTargetRepository struct{ *fakeRepository }

func (unavailableTargetRepository) ResolveServiceTargets(context.Context, []string) ([]DeviceRecord, error) {
	return nil, errors.New("fixture owner database unavailable")
}

func TestTargetScopedPublisherUsesCurrentTenantAndDeviceState(t *testing.T) {
	s := serviceFixture()
	s.TargetScopedPublisher = true
	s.AllowedDeviceIDs = nil
	s.AllowedTenantIDs = []string{"tenant-a"}
	repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{}}
	owner := testOwner(t, repo, []ServiceCredential{s})
	req := serviceRequest()
	base, err := owner.Authenticate(context.Background(), req)
	if err != nil || !base.Allow || len(base.Policy.Publish) != 0 {
		t.Fatalf("base connection gained publish scope: %v %v", base.Allow, err)
	}
	// The device is created after the owner starts; no credential-file change.
	row := DeviceRecord{ID: "new-target", TenantID: "tenant-a", DeviceNumber: "new-number", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}
	repo.serviceTargets[row.ID] = row
	req.WireClientID, err = bc.TargetPublisherClientID(s.AllowedClientIDs[0], row.ID)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := owner.Authenticate(context.Background(), req)
	if err != nil || !grant.Allow || !hasExact(grant.Policy.Publish, "devices/event/response/new-number/+") || len(grant.Policy.Subscribe) != 0 {
		t.Fatalf("new target not narrowly granted: %#v %v", grant, err)
	}
	for _, mutate := range []struct {
		name string
		fn   func(*DeviceRecord)
	}{
		{"foreign tenant", func(r *DeviceRecord) { r.TenantID = "tenant-b" }},
		{"disabled", func(r *DeviceRecord) { r.IsEnabled = "disabled" }},
		{"inactive", func(r *DeviceRecord) { r.ActivateFlag = "inactive" }},
		{"ambiguous", func(r *DeviceRecord) { r.DeviceNumberCount = 2 }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			bad := row
			mutate.fn(&bad)
			repo.serviceTargets[row.ID] = bad
			g, e := owner.Authenticate(context.Background(), req)
			if e != nil || g.Allow {
				t.Fatalf("invalid current target allowed: %v %v", g.Allow, e)
			}
		})
	}
	delete(repo.serviceTargets, row.ID)
	g, e := owner.Authenticate(context.Background(), req)
	if e != nil || g.Allow {
		t.Fatal("deleted target allowed")
	}
	repo.serviceTargets[row.ID] = row
	req.Password = "incorrect"
	g, e = owner.Authenticate(context.Background(), req)
	if e != nil || g.Allow {
		t.Fatal("client id selection bypassed service credential")
	}
}

func TestTargetPlatformPublisherDiscoversNewTenantsWithoutBroadMQTTGrants(t *testing.T) {
	s := serviceFixture()
	s.TargetScopedPublisher = true
	s.TargetTenantScope = "platform"
	s.AllowedDeviceIDs, s.AllowedTenantIDs = nil, nil
	repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{}}
	owner := testOwner(t, repo, []ServiceCredential{s})
	for _, tenant := range []string{"new-tenant-a", "new-tenant-b"} {
		row := DeviceRecord{ID: tenant + "-target", TenantID: tenant, DeviceNumber: tenant + "-number", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}
		repo.serviceTargets[row.ID] = row
		req := serviceRequest()
		req.WireClientID, _ = bc.TargetPublisherClientID(s.AllowedClientIDs[0], row.ID)
		g, err := owner.Authenticate(context.Background(), req)
		if err != nil || !g.Allow || len(g.Policy.Subscribe) != 0 || !hasExact(g.Policy.Publish, "devices/event/response/"+row.DeviceNumber+"/+") {
			t.Fatalf("new tenant target grant: %#v %v", g, err)
		}
		for _, other := range []string{"unknown", "new-tenant-a-number", "new-tenant-b-number"} {
			if other != row.DeviceNumber && hasExact(g.Policy.Publish, "devices/event/response/"+other+"/+") {
				t.Fatal("platform scope produced another target's MQTT permission")
			}
		}
		// A selector does not authorize an unrecognized application identity.
		for _, bad := range []struct{ client, password string }{
			{req.WireClientID, "wrong"}, {"other" + req.WireClientID, req.Password},
		} {
			invalid := req
			invalid.WireClientID, invalid.Password = bad.client, bad.password
			denied, e := owner.Authenticate(context.Background(), invalid)
			if e != nil || denied.Allow {
				t.Fatal("invalid platform identity allowed")
			}
		}
	}
	base, err := owner.Authenticate(context.Background(), serviceRequest())
	if err != nil || !base.Allow || len(base.Policy.Publish) != 0 {
		t.Fatal("base platform connection can publish")
	}
}

func TestTargetPlatformPublisherConfigFailsClosed(t *testing.T) {
	valid := serviceFixture()
	valid.TargetScopedPublisher, valid.TargetTenantScope = true, "platform"
	valid.AllowedDeviceIDs, valid.AllowedTenantIDs = nil, nil
	for _, tc := range []struct {
		name   string
		mutate func(*ServiceCredential)
	}{
		{"plugin", func(s *ServiceCredential) { s.PluginSocket = true }},
		{"no opt in", func(s *ServiceCredential) { s.TargetScopedPublisher = false }},
		{"unknown scope", func(s *ServiceCredential) { s.TargetTenantScope = "all" }},
		{"missing scope", func(s *ServiceCredential) { s.TargetTenantScope = "" }},
		{"manual tenants", func(s *ServiceCredential) { s.AllowedTenantIDs = []string{"tenant-a"} }},
		{"manual devices", func(s *ServiceCredential) { s.AllowedDeviceIDs = []string{"target"} }},
		{"any client", func(s *ServiceCredential) { s.AllowAnyClientID = true }},
		{"no client", func(s *ServiceCredential) { s.AllowedClientIDs = nil }},
		{"no password", func(s *ServiceCredential) { s.Password = "" }},
		{"wildcard topics", func(s *ServiceCredential) { s.Policy.Publish = []string{"#"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.mutate(&s)
			_, err := NewOwner(&fakeRepository{}, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{s}})
			if err == nil {
				t.Fatal("invalid system publisher accepted")
			}
		})
	}
}

func TestTargetPlatformPublisherRefreshUsesCurrentIdentityAndFailsClosed(t *testing.T) {
	s := serviceFixture()
	s.TargetScopedPublisher, s.TargetTenantScope = true, "platform"
	s.AllowedDeviceIDs, s.AllowedTenantIDs = nil, nil
	row := DeviceRecord{ID: "target", TenantID: "tenant-a", DeviceNumber: "number-a", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}
	repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{row.ID: row}}
	o := testOwner(t, repo, []ServiceCredential{s})
	req := serviceRequest()
	req.WireClientID, _ = bc.TargetPublisherClientID(s.AllowedClientIDs[0], row.ID)
	if g, e := o.Authenticate(context.Background(), req); e != nil || !g.Allow {
		t.Fatalf("initial grant %v %v", g.Allow, e)
	}
	// A valid platform publisher may serve the new tenant, but renewal must
	// compile only its current device number. No old tenant list is inherited.
	row.TenantID, row.DeviceNumber = "tenant-b", "number-b"
	repo.serviceTargets[row.ID] = row
	g, e := o.Authenticate(context.Background(), req)
	if e != nil || !g.Allow || hasExact(g.Policy.Publish, "devices/event/response/number-a/+") || !hasExact(g.Policy.Publish, "devices/event/response/number-b/+") {
		t.Fatal("renewal reused stale device identity")
	}
	row.IsEnabled = "disabled"
	repo.serviceTargets[row.ID] = row
	if g, e = o.Authenticate(context.Background(), req); e != nil || g.Allow {
		t.Fatal("disabled platform target renewed")
	}
	delete(repo.serviceTargets, row.ID)
	if g, e = o.Authenticate(context.Background(), req); e != nil || g.Allow {
		t.Fatal("deleted platform target renewed")
	}
	o = testOwner(t, unavailableTargetRepository{repo}, []ServiceCredential{s})
	if g, e = o.Authenticate(context.Background(), req); g.Allow || !errors.Is(e, ErrUnavailable) {
		t.Fatal("target lookup failure did not fail closed")
	}
}

func TestTargetPublisherRevocationRequiresFreshGrantWithinTTL(t *testing.T) {
	s := serviceFixture()
	s.TargetScopedPublisher = true
	s.AllowedDeviceIDs, s.AllowedTenantIDs = nil, []string{"tenant-a"}
	row := DeviceRecord{ID: "target", TenantID: "tenant-a", DeviceNumber: "number", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}
	repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{row.ID: row}}
	o, err := NewOwner(repo, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{s}})
	if err != nil {
		t.Fatal(err)
	}
	req := serviceRequest()
	req.WireClientID, _ = bc.TargetPublisherClientID(s.AllowedClientIDs[0], row.ID)
	g, err := o.Authenticate(context.Background(), req)
	if err != nil || !g.Allow {
		t.Fatalf("initial grant %v %v", g.Allow, err)
	}
	if remaining := time.Until(g.ExpiresAt); remaining <= 0 || remaining > time.Minute {
		t.Fatalf("revocation lease exceeds one minute: %v", remaining)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*DeviceRecord)
	}{
		{"moved out of tenant scope", func(r *DeviceRecord) { r.TenantID = "tenant-b" }},
		{"disabled", func(r *DeviceRecord) { r.IsEnabled = "disabled" }},
		{"empty tenant", func(r *DeviceRecord) { r.TenantID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := row
			tc.mutate(&bad)
			repo.serviceTargets[row.ID] = bad
			denied, e := o.Authenticate(context.Background(), req)
			if e != nil || denied.Allow {
				t.Fatal("stale authorization renewed")
			}
		})
	}
}

func TestTargetScopedPublisherRequiresOptInAndExplicitTenantScope(t *testing.T) {
	s := serviceFixture()
	req := serviceRequest()
	req.WireClientID, _ = bc.TargetPublisherClientID(s.AllowedClientIDs[0], "target-1")
	g, e := testOwner(t, &fakeRepository{}, []ServiceCredential{s}).Authenticate(context.Background(), req)
	if e != nil || g.Allow {
		t.Fatal("ordinary service accepted target client")
	}
	s.TargetScopedPublisher = true
	if _, e := NewOwner(&fakeRepository{}, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{s}}); e == nil {
		t.Fatal("target service without tenants accepted")
	}
	s.AllowedTenantIDs = []string{"tenant-a"}
	s.PluginSocket = true
	if _, e := NewOwner(&fakeRepository{}, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{s}}); e == nil {
		t.Fatal("plugin acquired platform target capability")
	}
}
