package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"

	"github.com/go-basic/uuid"
	"gorm.io/gorm"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/errcode"
	"project/pkg/utils"
)

func InstallMarketBundleWithSelectedOwner(ctx context.Context, req *model.InstallBundleRequest, claims *utils.UserClaims, rawJWT string) (*model.InstallBundleResponse, error) {
	if err := requireCurrentMarketInstallationManagement(ctx, claims); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, "installation request required")
	}
	switch os.Getenv("TP_MARKETINSTALL_OWNER") {
	case "", "legacy":
		return NewMarketBundleInstallService().InstallBundle(ctx, req, claims)
	case "encore":
	default:
		return nil, errcode.WithData(errcode.CodeSystemError, "invalid market installation owner configuration")
	}
	if claims == nil || rawJWT == "" {
		return nil, errcode.WithData(errcode.CodeOpDenied, "installation requires a live user session")
	}
	key := req.IdempotencyKey
	if key == "" {
		key = uuid.New()
	}
	if len(key) < 8 || len(key) > 128 {
		return nil, errcode.WithData(errcode.CodeParamError, "installation key must contain 8 to 128 bytes")
	}
	request := model.MarketInstallRequest{BundleKey: req.BundleKey, Version: req.Version, OverwritePolicy: req.OverwritePolicy, DeviceBindings: req.DeviceBindings}
	digest, err := marketInstallRequestDigest(request, claims.TenantID, claims.ID)
	if err != nil {
		return nil, err
	}
	grant, err := repo.FindMarketInstallAuthorizationByKey(ctx, claims.TenantID, claims.ID, key)
	if err == nil {
		if grant.RequestDigest != digest {
			return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{"errorCode": "IDEMPOTENCY_PAYLOAD_CONFLICT"})
		}
		if _, _, err := readCurrentMarketInstallGrant(ctx, model.MarketInstallIdentity{TenantID: claims.TenantID, SubjectID: claims.ID, AuthorizationRef: grant.ID}); err != nil {
			return nil, err
		}
	} else {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		authorization := "Bearer " + rawJWT
		plan, err := prepareMarketInstallPlan(ctx, req, claims, authorization)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{"error": err.Error()})
		}
		grant, err = IssueMarketInstallAuthorization(ctx, request, plan, claims, rawJWT, key)
		if err != nil {
			return nil, err
		}
	}
	var operation struct {
		ID        string `json:"id"`
		State     string `json:"state"`
		ErrorCode string `json:"errorCode"`
		BundleKey string `json:"bundleKey"`
		Version   string `json:"version"`
	}
	provider := NewMarketInstallProvider()
	input := struct {
		AuthorizationRef string                     `json:"authorizationRef"`
		Request          model.MarketInstallRequest `json:"request"`
	}{grant.ID, request}
	// The coordinator's ordinary authenticated transport receives the original
	// live user credential only in flight. It is never persisted by either owner.
	status, err := provider.call(ctx, os.Getenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL"), "/api/v2/market-installations", rawJWT, "X-Token", map[string]string{"Idempotency-Key": key}, input, &operation)
	if err != nil || status != http.StatusOK || operation.ID == "" {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{"errorCode": "INSTALLATION_SUBMIT_RESULT_UNKNOWN", "idempotencyKey": key, "error": "Query or retry only the same operation identity"})
	}
	return installationForwardResponse(operation.ID, req.BundleKey, req.Version, operation.State, operation.ErrorCode), nil
}
func GetSelectedMarketInstallation(ctx context.Context, id string, claims *utils.UserClaims, rawJWT string) (*model.InstallBundleResponse, error) {
	if err := requireCurrentMarketInstallationManagement(ctx, claims); err != nil {
		return nil, err
	}
	if os.Getenv("TP_MARKETINSTALL_OWNER") != "encore" {
		return NewMarketBundleInstallService().GetInstallationStatus(ctx, id, claims.TenantID)
	}
	if rawJWT == "" {
		return nil, errcode.WithData(errcode.CodeOpDenied, "installation requires a live user session")
	}
	var operation struct {
		ID        string `json:"id"`
		State     string `json:"state"`
		ErrorCode string `json:"errorCode"`
		BundleKey string `json:"bundleKey"`
		Version   string `json:"version"`
	}
	status, err := NewMarketInstallProvider().call(ctx, os.Getenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL"), "/api/v2/market-installations/"+url.PathEscape(id), rawJWT, "X-Token", nil, nil, &operation)
	if err != nil || status != 200 || operation.ID != id {
		return nil, errcode.WithData(errcode.CodeSystemError, "installation query is unavailable")
	}
	return installationForwardResponse(operation.ID, operation.BundleKey, operation.Version, operation.State, operation.ErrorCode), nil
}
func installationForwardResponse(id, bundle, version, state, code string) *model.InstallBundleResponse {
	status := map[string]string{"queued": "QUEUED", "running": "RUNNING", "paused": model.InstallStateCompensationRequired, "completed": model.InstallStateCompleted, "rejected": model.InstallStateFailed}[state]
	if status == "" {
		status = "UNKNOWN"
	}
	response := &model.InstallBundleResponse{InstallationID: id, BundleKey: bundle, Version: version, Status: status}
	if code != "" {
		response.Errors = []string{code}
	}
	return response
}
