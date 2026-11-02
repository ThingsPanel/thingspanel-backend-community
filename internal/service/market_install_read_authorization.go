package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

const MarketInstallationReadResource = "api/v1/device/market/bundles/install"

// AuthorizeCurrentMarketInstallationManagement shares the existing user table
// and canonical install Casbin resource for reads, submission and resume. The
// permission assertion neither creates a grant nor approves device execution.
// Callers must already authenticate a current session, or be the trusted
// provider transport carrying an identity verified by that native session.
func AuthorizeCurrentMarketInstallationManagement(ctx context.Context, identity model.MarketInstallationReadIdentity) (*model.MarketInstallationReadAuthorization, error) {
	if identity.ProtocolVersion != 1 || strings.TrimSpace(identity.TenantID) == "" || strings.TrimSpace(identity.SubjectID) == "" {
		return nil, repo.ErrMarketInstallUnauthorized
	}
	if ctx == nil || global.DB == nil || global.CasbinEnforcer == nil {
		return nil, errors.New("installation management authorization is unavailable")
	}
	var user struct {
		ID        string
		TenantID  *string
		Authority *string
		Status    *string
	}
	err := global.DB.WithContext(ctx).Table("users").Select("id,tenant_id,authority,status").Where("id = ?", identity.SubjectID).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repo.ErrMarketInstallUnauthorized
	}
	if err != nil {
		return nil, errors.New("installation management account lookup is unavailable")
	}
	if user.ID != identity.SubjectID || user.TenantID == nil || *user.TenantID != identity.TenantID || user.Status == nil || *user.Status != "N" || user.Authority == nil || (*user.Authority != "TENANT_ADMIN" && *user.Authority != "SYS_ADMIN") {
		return nil, repo.ErrMarketInstallUnauthorized
	}
	permission := &Casbin{}
	if !permission.GetUrl(MarketInstallationReadResource) || !permission.Verify(identity.SubjectID, MarketInstallationReadResource) {
		return nil, repo.ErrMarketInstallUnauthorized
	}
	return &model.MarketInstallationReadAuthorization{ProtocolVersion: 1, Authorized: true, TenantID: identity.TenantID, SubjectID: identity.SubjectID, Resource: MarketInstallationReadResource}, nil
}

func requireCurrentMarketInstallationManagement(ctx context.Context, claims *utils.UserClaims) error {
	if claims == nil || strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(claims.ID) == "" {
		return errcode.NewWithMessage(errcode.CodeNoPermission, "installation management requires a current identity")
	}
	switch strings.ToUpper(claims.Authority) {
	case "TENANT_ADMIN", "SYS_ADMIN", "PLATFORM_ADMIN":
	default:
		return errcode.NewWithMessage(errcode.CodeNoPermission, "installation management permission required")
	}
	_, err := AuthorizeCurrentMarketInstallationManagement(ctx, model.MarketInstallationReadIdentity{ProtocolVersion: 1, TenantID: claims.TenantID, SubjectID: claims.ID})
	if errors.Is(err, repo.ErrMarketInstallUnauthorized) {
		return errcode.NewWithMessage(errcode.CodeNoPermission, "current installation resource permission required")
	}
	if err != nil {
		return errcode.NewWithMessage(errcode.CodeSystemError, "current installation management authorization could not be confirmed")
	}
	return nil
}
