package runtime

import (
	"context"
	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/store"
	"errors"
	"testing"
)

type membershipDirectory struct {
	device      ports.Device
	calls, wide int
	err         error
}

func (d *membershipDirectory) ByIDs(_ context.Context, tenant string, ids []string) ([]ports.Device, error) {
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	if len(ids) != 1 {
		return nil, errors.New("source query must be a single exact ID")
	}
	if d.device.ID == "" {
		return nil, nil
	}
	return []ports.Device{d.device}, nil
}
func (d *membershipDirectory) ByDeviceConfig(context.Context, string, []string) ([]ports.Device, error) {
	d.wide++
	return nil, errors.New("full template scan forbidden")
}
func (d *membershipDirectory) ByLabels(context.Context, string, []string, bool) ([]ports.Device, error) {
	d.wide++
	return nil, errors.New("full labels scan forbidden")
}
func (d *membershipDirectory) ByType(context.Context, string, string) ([]ports.Device, error) {
	d.wide++
	return nil, errors.New("full type scan forbidden")
}
func TestSourceMembershipPreservesUnionAndCapabilityWithoutExpansion(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec domain.TargetSpec
		want bool
	}{
		{"id", domain.TargetSpec{DeviceIDs: []string{"d"}}, true},
		{"missing-id", domain.TargetSpec{DeviceIDs: []string{"other"}}, false},
		{"template", domain.TargetSpec{DeviceConfigIDs: []string{"cfg"}}, true},
		{"different-template", domain.TargetSpec{DeviceConfigIDs: []string{"other"}}, false},
		{"union-id-or-template", domain.TargetSpec{DeviceIDs: []string{"other"}, DeviceConfigIDs: []string{"cfg"}}, true},
		{"union-template-or-type", domain.TargetSpec{DeviceConfigIDs: []string{"other"}, DeviceTypes: []string{"sensor"}}, true},
		{"label-or", domain.TargetSpec{Labels: []string{"missing", "floor-1"}, LabelMatch: "or"}, true},
		{"label-and", domain.TargetSpec{Labels: []string{"floor-1", "hot"}, LabelMatch: "and"}, true},
		{"label-and-missing", domain.TargetSpec{Labels: []string{"floor-1", "missing"}, LabelMatch: "and"}, false},
		{"label-token-not-substring", domain.TargetSpec{Labels: []string{"floor-10"}}, false},
		{"union-label-or-id", domain.TargetSpec{DeviceIDs: []string{"d"}, Labels: []string{"missing"}}, true},
		{"capability-intersection", domain.TargetSpec{DeviceIDs: []string{"d"}, RequiredCapability: "missing"}, false},
		{"capability-present", domain.TargetSpec{DeviceIDs: []string{"d"}, RequiredCapability: "control"}, true},
		{"area-unsupported", domain.TargetSpec{AreaIDs: []string{"area"}}, false},
		{"area-does-not-exclude-id-union", domain.TargetSpec{AreaIDs: []string{"area"}, DeviceIDs: []string{"d"}}, true},
		{"budget-not-membership", domain.TargetSpec{DeviceIDs: []string{"other", "d"}, MaxTargets: 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := &membershipDirectory{device: ports.Device{ID: "d", TenantID: "t", DeviceConfigID: "cfg", DeviceType: "sensor", Label: "floor-1,hot", Capabilities: []string{"control"}}}
			r := New(nil, nil, nil, dir, nil)
			rule := &store.Rule{TenantID: "t", Definition: domain.Definition{Targets: tc.spec}}
			got, err := r.unboundSourceAllowed(context.Background(), rule, "d", "telemetry", "temperature")
			if err != nil || got != tc.want || dir.calls != 1 || dir.wide != 0 {
				t.Fatalf("membership=%v err=%v exact=%d full=%d", got, err, dir.calls, dir.wide)
			}
		})
	}
}
func TestSourceMembershipRequestCacheExpiresWithContext(t *testing.T) {
	dir := &membershipDirectory{device: ports.Device{ID: "d", TenantID: "t", DeviceConfigID: "cfg"}}
	r := New(nil, nil, nil, dir, nil)
	ctx := withSourceDeviceCache(context.Background())
	for i := 0; i < 100; i++ {
		rule := &store.Rule{TenantID: "t", Definition: domain.Definition{Targets: domain.TargetSpec{DeviceConfigIDs: []string{"cfg"}}}}
		if ok, err := r.unboundSourceAllowed(ctx, rule, "d", "telemetry", "temperature"); err != nil || !ok {
			t.Fatal("membership cache changed decision")
		}
	}
	if dir.calls != 1 || dir.wide != 0 {
		t.Fatal("candidate rules caused repeated/full scans")
	}
	dir.device.DeviceConfigID = "new"
	rule := &store.Rule{TenantID: "t", Definition: domain.Definition{Targets: domain.TargetSpec{DeviceConfigIDs: []string{"cfg"}}}}
	if ok, err := r.unboundSourceAllowed(withSourceDeviceCache(context.Background()), rule, "d", "telemetry", "temperature"); err != nil || ok || dir.calls != 2 {
		t.Fatal("cache survived into next event")
	}
}
func TestSourceMembershipDirectoryFailuresStayClosed(t *testing.T) {
	for _, dir := range []*membershipDirectory{{err: errors.New("owner404")}, {device: ports.Device{ID: "d", TenantID: "foreign"}}, {device: ports.Device{ID: "other", TenantID: "t"}}, {}} {
		r := New(nil, nil, nil, dir, nil)
		rule := &store.Rule{TenantID: "t", Definition: domain.Definition{Targets: domain.TargetSpec{DeviceConfigIDs: []string{"cfg"}}}}
		ok, err := r.unboundSourceAllowed(context.Background(), rule, "d", "telemetry", "temperature")
		if ok {
			t.Fatal("unavailable/mismatched device admitted")
		}
		if dir.device.ID != "" || dir.err != nil {
			if err == nil {
				t.Fatal("lookup failure swallowed")
			}
		}
	}
}
