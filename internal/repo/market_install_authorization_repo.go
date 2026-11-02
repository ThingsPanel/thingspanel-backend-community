package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"project/pkg/global"
)

var ErrMarketInstallUnauthorized = errors.New("installation authorization is no longer current")

type MarketInstallAuthorization struct {
	ID            string
	TenantID      string
	SubjectID     string
	Authority     string
	AuthVersion   int64
	ActorID       string
	ActorVersion  int64
	Delegated     bool
	Purpose       string
	RequestKey    string
	RequestDigest string
	OperationID   string
	PlanDigest    string
	Plan          json.RawMessage
	SessionHash   string
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	CreatedAt     time.Time
}

func (MarketInstallAuthorization) TableName() string { return "market_install_authorizations" }
func CreateMarketInstallAuthorization(ctx context.Context, grant *MarketInstallAuthorization) error {
	return global.DB.WithContext(ctx).Create(grant).Error
}
func ReadMarketInstallAuthorization(ctx context.Context, ref, tenant, subject string) (*MarketInstallAuthorization, error) {
	var grant MarketInstallAuthorization
	err := global.DB.WithContext(ctx).Where("id=? AND tenant_id=? AND subject_id=? AND purpose=? AND revoked_at IS NULL AND expires_at>?", ref, tenant, subject, "a08.install.v1", time.Now().UTC()).First(&grant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrMarketInstallUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if err := MarketInstallAccountCurrent(ctx, grant); err != nil {
		return nil, err
	}
	return &grant, nil
}

// Reads the existing identity owner's account/version schema. It never creates
// users, issues tokens, changes auth modes, or expands privileges.
func MarketInstallAccountCurrent(ctx context.Context, grant MarketInstallAuthorization) error {
	var count int64
	err := global.DB.WithContext(ctx).Table("users").Where("id=? AND status='N' AND auth_version=? AND authority=? AND COALESCE(tenant_id,'')=?", grant.SubjectID, grant.AuthVersion, grant.Authority, grant.TenantID).Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrMarketInstallUnauthorized
	}
	if grant.Authority != "TENANT_ADMIN" && grant.Authority != "SYS_ADMIN" {
		return ErrMarketInstallUnauthorized
	}
	if grant.Delegated {
		if grant.ActorID == "" {
			return ErrMarketInstallUnauthorized
		}
		err = global.DB.WithContext(ctx).Table("users").Where("id=? AND status='N' AND auth_version=? AND (authority='SYS_ADMIN' OR(authority='TENANT_ADMIN' AND tenant_id=? AND ? <> 'SYS_ADMIN'))", grant.ActorID, grant.ActorVersion, grant.TenantID, grant.Authority).Count(&count).Error
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrMarketInstallUnauthorized
		}
	}
	return nil
}

func BindMarketInstallGrantOperation(ctx context.Context, ref, tenant, subject, operation string) error {
	result := global.DB.WithContext(ctx).Model(&MarketInstallAuthorization{}).Where("id=? AND tenant_id=? AND subject_id=? AND (operation_id='' OR operation_id=?) AND revoked_at IS NULL AND expires_at>?", ref, tenant, subject, operation, time.Now().UTC()).Update("operation_id", operation)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrMarketInstallUnauthorized
	}
	return nil
}

func FindMarketInstallAuthorizationByKey(ctx context.Context, tenant, subject, key string) (*MarketInstallAuthorization, error) {
	var grant MarketInstallAuthorization
	err := global.DB.WithContext(ctx).Where("tenant_id=? AND request_key=?", tenant, key).First(&grant).Error
	if err != nil {
		return nil, err
	}
	if grant.SubjectID != subject {
		return nil, ErrMarketInstallUnauthorized
	}
	return &grant, nil
}
