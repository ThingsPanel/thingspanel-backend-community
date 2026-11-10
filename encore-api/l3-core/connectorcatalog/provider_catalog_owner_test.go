package connectorcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/connectorcatalog/providercatalog"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestProviderCatalogOwnersPublishAndResolveFixedRelease(t *testing.T) {
	ctx := context.Background()
	providerID := uuid.NewString()
	version := "opaque-release-" + uuid.NewString()
	publisherID := uuid.NewString()
	tenantUserID := uuid.NewString()
	tenantID := uuid.NewString()
	service := &Service{}
	defer et.OverrideAuthInfo("", nil)

	manifest := providerCatalogOwnerTestManifest(t, providerID, version, `{"type":"object", "properties":{"endpoint":{"type":"string"}}}`)
	changedManifest := providerCatalogOwnerTestManifest(t, providerID, version, `{"type":"object", "properties":{"endpoint":{"type":"string","minLength":1}}}`)

	// Publication runs through the actual private owner method and connector_runtime PG.
	et.OverrideAuthInfo(auth.UID(publisherID), &authn.Data{UserID: publisherID, Roles: []string{"SYS_ADMIN"}})
	published, err := service.PublishNativeProviderRelease(ctx, manifest)
	if err != nil {
		t.Fatalf("SYS_ADMIN publish: %v", err)
	}
	if published.ProviderID != providerID || published.Version != version || len(published.SchemaDigest) != 64 || published.PublishedAt.IsZero() {
		t.Fatalf("published response is incomplete: %#v", published)
	}
	replayed, err := service.PublishNativeProviderRelease(ctx, manifest)
	if err != nil {
		t.Fatalf("same-content replay: %v", err)
	}
	if replayed.PublishedAt != published.PublishedAt || replayed.SchemaDigest != published.SchemaDigest || replayed.ArtifactDigest != published.ArtifactDigest {
		t.Fatalf("replay did not preserve original publication identity: first=%#v replay=%#v", published, replayed)
	}
	if _, err := service.PublishNativeProviderRelease(ctx, changedManifest); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("same provider/version changed-content publish error=%v code=%v, want AlreadyExists", err, errs.Code(err))
	}

	// Tenant administrators may read fixed public schemas, but may not publish.
	et.OverrideAuthInfo(auth.UID(tenantUserID), &authn.Data{UserID: tenantUserID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	if _, err := service.PublishNativeProviderRelease(ctx, manifest); errs.Code(err) != errs.PermissionDenied {
		t.Fatalf("TENANT_ADMIN publication error=%v code=%v, want PermissionDenied", err, errs.Code(err))
	}
	resolved, err := service.ResolveNativeProviderRelease(ctx, &ResolveNativeProviderReleaseRequest{
		ProviderID: providerID, Version: version, SchemaDigest: published.SchemaDigest,
	})
	if err != nil {
		t.Fatalf("TENANT_ADMIN fixed-version resolve: %v", err)
	}
	if resolved.ProviderID != providerID || resolved.Version != version || resolved.SchemaDigest != published.SchemaDigest ||
		!bytes.Equal(resolved.ConfigurationSchema, mustManifestSchema(t, manifest, true)) ||
		!bytes.Equal(resolved.DeviceParameterSchema, mustManifestSchema(t, manifest, false)) ||
		!equalStrings(resolved.Protocols, []string{"test-protocol"}) || !equalStrings(resolved.DeviceKinds, []string{"standalone"}) {
		t.Fatalf("fixed release resolution differs from published schema: %#v", resolved)
	}

	wrongDigest := strings.Repeat("f", 64)
	if wrongDigest == published.SchemaDigest {
		wrongDigest = strings.Repeat("e", 64)
	}
	if _, err := service.ResolveNativeProviderRelease(ctx, &ResolveNativeProviderReleaseRequest{
		ProviderID: providerID, Version: version, SchemaDigest: wrongDigest,
	}); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("wrong schema digest error=%v code=%v, want FailedPrecondition", err, errs.Code(err))
	}
	if _, err := service.ResolveNativeProviderRelease(ctx, &ResolveNativeProviderReleaseRequest{
		ProviderID: providerID, Version: "latest", SchemaDigest: published.SchemaDigest,
	}); errs.Code(err) != errs.NotFound {
		t.Fatalf("nonexistent explicit version must not resolve latest: error=%v code=%v", err, errs.Code(err))
	}

	// SYS_ADMIN and ordinary tenant users cannot use the tenant directory read path.
	et.OverrideAuthInfo(auth.UID(publisherID), &authn.Data{UserID: publisherID, Roles: []string{"SYS_ADMIN"}})
	if _, err := service.ResolveNativeProviderRelease(ctx, &ResolveNativeProviderReleaseRequest{
		ProviderID: providerID, Version: version, SchemaDigest: published.SchemaDigest,
	}); errs.Code(err) != errs.PermissionDenied {
		t.Fatalf("SYS_ADMIN read error=%v code=%v, want PermissionDenied", err, errs.Code(err))
	}
	et.OverrideAuthInfo(auth.UID(tenantUserID), &authn.Data{UserID: tenantUserID, TenantID: tenantID, Roles: []string{"TENANT_USER"}})
	if _, err := service.ResolveNativeProviderRelease(ctx, &ResolveNativeProviderReleaseRequest{
		ProviderID: providerID, Version: version, SchemaDigest: published.SchemaDigest,
	}); errs.Code(err) != errs.PermissionDenied {
		t.Fatalf("TENANT_USER read error=%v code=%v, want PermissionDenied", err, errs.Code(err))
	}

	var eventCount int
	var actorID string
	tx, err := providerCatalogDB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM provider_publication_events WHERE provider_id=$1 AND version=$2`, providerID, version).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT actor_id FROM provider_publication_events WHERE provider_id=$1 AND version=$2`, providerID, version).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || actorID != publisherID {
		t.Fatalf("publication audit events=%d actor=%q, want one event by SYS_ADMIN publisher", eventCount, actorID)
	}
}

func providerCatalogOwnerTestManifest(t *testing.T, providerID, version, configuration string) *PublishNativeProviderReleaseRequest {
	t.Helper()
	validated := mustProviderCatalogTestManifest(t, providerID, version, configuration)
	wire := providerReleaseManifestWire{
		FormatVersion:         validated.FormatVersion(),
		ProviderID:            validated.ProviderID(),
		Name:                  validated.Name(),
		Version:               validated.Version(),
		ArtifactDigest:        validated.ArtifactDigest(),
		Protocols:             validated.Protocols(),
		DeviceKinds:           validated.DeviceKinds(),
		ConfigurationSchema:   validated.OriginalConfigurationSchema(),
		DeviceParameterSchema: validated.OriginalDeviceParameterSchema(),
	}
	encoded, err := marshalProviderReleaseManifest(wire)
	if err != nil {
		t.Fatal(err)
	}
	return &PublishNativeProviderReleaseRequest{Manifest: encoded}
}

func mustManifestSchema(t *testing.T, req *PublishNativeProviderReleaseRequest, configuration bool) json.RawMessage {
	t.Helper()
	validated, err := validateProviderTestRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if configuration {
		return validated.OriginalConfigurationSchema()
	}
	return validated.OriginalDeviceParameterSchema()
}

func validateProviderTestRequest(req *PublishNativeProviderReleaseRequest) (*providercatalog.ValidatedManifest, error) {
	return providercatalog.ValidateManifest(req.Manifest)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
