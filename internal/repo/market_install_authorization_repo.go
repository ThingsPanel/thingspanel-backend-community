package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/pkg/global"
)

var ErrMarketInstallUnauthorized = errors.New("installation authorization is no longer current")
var ErrMarketInstallPlanIntegrity = errors.New("installation plan integrity failure")
var ErrMarketInstallGrantOutcomeUnknown = errors.New("installation authorization persistence outcome unknown; query only the original request key")

// This names the existing v1 digest's JSON number-lexeme representation. It is
// not a new digest algorithm and never authorizes rehashing an old grant.
const MarketInstallPlanEncoding = "json-number-lexeme-v1"

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
	PlanExact     *string
	PlanEncoding  *string
	SessionHash   string
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	CreatedAt     time.Time
}

func (MarketInstallAuthorization) TableName() string { return "market_install_authorizations" }
func CreateMarketInstallAuthorization(ctx context.Context, grant *MarketInstallAuthorization) error {
	if grant == nil || grant.PlanExact == nil || grant.PlanEncoding == nil || *grant.PlanEncoding != MarketInstallPlanEncoding || *grant.PlanExact != string(grant.Plan) || !json.Valid(grant.Plan) {
		return ErrMarketInstallPlanIntegrity
	}
	// PostgreSQL stores microseconds. Readback must compare the actual immutable
	// identity, not the caller's nanoseconds that the database cannot retain.
	grant.ExpiresAt = grant.ExpiresAt.UTC().Truncate(time.Microsecond)
	grant.CreatedAt = grant.CreatedAt.UTC().Truncate(time.Microsecond)
	expected := *grant
	return marketInstallGrantTransaction(ctx, func(tx *gorm.DB) error {
		created := tx.Create(grant)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected != 1 {
			return fmt.Errorf("installation authorization was not inserted exactly once")
		}
		return verifyMarketInstallGrantWrite(tx, expected, *expected.PlanExact)
	})
}

// SealMarketInstallAuthorizationPlan is called only after the service has
// verified the legacy plan against its ORIGINAL PlanDigest and rechecked the
// current session/account/permission. It fills representation metadata only;
// it never renews a grant, changes its digest, binds an operation or reissues a
// session marker. The locked immutable row must still match that observation.
func SealMarketInstallAuthorizationPlan(ctx context.Context, expected MarketInstallAuthorization, exact string) error {
	if expected.PlanExact != nil || expected.PlanEncoding != nil || !json.Valid([]byte(exact)) {
		return ErrMarketInstallPlanIntegrity
	}
	return marketInstallGrantTransaction(ctx, func(tx *gorm.DB) error {
		var current MarketInstallAuthorization
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND subject_id=? AND purpose=? AND revoked_at IS NULL", expected.ID, expected.TenantID, expected.SubjectID, expected.Purpose).Take(&current)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrMarketInstallUnauthorized
		}
		if result.Error != nil {
			return result.Error
		}
		// Check after acquiring the row lock so time spent waiting cannot revive
		// an expired authorization. This is not a downstream execution fence.
		if !current.ExpiresAt.After(time.Now().UTC()) || !sameMarketInstallGrantIdentity(current, expected) {
			return ErrMarketInstallUnauthorized
		}
		if current.PlanExact != nil || current.PlanEncoding != nil {
			// Another reader may have sealed the same verified original plan.
			if current.PlanExact == nil || current.PlanEncoding == nil || *current.PlanExact != exact || *current.PlanEncoding != MarketInstallPlanEncoding {
				return ErrMarketInstallPlanIntegrity
			}
			return verifyMarketInstallGrantWrite(tx, expected, exact)
		}
		updated := tx.Model(&MarketInstallAuthorization{}).Where("id=? AND plan_exact IS NULL AND plan_encoding IS NULL AND plan_digest=?", expected.ID, expected.PlanDigest).Updates(map[string]interface{}{"plan_exact": exact, "plan_encoding": MarketInstallPlanEncoding})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return fmt.Errorf("installation authorization exact plan was not sealed exactly once")
		}
		return verifyMarketInstallGrantWrite(tx, expected, exact)
	})
}

func verifyMarketInstallGrantWrite(tx *gorm.DB, expected MarketInstallAuthorization, exact string) error {
	var stored MarketInstallAuthorization
	// The plan digest and exact representation metadata are the integrity fence.
	// Comparing a JSON column to a text bind has different semantics in PG and MySQL.
	result := tx.Where("id=? AND plan_digest=?", expected.ID, expected.PlanDigest).Take(&stored)
	if result.Error != nil {
		return result.Error
	}
	if !sameMarketInstallGrantIdentity(stored, expected) || stored.PlanExact == nil || stored.PlanEncoding == nil || *stored.PlanExact != exact || *stored.PlanEncoding != MarketInstallPlanEncoding {
		return ErrMarketInstallPlanIntegrity
	}
	return nil
}

func sameMarketInstallGrantIdentity(a, b MarketInstallAuthorization) bool {
	return a.ID == b.ID && a.TenantID == b.TenantID && a.SubjectID == b.SubjectID && a.Authority == b.Authority &&
		a.AuthVersion == b.AuthVersion && a.ActorID == b.ActorID && a.ActorVersion == b.ActorVersion && a.Delegated == b.Delegated &&
		a.Purpose == b.Purpose && a.RequestKey == b.RequestKey && a.RequestDigest == b.RequestDigest && a.OperationID == b.OperationID &&
		a.PlanDigest == b.PlanDigest && a.SessionHash == b.SessionHash && a.ExpiresAt.Equal(b.ExpiresAt) && a.CreatedAt.Equal(b.CreatedAt) &&
		((a.RevokedAt == nil && b.RevokedAt == nil) || (a.RevokedAt != nil && b.RevokedAt != nil && a.RevokedAt.Equal(*b.RevokedAt)))
}

func marketInstallGrantTransaction(ctx context.Context, apply func(*gorm.DB) error) error {
	tx := global.DB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	finished := false
	defer func() {
		if !finished {
			_ = tx.Rollback().Error
		}
	}()
	if err := apply(tx); err != nil {
		rollbackErr := tx.Rollback().Error
		finished = true
		if rollbackErr != nil {
			return errors.Join(ErrMarketInstallGrantOutcomeUnknown, err, rollbackErr)
		}
		return err
	}
	commitErr := tx.Commit().Error
	finished = true
	if commitErr != nil {
		return errors.Join(ErrMarketInstallGrantOutcomeUnknown, commitErr)
	}
	return nil
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
