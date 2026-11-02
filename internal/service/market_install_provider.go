package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/utils"
)

type MarketInstallProvider struct{ client *http.Client }

func NewMarketInstallProvider() *MarketInstallProvider {
	return &MarketInstallProvider{client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *MarketInstallProvider) Step(ctx context.Context, req model.MarketInstallStepRequest, execute bool) (model.MarketInstallObservation, error) {
	plan, err := AuthorizeMarketInstallStep(ctx, req)
	if err != nil {
		return model.MarketInstallObservation{}, err
	}
	switch req.Step.Owner {
	case "thingmodel":
		return p.nativeStep(ctx, req, execute)
	case "community-config":
		return p.configStep(ctx, req, plan, execute)
	case "thingsvis":
		return p.dashboardStep(ctx, req, execute)
	default:
		return model.MarketInstallObservation{}, repo.ErrMarketInstallUnauthorized
	}
}
func (p *MarketInstallProvider) nativeStep(ctx context.Context, req model.MarketInstallStepRequest, execute bool) (model.MarketInstallObservation, error) {
	mode := "query"
	if execute {
		mode = "execute"
	}
	payload := struct {
		model.MarketInstallStepRequest
		Mode string `json:"mode"`
	}{req, mode}
	var observation model.MarketInstallObservation
	status, err := p.call(ctx, os.Getenv("TP_MARKETINSTALL_NATIVE_BASE_URL"), "/internal/market-install/native", os.Getenv("TP_MARKETINSTALL_NATIVE_TOKEN"), "X-TP-Service-Token", nil, payload, &observation)
	if err != nil {
		return observation, err
	}
	if status != 200 {
		if status == 400 || status == 403 || status == 409 {
			return model.MarketInstallObservation{State: "rejected", ErrorCode: "NATIVE_OWNER_REJECTED"}, nil
		}
		return model.MarketInstallObservation{State: "unknown", ErrorCode: "NATIVE_OWNER_UNAVAILABLE"}, nil
	}
	if observation.State == "confirmed" && req.Step.Action == "readback" {
		if observation.Receipt == nil {
			return model.MarketInstallObservation{}, fmt.Errorf("native readback receipt missing")
		}
		var payload marketInstallNativePayload
		var snapshot nativeMarketSnapshotResponse
		if err := json.Unmarshal(req.Step.Payload, &payload); err != nil {
			return observation, err
		}
		if err := json.Unmarshal(observation.Receipt.Data, &snapshot); err != nil {
			return observation, err
		}
		if payload.Reuse != nil {
			err = verifyMarketSourceSnapshotContent(&payload.Dependency, &snapshot)
		} else {
			err = verifyImportedNativeSnapshotContent(payload.Model, payload.Resources, &snapshot)
		}
		if err != nil {
			return model.MarketInstallObservation{State: "unknown", ErrorCode: "NATIVE_READBACK_MISMATCH"}, nil
		}
	}
	return observation, nil
}
func (p *MarketInstallProvider) call(ctx context.Context, base, path, token, header string, headers map[string]string, input, output interface{}) (int, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(token) < 32 {
		return 0, fmt.Errorf("installation provider endpoint is not configured")
	}
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(raw)
	}
	method := http.MethodPost
	if input == nil {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set(header, token)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
	if err != nil {
		return 0, err
	}
	if len(raw) > 8*1024*1024 {
		return 0, fmt.Errorf("installation owner response exceeds limit")
	}
	if len(raw) > 0 && output != nil {
		if err := json.Unmarshal(raw, output); err != nil {
			return 0, fmt.Errorf("installation owner returned invalid response")
		}
	}
	return response.StatusCode, nil
}
func (p *MarketInstallProvider) configStep(ctx context.Context, req model.MarketInstallStepRequest, plan model.MarketInstallPlan, execute bool) (model.MarketInstallObservation, error) {
	if req.Step.Action != "bind" {
		return model.MarketInstallObservation{}, repo.ErrMarketInstallUnauthorized
	}
	existing, err := repo.ReadMarketInstallConfigReceipt(ctx, req)
	if err == nil {
		return configObservation(existing), nil
	}
	if errors.Is(err, repo.ErrMarketInstallConfigChanged) {
		return model.MarketInstallObservation{State: "unknown", ErrorCode: "CONFIG_TARGET_CHANGED"}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.MarketInstallObservation{}, err
	}
	if !execute {
		return model.MarketInstallObservation{State: "not_observed"}, nil
	}
	var readbackRequest model.MarketInstallStepRequest
	for index, step := range plan.Steps {
		if step.ResourceKey == req.Step.ResourceKey && step.Owner == "thingmodel" && step.Action == "readback" {
			readbackRequest = req
			readbackRequest.Step = step
			readbackRequest.StepID = step.ID
			readbackRequest.PayloadHash, _ = marketInstallStepHash(step)
			readbackRequest.IdempotencyKey = marketInstallStepKey(req.OperationID, step)
			readbackRequest.PriorResults = append([]model.MarketInstallPriorResult{}, req.PriorResults[:index]...)
			break
		}
	}
	if readbackRequest.StepID == "" {
		return model.MarketInstallObservation{}, fmt.Errorf("configuration has no native readback prerequisite")
	}
	observed, err := p.nativeStep(ctx, readbackRequest, false)
	if err != nil {
		return observed, err
	}
	if observed.State != "confirmed" || observed.Receipt == nil {
		return model.MarketInstallObservation{State: "unknown", ErrorCode: "NATIVE_PREREQUISITE_UNKNOWN"}, nil
	}
	var snapshot nativeMarketSnapshotResponse
	if err := json.Unmarshal(observed.Receipt.Data, &snapshot); err != nil {
		return model.MarketInstallObservation{}, err
	}
	var payload marketInstallNativePayload
	if err := json.Unmarshal(req.Step.Payload, &payload); err != nil {
		return model.MarketInstallObservation{}, err
	}
	record := repo.MarketNativeModelImport{TenantID: req.TenantID, MarketTemplateID: payload.MarketTemplateID, MarketVersion: payload.Version, SourceModelID: payload.Dependency.SourceModelID, SourceSnapshotID: payload.Dependency.SourceSnapshotID, SourceRevision: payload.Dependency.SourceRevision, DependencyChecksum: payload.Dependency.DependencyChecksum, CreatedBy: req.SubjectID, LocalModelID: &snapshot.ModelID, LocalSnapshotID: &snapshot.SnapshotID, LocalRevision: &snapshot.PublishedRevision}
	config := buildImportedMarketDeviceConfig(&model.MarketTemplateFullData{Name: payload.Name, DeviceConfig: payload.Config}, &utils.UserClaims{ID: req.SubjectID, TenantID: req.TenantID}, payload.CoverURL, &record)
	receipt, err := repo.ExecuteMarketInstallConfigBinding(ctx, req, record, config)
	if err != nil {
		return model.MarketInstallObservation{}, err
	}
	return configObservation(receipt), nil
}
func configObservation(receipt *repo.MarketInstallConfigReceipt) model.MarketInstallObservation {
	return model.MarketInstallObservation{State: "confirmed", Receipt: &model.MarketInstallReceipt{Owner: "community-config", ResourceID: receipt.ConfigID, PayloadHash: receipt.PayloadHash, Data: receipt.Result}}
}
func (p *MarketInstallProvider) dashboardStep(ctx context.Context, req model.MarketInstallStepRequest, execute bool) (model.MarketInstallObservation, error) {
	if req.Step.Action != "import" {
		return model.MarketInstallObservation{}, repo.ErrMarketInstallUnauthorized
	}
	var payload marketInstallDashboardPayload
	if err := json.Unmarshal(req.Step.Payload, &payload); err != nil {
		return model.MarketInstallObservation{}, err
	}
	for _, proof := range payload.DeviceProofs {
		device, err := dal.GetDeviceByID(proof.DeviceID)
		if err != nil {
			return model.MarketInstallObservation{}, err
		}
		if device == nil || device.TenantID != req.TenantID || device.DeviceConfigID == nil || *device.DeviceConfigID != proof.ConfigID {
			return model.MarketInstallObservation{State: "unknown", ErrorCode: "DEVICE_BINDING_CHANGED"}, nil
		}
		config, err := dal.GetDeviceConfigByID(proof.ConfigID)
		if err != nil {
			return model.MarketInstallObservation{}, err
		}
		if config == nil || config.TenantID != req.TenantID || config.NativeBindingRevision != proof.BindingRevision || config.NativeThingModelID == nil || *config.NativeThingModelID != proof.ModelID || config.NativeThingModelSnapshotID == nil || *config.NativeThingModelSnapshotID != proof.SnapshotID || config.NativeThingModelRevision == nil || *config.NativeThingModelRevision != proof.Revision {
			return model.MarketInstallObservation{State: "unknown", ErrorCode: "DEVICE_BINDING_CHANGED"}, nil
		}
	}
	// Community external identities are not ThingsVis CUIDs. Resolve the
	// existing trusted SSO mapping for this exact live grant on every call.
	var identity struct {
		ProtocolVersion   string `json:"protocolVersion"`
		State             string `json:"state"`
		TenantID          string `json:"tenantId"`
		UserID            string `json:"userId"`
		ErrorCode         string `json:"errorCode"`
		ExternalTenantID  string `json:"externalTenantId"`
		ExternalSubjectID string `json:"externalSubjectId"`
		OperationKey      string `json:"operationKey"`
		PayloadHash       string `json:"payloadHash"`
	}
	identityStatus, err := p.call(ctx, os.Getenv("TP_MARKETINSTALL_THINGSVIS_BASE_URL"), "/api/internal/market-dashboards/import/v1/identity", os.Getenv("TP_MARKETINSTALL_THINGSVIS_IDENTITY_TOKEN"), "X-TP-Service-Token", nil, req, &identity)
	if err != nil {
		return model.MarketInstallObservation{}, err
	}
	if identityStatus != 200 || identity.State != "confirmed" || identity.ProtocolVersion != "thingsvis-import-receipt-v1" || identity.TenantID == "" || identity.UserID == "" {
		state := "unknown"
		if identity.State == "rejected" {
			state = "rejected"
		}
		return model.MarketInstallObservation{State: state, ErrorCode: identity.ErrorCode}, nil
	}
	if identity.ExternalTenantID != req.TenantID || identity.ExternalSubjectID != req.SubjectID || identity.OperationKey != req.IdempotencyKey || identity.PayloadHash != req.PayloadHash {
		return model.MarketInstallObservation{State: "unknown", ErrorCode: "THINGSVIS_IDENTITY_INVALID"}, nil
	}
	dashboard := payload.Dashboard
	var input interface{}
	path := "/api/internal/market-dashboards/import/v1"
	if execute {
		input = struct {
			OperationKey      string                     `json:"operationKey"`
			PayloadHash       string                     `json:"payloadHash"`
			InstanceKey       string                     `json:"instanceKey"`
			ResourceKey       string                     `json:"resourceKey"`
			DashboardSnapshot ThingsVisMarketSnapshot    `json:"dashboardSnapshot"`
			DeviceBindings    []model.DeviceBindingInput `json:"deviceBindings"`
			Name              string                     `json:"name"`
		}{req.IdempotencyKey, req.PayloadHash, req.OperationID, req.Step.ResourceKey, ThingsVisMarketSnapshot{Name: dashboard.Name, SchemaVersion: dashboard.SchemaVersion, CanvasConfig: dashboard.CanvasConfig, Nodes: dashboard.Nodes, DataSources: dashboard.DataSources, Variables: dashboard.Variables}, payload.DeviceBindings, dashboard.Name}
	} else {
		path += "?operationKey=" + url.QueryEscape(req.IdempotencyKey) + "&payloadHash=" + url.QueryEscape(req.PayloadHash)
	}
	var response struct {
		State           string `json:"state"`
		ErrorCode       string `json:"errorCode"`
		OperationKey    string `json:"operationKey"`
		PayloadHash     string `json:"payloadHash"`
		DashboardID     string `json:"dashboardId"`
		InstanceKey     string `json:"instanceKey"`
		ResourceKey     string `json:"resourceKey"`
		ReceiptID       string `json:"receiptId"`
		ProjectID       string `json:"projectId"`
		ProtocolVersion string `json:"protocolVersion"`
		Replayed        bool   `json:"replayed"`
	}
	status, err := p.call(ctx, os.Getenv("TP_MARKETINSTALL_THINGSVIS_BASE_URL"), path, os.Getenv("TP_MARKETINSTALL_THINGSVIS_TOKEN"), "X-Internal-Token", map[string]string{"X-Tenant-ID": identity.TenantID, "X-User-ID": identity.UserID}, input, &response)
	if err != nil {
		return model.MarketInstallObservation{}, err
	}
	if response.State == "unknown" {
		return model.MarketInstallObservation{State: "unknown", ErrorCode: response.ErrorCode}, nil
	}
	if status == 409 && response.State == "rejected" {
		return model.MarketInstallObservation{State: "rejected", ErrorCode: response.ErrorCode}, nil
	}
	if status != 200 && status != 201 {
		return model.MarketInstallObservation{State: "unknown", ErrorCode: "THINGSVIS_UNAVAILABLE"}, nil
	}
	if response.State == "confirmed" {
		if response.ProtocolVersion != "thingsvis-import-receipt-v1" || response.OperationKey != req.IdempotencyKey || response.PayloadHash != req.PayloadHash || response.InstanceKey != req.OperationID || response.ResourceKey != req.Step.ResourceKey || response.DashboardID == "" || response.ReceiptID == "" || response.ProjectID == "" {
			return model.MarketInstallObservation{State: "unknown", ErrorCode: "THINGSVIS_RECEIPT_INVALID"}, nil
		}
		raw, _ := json.Marshal(response)
		return model.MarketInstallObservation{State: "confirmed", Receipt: &model.MarketInstallReceipt{Owner: "thingsvis", ResourceID: response.DashboardID, PayloadHash: req.PayloadHash, Data: raw}}, nil
	}
	return model.MarketInstallObservation{State: response.State, ErrorCode: response.ErrorCode}, nil
}
