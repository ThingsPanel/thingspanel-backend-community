package brokercontrol

import (
	"context"
	"testing"

	bc "project/internal/brokercontract"
)

type targetPluginRepository struct {
	*fakeRepository
	plugins []bc.PluginTarget
}

func (r targetPluginRepository) ResolvePluginTargets(_ context.Context, ids []string) ([]bc.PluginTarget, error) {
	var result []bc.PluginTarget
	for _, p := range r.plugins {
		for _, id := range ids {
			if p.DeviceID == id {
				result = append(result, p)
			}
		}
	}
	return result, nil
}

func TestTargetPlatformPublisherGrantsManagedPluginCommandAliases(t *testing.T) {
	for _, kind := range []string{"HTTP", "homeassistant"} {
		t.Run(kind, func(t *testing.T) {
			s := serviceFixture()
			s.TargetScopedPublisher, s.TargetTenantScope = true, "platform"
			s.AllowedDeviceIDs, s.AllowedTenantIDs = nil, nil
			s.Policy.Publish = []string{"devices/command/+/+", "gateway/command/+/+", "devices/telemetry/control/+"}
			record := DeviceRecord{ID: "target", TenantID: "tenant-a", DeviceNumber: "number", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}
			if kind == "HTTP" {
				record.ProtocolType = &kind
			} else {
				access := "managed-access"
				record.ServiceAccessID = &access
			}
			prefix := "plugin/" + kind + "/"
			repo := targetPluginRepository{fakeRepository: &fakeRepository{serviceTargets: map[string]DeviceRecord{"target": record}},
				plugins: []bc.PluginTarget{{TenantID: "tenant-a", DeviceID: "target", DeviceNumber: "number", Kind: bc.PrincipalDevice, TopicPrefix: prefix}}}
			o := testOwner(t, repo, []ServiceCredential{s})
			req := serviceRequest()
			req.WireClientID, _ = bc.TargetPublisherClientID(s.AllowedClientIDs[0], "target")
			grant, err := o.Authenticate(context.Background(), req)
			if err != nil || !grant.Allow {
				t.Fatalf("managed plugin publisher denied: allow=%v err=%v", grant.Allow, err)
			}
			if err := bc.ValidateGrant(grant); err != nil {
				t.Fatalf("driver contract rejects owner grant: %v grant=%+v", err, grant)
			}
			if !hasExact(grant.Policy.Publish, prefix+"devices/command/number/+") || len(grant.Policy.Subscribe) != 0 {
				t.Fatalf("missing exact plugin command permission: %+v", grant.Policy)
			}
		})
	}
}
