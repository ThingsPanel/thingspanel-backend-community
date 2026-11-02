package thingmodel

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"encore.app/marketinstall/app"
	modelapp "encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
	"encore.dev/beta/errs"
)

type InstallProviderRequest struct {
	ServiceToken string `header:"X-TP-Service-Token"`
	app.StepRequest
	Mode string `json:"mode"`
}

// InstallProvider is purpose-limited machine transport. Its service credential
// is insufficient authorization: every call verifies the immutable original
// step and the live revocable user delegation at the Community identity owner.
// It never changes global auth data or accepts a generic caller principal.
//
//encore:api public method=POST path=/internal/market-install/native
func (s *Service) InstallProvider(ctx context.Context, req *InstallProviderRequest) (*app.Observation, error) {
	if req == nil {
		return nil, installProviderError()
	}
	configured := os.Getenv("TP_MARKETINSTALL_NATIVE_TOKEN")
	if len(configured) < 32 || len(req.ServiceToken) != len(configured) || subtle.ConstantTimeCompare([]byte(configured), []byte(req.ServiceToken)) != 1 {
		return nil, installProviderError()
	}
	if req.Mode != "query" && req.Mode != "execute" {
		return nil, installProviderError()
	}
	if req.Step.Owner != "thingmodel" || req.StepID != req.Step.ID || req.IdempotencyKey != app.StableStepKey(req.OperationID, req.Step) {
		return nil, installProviderError()
	}
	hash, err := app.StepHash(req.Step)
	if err != nil || hash != req.PayloadHash {
		return nil, installProviderError()
	}
	if err := verifyInstallationDelegation(ctx, req.StepRequest); err != nil {
		return nil, err
	}
	capability := core.CapabilityRead
	switch req.Step.Action {
	case "create":
		capability = core.CapabilityWrite
	case "publish":
		capability = core.CapabilityPublish
	case "readback":
	default:
		return nil, installProviderError()
	}
	principal := core.Principal{TenantID: core.TenantID(canonicalIdentity("tenant", req.TenantID)), SubjectID: canonicalIdentity("subject", req.SubjectID), Capabilities: []string{string(core.CapabilityRead), string(capability)}}
	if req.Step.Action == "readback" {
		return installReadback(ctx, principal, req.StepRequest)
	}
	if req.Mode == "query" {
		observed, err := modelapp.ReadReceipt(ctx, postgresRepository{}, principal, req.Step.Action, req.IdempotencyKey, "")
		if err != nil {
			return nil, apiError(err)
		}
		observation := &app.Observation{State: observed.State}
		if observed.State == modelapp.ReceiptConfirmed && observed.Result != nil {
			data, _ := json.Marshal(struct {
				Result           modelapp.Result `json:"result"`
				OwnerPayloadHash string          `json:"ownerPayloadHash"`
			}{*observed.Result, observed.PayloadHash})
			observation.Receipt = &app.Receipt{Owner: "thingmodel", ResourceID: observed.Result.ModelID, PayloadHash: req.PayloadHash, Data: data}
		}
		return observation, nil
	}
	var result modelapp.Result
	switch req.Step.Action {
	case "create":
		var payload struct {
			Model     ModelDraft                  `json:"model"`
			Resources *modelapp.ResourcesDocument `json:"resources"`
		}
		if err := json.Unmarshal(req.Step.Payload, &payload); err != nil || payload.Resources == nil {
			return nil, apiError(modelapp.ErrInvalid)
		}
		native, err := payload.Model.coreModel()
		if err != nil {
			return nil, apiError(err)
		}
		result, err = application().CreateWithResources(ctx, principal, req.IdempotencyKey, native, payload.Resources)
		if err != nil {
			return nil, apiError(err)
		}
	case "publish":
		created, err := installationPriorCommand(ctx, principal, req.StepRequest, "create")
		if err != nil {
			return nil, apiError(err)
		}
		if created.ResourcesRevision == nil {
			return nil, apiError(modelapp.ErrInvalid)
		}
		result, err = application().PublishWithResources(ctx, principal, req.IdempotencyKey, core.ModelID(created.ModelID), created.DraftRevision, "Installed from immutable market bundle", created.ResourcesRevision)
		if err != nil {
			return nil, apiError(err)
		}
	}
	data, _ := json.Marshal(struct {
		Result modelapp.Result `json:"result"`
	}{result})
	return &app.Observation{State: app.Confirmed, Receipt: &app.Receipt{Owner: "thingmodel", ResourceID: result.ModelID, PayloadHash: req.PayloadHash, Data: data}}, nil
}
func installationPriorCommand(ctx context.Context, p core.Principal, req app.StepRequest, action string) (modelapp.Result, error) {
	step := app.Step{ID: req.Step.ResourceKey + ":" + action, Owner: "thingmodel", Action: action}
	key := app.StableStepKey(req.OperationID, step)
	// The reference checker authorizes this immutable installation plan, but each
	// dependency is resolved again from the owner's receipt, never caller data.
	if action == "create" {
		p.Capabilities = append(p.Capabilities, string(core.CapabilityWrite))
	}
	if action == "publish" {
		p.Capabilities = append(p.Capabilities, string(core.CapabilityPublish))
	}
	observed, err := modelapp.ReadReceipt(ctx, postgresRepository{}, p, action, key, "")
	if err != nil {
		return modelapp.Result{}, err
	}
	if observed.State != modelapp.ReceiptConfirmed || observed.Result == nil {
		return modelapp.Result{}, fmt.Errorf("original dependency receipt is not confirmed")
	}
	return *observed.Result, nil
}
func installReadback(ctx context.Context, p core.Principal, req app.StepRequest) (*app.Observation, error) {
	var payload struct {
		Reuse *struct {
			ModelID           string `json:"modelID"`
			PublishedRevision uint64 `json:"publishedRevision"`
			SnapshotID        string `json:"snapshotID"`
		} `json:"reuse"`
	}
	if err := json.Unmarshal(req.Step.Payload, &payload); err != nil {
		return nil, apiError(modelapp.ErrInvalid)
	}
	var modelID string
	var revision uint64
	var expectedSnapshot string
	if payload.Reuse != nil {
		modelID = payload.Reuse.ModelID
		revision = payload.Reuse.PublishedRevision
		expectedSnapshot = payload.Reuse.SnapshotID
	} else {
		published, err := installationPriorCommand(ctx, p, req, "publish")
		if err != nil {
			return nil, apiError(err)
		}
		if published.PublishedRevision == nil {
			return nil, apiError(modelapp.ErrInvalid)
		}
		modelID = published.ModelID
		revision = *published.PublishedRevision
	}
	version, err := application().Version(ctx, p, core.ModelID(modelID), revision)
	if err != nil {
		return nil, apiError(err)
	}
	if expectedSnapshot != "" && expectedSnapshot != string(version.Snapshot.ID) {
		return nil, apiError(modelapp.ErrConflict)
	}
	snapshot := SnapshotDetail{ModelID: string(version.Snapshot.ModelID), PublishedRevision: version.Snapshot.Revision, SnapshotID: string(version.Snapshot.ID), Checksum: version.Snapshot.Checksum, CreatedAt: version.CreatedAt, Changelog: version.Changelog, Model: draftFromCore(core.Model{Name: version.Snapshot.Name, Description: version.Snapshot.Description, Category: version.Snapshot.Category, Fields: version.Snapshot.Fields}), Resources: VersionResources{ModelID: string(version.Snapshot.ModelID), Revision: version.Resources.Revision, Profile: version.Resources.Document.Profile, WebChartConfig: version.Resources.Document.WebChartConfig, AppChartConfig: version.Resources.Document.AppChartConfig}}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, apiError(err)
	}
	return &app.Observation{State: app.Confirmed, Receipt: &app.Receipt{Owner: "thingmodel", ResourceID: snapshot.ModelID, PayloadHash: req.PayloadHash, Data: data}}, nil
}
func verifyInstallationDelegation(ctx context.Context, req app.StepRequest) error {
	base := strings.TrimRight(os.Getenv("TP_MARKETINSTALL_PROVIDER_BASE_URL"), "/")
	token := os.Getenv("TP_MARKETINSTALL_PROVIDER_TOKEN")
	if base == "" || len(token) < 32 {
		return installProviderError()
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return installProviderError()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/internal/market-install/authorize", bytes.NewReader(raw))
	if err != nil {
		return installProviderError()
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-TP-Service-Token", token)
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return &errs.Error{Code: errs.Unavailable, Message: "installation delegation unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return installProviderError()
	}
	var proof struct {
		Authorized  bool   `json:"authorized"`
		TenantID    string `json:"tenantId"`
		SubjectID   string `json:"subjectId"`
		PayloadHash string `json:"payloadHash"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&proof); err != nil || !proof.Authorized || proof.TenantID != req.TenantID || proof.SubjectID != req.SubjectID || proof.PayloadHash != req.PayloadHash {
		return installProviderError()
	}
	return nil
}
func installProviderError() error {
	return &errs.Error{Code: errs.PermissionDenied, Message: "installation delegation does not authorize this exact step"}
}
