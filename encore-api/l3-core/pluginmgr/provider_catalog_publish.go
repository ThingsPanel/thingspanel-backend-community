package pluginmgr

import (
	"context"
	"errors"
	"time"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.app/l3-core/pluginmgr/providercatalog"
	"encore.dev/beta/errs"
)

// Publication is a private deployment capability. It is intentionally absent
// from the user-facing metadata API: tenant administrators cannot change the
// global provider release catalog or register their own schema authority.
type PublishNativeProviderReleaseRequest struct {
	Manifest []byte `json:"manifest"`
}

type PublishNativeProviderReleaseResponse struct {
	ProviderID     string    `json:"providerId"`
	Version        string    `json:"version"`
	SchemaDigest   string    `json:"schemaDigest"`
	ArtifactDigest string    `json:"artifactDigest"`
	PublishedAt    time.Time `json:"publishedAt"`
}

func authorizeProviderPublication(data *authn.Data) error {
	if data == nil || !validProviderPublisherID(data.UserID) {
		return &errs.Error{Code: errs.Unauthenticated, Message: "missing verified publisher"}
	}
	// SYS_ADMIN is the verified Community platform authority. A tenant scope
	// or an unverified lowercase legacy role cannot grant global publication.
	verifiedPlatformAdmin := false
	for _, role := range data.Roles {
		if role == "SYS_ADMIN" {
			verifiedPlatformAdmin = true
		}
	}
	if !verifiedPlatformAdmin || !authz.Can(data, "provider", "publish") {
		return &errs.Error{Code: errs.PermissionDenied, Message: "platform provider publication authority is required"}
	}
	return nil
}

// PublishNativeProviderRelease freezes the exact artifact and two schema
// byte sequences. Its natural key (provider ID, version) is the permanent
// idempotency boundary: unchanged retries return the original publication,
// while different content for that key is a conflict.
//
//encore:api private
func (s *Service) PublishNativeProviderRelease(ctx context.Context, req *PublishNativeProviderReleaseRequest) (*PublishNativeProviderReleaseResponse, error) {
	if err := authorizeProviderPublication(authn.CurrentData()); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "provider manifest is required"}
	}
	manifest, err := providercatalog.ValidateManifest(req.Manifest)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "provider manifest is invalid"}
	}
	release, err := publishProviderRelease(ctx, manifest, authn.CurrentData().UserID, newID())
	if errors.Is(err, ErrProviderReleaseConflict) {
		return nil, &errs.Error{Code: errs.AlreadyExists, Message: "provider version already contains a different release"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "provider publication is unavailable"}
	}
	return &PublishNativeProviderReleaseResponse{ProviderID: release.ProviderID, Version: release.Version,
		SchemaDigest: release.SchemaDigest, ArtifactDigest: release.ArtifactDigest, PublishedAt: release.PublishedAt.UTC()}, nil
}
