package main

import (
	"encore.app/l3-core/notification"
	"strings"
	"testing"
	"time"
)

func TestServiceRenewCannotExpandAnExistingGrant(t *testing.T) {
	p := serviceRenewPolicy{Version: 1, DeploymentID: "deployment-a", TenantID: "tenant-a"}
	valid := func() notification.ProvisionedSourceCredentials {
		return notification.ProvisionedSourceCredentials{DeploymentID: "deployment-a", AllowedTenantIDs: []string{"tenant-a"}, SourceCredential: strings.Repeat("s", 32), ProjectionCredential: strings.Repeat("p", 32)}
	}
	if err := validateServicePair(p, valid(), 8*time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"deployment", "tenant", "extra_tenant", "missing_source", "same_token", "long_ttl"} {
		pair := valid()
		ttl := 8 * time.Hour
		switch name {
		case "deployment":
			pair.DeploymentID = "deployment-b"
		case "tenant":
			pair.AllowedTenantIDs = []string{"tenant-b"}
		case "extra_tenant":
			pair.AllowedTenantIDs = append(pair.AllowedTenantIDs, "tenant-b")
		case "missing_source":
			pair.SourceCredential = ""
		case "same_token":
			pair.SourceCredential = pair.ProjectionCredential
		case "long_ttl":
			ttl = 9 * time.Hour
		}
		if err := validateServicePair(p, pair, ttl); err == nil {
			t.Fatalf("accepted unauthorized change: %s", name)
		}
	}
}
