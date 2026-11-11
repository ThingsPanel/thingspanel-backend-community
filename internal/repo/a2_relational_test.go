package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational/reltest"
	"project/pkg/global"
)

func TestMarketNativeImportTenantBoundaryPostgresMySQL(t *testing.T) {
	reltest.Run(t, "market-native-import-tenant-boundary", func(s *reltest.Scenario) {
		s.Step("create isolated native import table", func(ctx context.Context, db *gorm.DB) (any, error) {
			ddl := `CREATE TABLE market_native_model_imports (
				id VARCHAR(36) PRIMARY KEY DEFAULT gen_random_uuid()::text,
				tenant_id VARCHAR(100) NOT NULL, market_template_id VARCHAR(100) NOT NULL, market_version VARCHAR(128) NOT NULL,
				source_model_id VARCHAR(36) NOT NULL, source_snapshot_id VARCHAR(36) NOT NULL, source_revision INT NOT NULL,
				dependency_checksum CHAR(64) NOT NULL, created_by VARCHAR(100) NOT NULL, status VARCHAR(20) NOT NULL,
				local_model_id VARCHAR(36), local_snapshot_id VARCHAR(36), local_revision INT, local_device_config_id VARCHAR(36),
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE (tenant_id, market_template_id, market_version))`
			if relational.MustDialectOf(db) == relational.MySQL {
				ddl = `CREATE TABLE market_native_model_imports (
					id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
					tenant_id VARCHAR(100) NOT NULL, market_template_id VARCHAR(100) NOT NULL, market_version VARCHAR(128) NOT NULL,
					source_model_id VARCHAR(36) NOT NULL, source_snapshot_id VARCHAR(36) NOT NULL, source_revision INT NOT NULL,
					dependency_checksum CHAR(64) NOT NULL, created_by VARCHAR(100) NOT NULL, status VARCHAR(20) NOT NULL,
					local_model_id VARCHAR(36), local_snapshot_id VARCHAR(36), local_revision INT, local_device_config_id VARCHAR(36),
					created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
					UNIQUE (tenant_id, market_template_id, market_version)) ENGINE=InnoDB`
			}
			return nil, db.WithContext(ctx).Exec(ddl).Error
		})
		s.Step("same template version is unique within a tenant only", func(ctx context.Context, db *gorm.DB) (any, error) {
			previous := global.DB
			global.DB = db
			defer func() { global.DB = previous }()
			candidate := MarketNativeModelImport{TenantID: "tenant-a", MarketTemplateID: "template-shared", MarketVersion: "1.0.0",
				SourceModelID: "model-a", SourceSnapshotID: "snapshot-a", SourceRevision: 1,
				DependencyChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedBy: "installer-a"}
			_, first, err := ReserveMarketNativeModelImport(ctx, candidate)
			if err != nil {
				return nil, err
			}
			if !first {
				return nil, fmt.Errorf("first reservation created=%t, want true", first)
			}
			_, replay, err := ReserveMarketNativeModelImport(ctx, candidate)
			if err != nil {
				return nil, err
			}
			if replay {
				return nil, fmt.Errorf("same-tenant replay created=%t, want false", replay)
			}
			otherTenant := candidate
			otherTenant.TenantID, otherTenant.SourceModelID, otherTenant.CreatedBy = "tenant-b", "model-b", "installer-b"
			_, otherCreated, err := ReserveMarketNativeModelImport(ctx, otherTenant)
			if err != nil {
				return nil, err
			}
			if !otherCreated {
				return nil, fmt.Errorf("other-tenant reservation created=%t, want true", otherCreated)
			}
			return fmt.Sprintf("first=%t replay=%t other-tenant-created=%t", first, replay, otherCreated), nil
		})
	})
}

func TestMarketInstallAuthorizationCreateAndLegacySealPostgresMySQL(t *testing.T) {
	reltest.Run(t, "market-install-authorization-create-and-legacy-seal", func(s *reltest.Scenario) {
		s.Step("create authorization table with legacy and exact-plan columns", func(ctx context.Context, db *gorm.DB) (any, error) {
			planType, timeType := "JSONB", "TIMESTAMPTZ"
			if relational.MustDialectOf(db) == relational.MySQL {
				planType, timeType = "JSON", "DATETIME(6)"
			}
			ddl := `CREATE TABLE market_install_authorizations (
				id VARCHAR(36) PRIMARY KEY, tenant_id VARCHAR(100) NOT NULL, subject_id VARCHAR(100) NOT NULL,
				authority VARCHAR(100) NOT NULL, auth_version BIGINT NOT NULL, actor_id VARCHAR(100) NOT NULL,
				actor_version BIGINT NOT NULL, delegated BOOLEAN NOT NULL, purpose VARCHAR(100) NOT NULL,
				request_key VARCHAR(128) NOT NULL, request_digest VARCHAR(67) NOT NULL, plan_digest CHAR(64) NOT NULL,
				operation_id VARCHAR(100) NOT NULL, plan ` + planType + ` NOT NULL, plan_exact TEXT NULL,
				plan_encoding VARCHAR(64) NULL, session_hash VARCHAR(128) NOT NULL, expires_at ` + timeType + ` NOT NULL,
				revoked_at ` + timeType + ` NULL, created_at ` + timeType + ` NOT NULL,
				UNIQUE (tenant_id, request_key))`
			return nil, db.WithContext(ctx).Exec(ddl).Error
		})
		s.Step("create a current authorization and seal only the matching tenant's legacy row", func(ctx context.Context, db *gorm.DB) (any, error) {
			previous := global.DB
			global.DB = db
			defer func() { global.DB = previous }()
			now := time.Now().UTC().Truncate(time.Microsecond)
			plan := json.RawMessage(`{"version":1,"count":1.0}`)
			exact, encoding := string(plan), MarketInstallPlanEncoding
			created := MarketInstallAuthorization{ID: "grant-new", TenantID: "tenant-a", SubjectID: "subject-a", Authority: "TENANT_ADMIN",
				AuthVersion: 3, ActorID: "subject-a", ActorVersion: 3, Purpose: "a08.install.v1", RequestKey: "new-request",
				RequestDigest: "request-digest", PlanDigest: strings.Repeat("a", 64), Plan: plan, PlanExact: &exact, PlanEncoding: &encoding,
				SessionHash: "session-new", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
			if err := CreateMarketInstallAuthorization(ctx, &created); err != nil {
				return nil, fmt.Errorf("create authorization: %w", err)
			}
			legacy := MarketInstallAuthorization{ID: "grant-legacy", TenantID: "tenant-a", SubjectID: "subject-a", Authority: "TENANT_ADMIN",
				AuthVersion: 3, ActorID: "subject-a", ActorVersion: 3, Purpose: "a08.install.v1", RequestKey: "legacy-request",
				RequestDigest: "legacy-digest", PlanDigest: strings.Repeat("b", 64), Plan: plan, SessionHash: "session-legacy",
				ExpiresAt: now.Add(time.Hour), CreatedAt: now}
			if err := db.WithContext(ctx).Create(&legacy).Error; err != nil {
				return nil, fmt.Errorf("seed legacy authorization: %w", err)
			}
			wrongTenant := legacy
			wrongTenant.TenantID = "tenant-b"
			if err := SealMarketInstallAuthorizationPlan(ctx, wrongTenant, string(plan)); err != ErrMarketInstallUnauthorized {
				return nil, fmt.Errorf("cross-tenant seal error=%v, want unauthorized", err)
			}
			if err := SealMarketInstallAuthorizationPlan(ctx, legacy, string(plan)); err != nil {
				return nil, fmt.Errorf("seal matching legacy authorization: %w", err)
			}
			var sealed MarketInstallAuthorization
			if err := db.WithContext(ctx).Where("id=? AND tenant_id=?", legacy.ID, legacy.TenantID).Take(&sealed).Error; err != nil {
				return nil, err
			}
			if sealed.PlanExact == nil || sealed.PlanEncoding == nil || *sealed.PlanExact != string(plan) || *sealed.PlanEncoding != MarketInstallPlanEncoding {
				return nil, fmt.Errorf("legacy row not sealed exactly: exact=%v encoding=%v", sealed.PlanExact, sealed.PlanEncoding)
			}
			return "created current grant; cross-tenant seal rejected; legacy grant sealed and read back", nil
		})
	})
}
