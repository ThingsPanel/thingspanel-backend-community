package repo

import (
	"context"
	"fmt"
	"testing"

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
