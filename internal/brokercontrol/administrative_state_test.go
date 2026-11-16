package brokercontrol

import (
	"context"
	"testing"
)

func TestAdministrativeDisableRejectsAuthenticationAndEnableRestores(t *testing.T) {
	repo := &fakeRepository{devices: []DeviceRecord{activeDevice()}}
	owner := testOwner(t, repo, nil)
	ctx := context.Background()
	for _, step := range []struct {
		state string
		allow bool
	}{{"enabled", true}, {"disabled", false}, {"enabled", true}} {
		repo.devices[0].IsEnabled = step.state
		grant, err := owner.Authenticate(ctx, authRequest())
		if err != nil || grant.Allow != step.allow {
			t.Fatalf("state %s allow=%v err=%v", step.state, grant.Allow, err)
		}
	}
}
