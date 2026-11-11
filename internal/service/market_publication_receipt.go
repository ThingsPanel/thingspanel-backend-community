package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/utils"
)

type MarketPublicationConflictError struct{}

func (*MarketPublicationConflictError) Error() string {
	return "publication key or immutable version conflicts"
}
func (*MarketPublicationConflictError) Unwrap() error { return ErrMarketRequestRejected }

// This is an operation namespace, not a publisher claim. Horizon establishes
// the personal publisher from the verified market access token. Tenant and
// platform subject come only from the authenticated platform claims.
func dashboardPublicationOperationKey(claims *utils.UserClaims, operationKey string) (string, error) {
	if claims == nil || strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(claims.ID) == "" {
		return "", errcode.WithData(errcode.CodeNoPermission, "authenticated tenant subject required")
	}
	if operationKey == "" || len(operationKey) > 128 || strings.TrimSpace(operationKey) != operationKey || strings.ContainsAny(operationKey, "\r\n\t\x00") {
		return "", errcode.WithData(errcode.CodeParamError, "invalid publication operationKey")
	}
	identity, _ := json.Marshal([]string{"dashboard-publication-v1", claims.TenantID, claims.ID, operationKey})
	digest := sha256.Sum256(identity)
	return hex.EncodeToString(digest[:]), nil
}

// QueryPublicationReceipt performs no ThingsVis export, dependency lookup,
// upload or publication retry. It can therefore recover the original outcome
// even after the source dashboard has changed or been deleted.
func (s *MarketDashboardBundleService) QueryPublicationReceipt(ctx context.Context, operationKey, marketToken string, claims *utils.UserClaims) (*model.PublicationReceiptResponse, error) {
	key, err := dashboardPublicationOperationKey(claims, operationKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(marketToken) == "" {
		return nil, errcode.WithData(errcode.CodeNoPermission, "market authentication required")
	}
	result, err := s.market.GetBundlePublicationReceipt(ctx, marketToken, key)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, "publication result is unknown; query the original operation again")
	}
	result.OperationKey = operationKey
	return result, nil
}

func (c *MarketClient) GetBundlePublicationReceipt(ctx context.Context, token, key string) (*model.PublicationReceiptResponse, error) {
	if token == "" || key == "" || len(key) > 255 || strings.ContainsAny(key, "\r\n\t\x00") {
		return nil, fmt.Errorf("invalid publication query credentials or key")
	}
	endpoint := fmt.Sprintf("%s/api/market/bundles/publication-receipts/%s", strings.TrimRight(c.baseURL, "/"), url.PathEscape(key))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("publication query response unknown: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: publication query status=%d", ErrMarketRequestRejected, response.StatusCode)
	}
	const maxReceiptBytes = 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReceiptBytes+1))
	if err != nil || len(body) > maxReceiptBytes {
		return nil, fmt.Errorf("%w: publication query body unavailable", ErrMarketInvalidResponse)
	}
	var envelope model.HorizonPublicationReceiptResponse
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code != 0 || envelope.Data == nil {
		return nil, fmt.Errorf("%w: invalid publication query envelope", ErrMarketInvalidResponse)
	}
	result := envelope.Data
	switch result.State {
	case "not_observed":
		if result.Result != nil || result.TargetState != "" {
			return nil, fmt.Errorf("%w: contradictory publication query", ErrMarketInvalidResponse)
		}
	case "confirmed":
		if result.Result == nil || result.Result.BundleKey == "" || result.Result.Version == "" || result.Result.ContentHash == "" || result.Result.Status == "" {
			return nil, fmt.Errorf("%w: incomplete publication receipt", ErrMarketInvalidResponse)
		}
		if result.TargetState != "" && result.Result.TargetState != "" && result.TargetState != result.Result.TargetState {
			return nil, fmt.Errorf("%w: conflicting target states", ErrMarketInvalidResponse)
		}
		if result.TargetState == "" {
			result.TargetState = result.Result.TargetState
		}
		if result.TargetState == "" || result.Result.VersionID == "" {
			result.TargetState = "unknown"
		}
		switch result.TargetState {
		case "pending_review", "published", "rejected", "unpublished", "deleted", "unknown":
		default:
			return nil, fmt.Errorf("%w: unrecognized publication target state", ErrMarketInvalidResponse)
		}
		result.Result.TargetState = result.TargetState
	default:
		return nil, fmt.Errorf("%w: unrecognized publication query state", ErrMarketInvalidResponse)
	}
	return result, nil
}
