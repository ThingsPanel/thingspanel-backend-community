package service

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"

	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/utils"
)

type marketInstallationLookupOperation struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	ErrorCode string `json:"errorCode"`
	BundleKey string `json:"bundleKey"`
	Version   string `json:"version"`
}

// GetSelectedMarketInstallationByKey makes exactly one read of the selected
// owner. It never calls Submit, Prepare, Resume, or the legacy installation
// writer. The live user token is forwarded only in the authenticated header.
func GetSelectedMarketInstallationByKey(ctx context.Context, key string, claims *utils.UserClaims, rawJWT string) (*model.MarketInstallationLookup, error) {
	if claims == nil || strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(claims.ID) == "" || strings.TrimSpace(rawJWT) == "" {
		return nil, errcode.NewWithMessage(errcode.CodeNoPermission, "installation query requires a live user session")
	}
	if len(key) < 8 || len(key) > 128 || strings.ContainsRune(key, '\x00') {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, "installation key must contain 8 to 128 bytes")
	}
	if os.Getenv("TP_MARKETINSTALL_OWNER") != "encore" {
		// The legacy ledger has no original-subject field. A tenant-only fallback
		// would silently weaken this read contract and hide an owner mismatch.
		return nil, installationLookupUnavailable("INSTALLATION_QUERY_OWNER_UNAVAILABLE")
	}
	if err := requireCurrentMarketInstallationManagement(ctx, claims); err != nil {
		return nil, err
	}
	var observed struct {
		State        string                             `json:"state"`
		OperationKey string                             `json:"operationKey"`
		Operation    *marketInstallationLookupOperation `json:"operation,omitempty"`
	}
	path := "/api/v2/market-installation-operations?" + url.Values{"operationKey": {key}}.Encode()
	status, err := NewMarketInstallProvider().call(ctx, os.Getenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL"), path, rawJWT, "X-Token", nil, nil, &observed)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, errcode.NewWithMessage(errcode.CodeNoPermission, "installation query authorization is no longer current")
	}
	if err != nil || status != http.StatusOK || observed.OperationKey != key {
		return nil, installationLookupUnavailable("INSTALLATION_QUERY_UNCONFIRMED")
	}
	result := &model.MarketInstallationLookup{State: observed.State, OperationKey: key}
	switch observed.State {
	case "not_observed":
		if observed.Operation != nil {
			return nil, installationLookupUnavailable("INSTALLATION_QUERY_UNCONFIRMED")
		}
		return result, nil
	case "confirmed":
		op := observed.Operation
		if op == nil || op.ID == "" || op.BundleKey == "" || op.Version == "" {
			return nil, installationLookupUnavailable("INSTALLATION_QUERY_UNCONFIRMED")
		}
		result.Result = installationForwardResponse(op.ID, op.BundleKey, op.Version, op.State, op.ErrorCode)
		if result.Result.Status == "UNKNOWN" {
			return nil, installationLookupUnavailable("INSTALLATION_QUERY_UNCONFIRMED")
		}
		return result, nil
	default:
		return nil, installationLookupUnavailable("INSTALLATION_QUERY_UNCONFIRMED")
	}
}

func installationLookupUnavailable(code string) error {
	return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
		"errorCode": code,
		"error":     "The original installation could not be confirmed. Retain its operation key and query again; do not submit another installation.",
	})
}
