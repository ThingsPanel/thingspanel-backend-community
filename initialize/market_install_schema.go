package initialize

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	componentsql "project/sql"
)

const marketInstallMigrationModule = "market-install"
const marketInstallMigrationLock = int64(0x4d41524b4554494e)

func ApplyMarketInstallSchema(db *gorm.DB) error {
	files, err := componentsql.MarketInstallMigrations()
	if err != nil {
		return err
	}
	return applyComponentMigrations(db, componentMigration{
		module: marketInstallMigrationModule, description: "market installation",
		lockKey: marketInstallMigrationLock, files: files, verify: verifyMarketInstallSchema,
	})
}

func verifyMarketInstallSchema(tx *gorm.DB) error {
	if err := verifyNativeColumn(tx, "market_bundle_installations", "request_digest", expectedNativeColumn{
		dataType: "character varying", length: 67, nullable: "NO",
	}); err != nil {
		return err
	}
	var columnDefault string
	if err := tx.Raw(`SELECT column_default FROM information_schema.columns
		WHERE table_schema='public' AND table_name='market_bundle_installations' AND column_name='request_digest'`).Scan(&columnDefault).Error; err != nil {
		return err
	}
	if strings.TrimSpace(columnDefault) != "''::character varying" {
		return fmt.Errorf("market installation request_digest must default to the empty legacy identity")
	}
	for _, expected := range []struct{ table, name, definition string }{
		{"market_bundle_installations", "uq_installation_idempotency_tenant", "UNIQUE (idempotency_key, tenant_id)"},
		{"market_resource_mappings", "uq_resource_mapping", "UNIQUE (installation_id, resource_type, market_resource_key)"},
		{"market_bundle_binding_status", "uq_binding_key", "UNIQUE (installation_id, binding_key)"},
	} {
		if err := verifyNativeConstraint(tx, expected.table, expected.name, expected.definition, "u"); err != nil {
			return err
		}
	}
	return nil
}
