package thingmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"encore.app/l1-access/authn"
	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

//encore:service
type Service struct{}

func application() *app.Service { return app.New(newPostgresRepository(), uuid.NewString) }

type CreateRequest struct {
	IdempotencyKey string                 `header:"Idempotency-Key"`
	Model          ModelDraft             `json:"model"`
	Resources      *app.ResourcesDocument `json:"resources,omitempty"`
}
type UpdateRequest struct {
	IdempotencyKey            string                 `header:"Idempotency-Key"`
	ModelID                   string                 `json:"modelID"`
	ExpectedDraftRevision     uint64                 `json:"expectedDraftRevision"`
	Model                     *ModelDraft            `json:"model,omitempty"`
	FieldOperations           json.RawMessage        `json:"fieldOperations,omitempty"`
	Resources                 *app.ResourcesDocument `json:"resources,omitempty"`
	ExpectedResourcesRevision *uint64                `json:"expectedResourcesRevision,omitempty"`
}
type PublishRequest struct {
	IdempotencyKey            string  `header:"Idempotency-Key"`
	ModelID                   string  `json:"modelID"`
	ExpectedDraftRevision     uint64  `json:"expectedDraftRevision"`
	Changelog                 string  `json:"changelog,omitempty"`
	ExpectedResourcesRevision *uint64 `json:"expectedResourcesRevision,omitempty"`
}

//encore:api auth method=POST path=/api/v2/thing-model-commands/create
func (s *Service) Create(ctx context.Context, req *CreateRequest) (*app.Result, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	m, e := req.Model.coreModel()
	if e != nil {
		return nil, apiError(e)
	}
	r, e := application().CreateWithResources(ctx, p, req.IdempotencyKey, m, req.Resources)
	if e != nil {
		return nil, apiError(e)
	}
	return &r, nil
}

//encore:api auth method=POST path=/api/v2/thing-model-commands/update
func (s *Service) Update(ctx context.Context, req *UpdateRequest) (*app.Result, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	if req.Model == nil || len(req.FieldOperations) != 0 || (req.Resources != nil && req.ExpectedResourcesRevision == nil) {
		return nil, apiError(fmt.Errorf("%w: this implementation requires a complete model replacement", app.ErrInvalid))
	}
	m, e := req.Model.coreModel()
	if e != nil {
		return nil, apiError(e)
	}
	r, e := application().UpdateWithResources(ctx, p, req.IdempotencyKey, core.ModelID(req.ModelID), req.ExpectedDraftRevision, m, req.Resources, req.ExpectedResourcesRevision)
	if e != nil {
		return nil, apiError(e)
	}
	return &r, nil
}

//encore:api auth method=POST path=/api/v2/thing-model-commands/publish
func (s *Service) Publish(ctx context.Context, req *PublishRequest) (*app.Result, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	r, e := application().PublishWithResources(ctx, p, req.IdempotencyKey, core.ModelID(req.ModelID), req.ExpectedDraftRevision, req.Changelog, req.ExpectedResourcesRevision)
	if e != nil {
		return nil, apiError(e)
	}
	return &r, nil
}

type ListRequest struct {
	Page      int    `query:"page"`
	PageSize  int    `query:"pageSize"`
	Query     string `query:"query"`
	Kind      string `query:"kind"`
	Lifecycle string `query:"lifecycle"`
}
type ModelList struct {
	Items    []ModelSummary `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Total    int            `json:"total"`
}

func pageValues(p, n int) (int, int) {
	if p < 1 {
		p = 1
	}
	if n < 1 {
		n = 20
	}
	if n > 200 {
		n = 200
	}
	return p, n
}

//encore:api auth method=GET path=/api/v2/thing-models
func (s *Service) List(ctx context.Context, req *ListRequest) (*ModelList, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	if req.Kind != "" || req.Lifecycle != "" {
		return nil, apiError(fmt.Errorf("%w: kind and lifecycle filtering are not implemented", app.ErrInvalid))
	}
	page, size := pageValues(req.Page, req.PageSize)
	records, total, e := application().List(ctx, p, page, size, req.Query)
	if e != nil {
		return nil, apiError(e)
	}
	out := &ModelList{Items: make([]ModelSummary, 0, len(records)), Page: page, PageSize: size, Total: total}
	for _, r := range records {
		out.Items = append(out.Items, ModelSummary{ModelID: string(r.Model.ID), Name: r.Model.Name, Lifecycle: r.Lifecycle, DraftRevision: r.Model.DraftRevision, PublishedRevision: publishedRevision(r.Model.PublishedRevision), UpdatedAt: r.UpdatedAt})
	}
	return out, nil
}

//encore:api auth method=GET path=/api/v2/thing-models/:modelID
func (s *Service) Get(ctx context.Context, modelID string) (*ModelDetail, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	r, e := application().Get(ctx, p, core.ModelID(modelID))
	if e != nil {
		return nil, apiError(e)
	}
	return &ModelDetail{ModelID: string(r.Model.ID), Draft: draftFromCore(r.Model), DraftRevision: r.Model.DraftRevision, Lifecycle: r.Lifecycle, PublishedRevision: publishedRevision(r.Model.PublishedRevision)}, nil
}

type VersionsRequest struct {
	Page     int `query:"page"`
	PageSize int `query:"pageSize"`
}
type VersionList struct {
	Items    []VersionSummary `json:"items"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
	Total    int              `json:"total"`
}

//encore:api auth method=GET path=/api/v2/thing-models/:modelID/versions
func (s *Service) Versions(ctx context.Context, modelID string, req *VersionsRequest) (*VersionList, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	items, e := application().Versions(ctx, p, core.ModelID(modelID))
	if e != nil {
		return nil, apiError(e)
	}
	page, size := pageValues(req.Page, req.PageSize)
	out := &VersionList{Items: []VersionSummary{}, Page: page, PageSize: size, Total: len(items)}
	start, e := paginationStart(page, size)
	if e != nil {
		return nil, apiError(e)
	}
	if start >= len(items) {
		return out, nil
	}
	end := len(items)
	if len(items)-start > size {
		end = start + size
	}
	for _, v := range items[start:end] {
		out.Items = append(out.Items, VersionSummary{PublishedRevision: v.Snapshot.Revision, SnapshotID: string(v.Snapshot.ID), Checksum: v.Snapshot.Checksum, CreatedAt: v.CreatedAt, Changelog: v.Changelog})
	}
	return out, nil
}

func paginationStart(page, size int) (int, error) {
	if page < 1 || size < 1 || page-1 > int(^uint(0)>>1)/size {
		return 0, fmt.Errorf("%w: pagination exceeds supported range", app.ErrInvalid)
	}
	return (page - 1) * size, nil
}

//encore:api auth method=GET path=/api/v2/thing-models/:modelID/versions/:publishedRevision
func (s *Service) Version(ctx context.Context, modelID string, publishedRevision uint64) (*SnapshotDetail, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	v, e := application().Version(ctx, p, core.ModelID(modelID), publishedRevision)
	if e != nil {
		return nil, apiError(e)
	}
	return &SnapshotDetail{ModelID: string(v.Snapshot.ModelID), PublishedRevision: v.Snapshot.Revision, SnapshotID: string(v.Snapshot.ID), Checksum: v.Snapshot.Checksum, CreatedAt: v.CreatedAt, Changelog: v.Changelog, Model: draftFromCore(core.Model{Name: v.Snapshot.Name, Description: v.Snapshot.Description, Category: v.Snapshot.Category, Fields: v.Snapshot.Fields}), Resources: VersionResources{ModelID: string(v.Snapshot.ModelID), Revision: v.Resources.Revision, Profile: v.Resources.Document.Profile, WebChartConfig: v.Resources.Document.WebChartConfig, AppChartConfig: v.Resources.Document.AppChartConfig}}, nil
}

//encore:api auth method=GET path=/api/v2/thing-models/:modelID/resources
func (s *Service) Resources(ctx context.Context, modelID string) (*ResourcesResponse, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	r, e := application().Resources(ctx, p, core.ModelID(modelID))
	if e != nil {
		return nil, apiError(e)
	}
	return &ResourcesResponse{ModelID: modelID, Revision: r.Revision, Profile: r.Document.Profile, WebChartConfig: r.Document.WebChartConfig, AppChartConfig: r.Document.AppChartConfig}, nil
}

//encore:api auth method=POST path=/api/v2/thing-model-commands/update-resources
func (s *Service) UpdateResources(ctx context.Context, req *UpdateResourcesRequest) (*app.Result, error) {
	p, e := currentPrincipal()
	if e != nil {
		return nil, e
	}
	if req == nil || req.Resources == nil || req.ExpectedRevision == nil {
		return nil, apiError(fmt.Errorf("%w: resources and expectedRevision are required", app.ErrInvalid))
	}
	r, e := application().UpdateResources(ctx, p, req.IdempotencyKey, core.ModelID(req.ModelID), *req.ExpectedRevision, *req.Resources)
	if e != nil {
		return nil, apiError(e)
	}
	return &r, nil
}

func currentPrincipal() (core.Principal, error) { return principalForData(authn.CurrentData()) }
func principalForData(d *authn.Data) (core.Principal, error) {
	if d == nil || strings.TrimSpace(d.UserID) == "" || strings.TrimSpace(d.TenantID) == "" {
		return core.Principal{}, &errs.Error{Code: errs.Unauthenticated, Message: "authentication required"}
	}
	p := core.Principal{SubjectID: canonicalIdentity("subject", d.UserID), TenantID: core.TenantID(canonicalIdentity("tenant", d.TenantID))}
	for _, r := range d.Roles {
		switch strings.ToUpper(r) {
		case "SYS_ADMIN", "PLATFORM_ADMIN":
			return core.Principal{}, &errs.Error{Code: errs.PermissionDenied, Message: "platform admin requires an audited tenant delegation"}
		case "TENANT_ADMIN":
			p.Capabilities = []string{string(core.CapabilityRead), string(core.CapabilityWrite), string(core.CapabilityPublish), string(core.CapabilityDelete)}
			return p, nil
		}
	}
	return p, nil
}

// Full source identity is retained in a stable namespace hash; never strip
// characters or pad/truncate short IDs, which can collapse distinct tenants.
func canonicalIdentity(kind, raw string) string {
	if id, e := uuid.Parse(raw); e == nil {
		return id.String()
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("thingspanel/community/"+kind+"/"+raw)).String()
}
func apiError(e error) error {
	switch {
	case errors.Is(e, core.ErrUnauthorized):
		return &errs.Error{Code: errs.PermissionDenied, Message: "not authorized for this model operation"}
	case errors.Is(e, app.ErrNotFound):
		return &errs.Error{Code: errs.NotFound, Message: "model or version not found"}
	case errors.Is(e, app.ErrConflict), errors.Is(e, app.ErrArchived):
		return &errs.Error{Code: errs.Aborted, Message: "model revision or command key conflicts; reload and review the current model"}
	case errors.Is(e, app.ErrInvalid):
		return &errs.Error{Code: errs.InvalidArgument, Message: e.Error()}
	default:
		return &errs.Error{Code: errs.Internal, Message: "model persistence operation failed"}
	}
}
