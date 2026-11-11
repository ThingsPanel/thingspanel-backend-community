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
		{"market_install_authorizations", "uq_market_install_authorization_request", "UNIQUE (tenant_id, request_key)"},
		{"market_install_config_receipts", "uq_market_install_config_resource", "UNIQUE (tenant_id, instance_key, resource_key)"},
	} {
		if err := verifyNativeConstraint(tx, expected.table, expected.name, expected.definition, "u"); err != nil {
			return err
		}
	}
	for table, columns := range map[string]map[string]expectedNativeColumn{
		"market_install_authorizations": {
			"id": {dataType: "character varying", length: 36, nullable: "NO"}, "tenant_id": {dataType: "text", nullable: "NO"}, "subject_id": {dataType: "text", nullable: "NO"},
			"authority": {dataType: "text", nullable: "NO"}, "auth_version": {dataType: "bigint", nullable: "NO"}, "actor_id": {dataType: "text", nullable: "NO"}, "actor_version": {dataType: "bigint", nullable: "NO"},
			"delegated": {dataType: "boolean", nullable: "NO"}, "purpose": {dataType: "text", nullable: "NO"}, "request_key": {dataType: "character varying", length: 128, nullable: "NO"},
			"request_digest": {dataType: "character varying", length: 67, nullable: "NO"}, "plan_digest": {dataType: "character", length: 64, nullable: "NO"}, "operation_id": {dataType: "text", nullable: "NO"},
			"plan": {dataType: "jsonb", nullable: "NO"}, "session_hash": {dataType: "text", nullable: "NO"}, "expires_at": {dataType: "timestamp with time zone", nullable: "NO"}, "revoked_at": {dataType: "timestamp with time zone", nullable: "YES"}, "created_at": {dataType: "timestamp with time zone", nullable: "NO"},
			"plan_exact": {dataType: "text", nullable: "YES"}, "plan_encoding": {dataType: "text", nullable: "YES"},
		},
		"market_install_config_receipts": {
			"tenant_id": {dataType: "text", nullable: "NO"}, "subject_id": {dataType: "text", nullable: "NO"}, "operation_key": {dataType: "character", length: 64, nullable: "NO"},
			"payload_hash": {dataType: "character", length: 64, nullable: "NO"}, "instance_key": {dataType: "text", nullable: "NO"}, "resource_key": {dataType: "character varying", length: 128, nullable: "NO"},
			"config_id": {dataType: "character varying", length: 36, nullable: "NO"}, "binding_revision": {dataType: "bigint", nullable: "NO"}, "result": {dataType: "jsonb", nullable: "NO"}, "created_at": {dataType: "timestamp with time zone", nullable: "NO"},
		},
	} {
		for name, expected := range columns {
			if err := verifyNativeColumn(tx, table, name, expected); err != nil {
				return err
			}
		}
	}
	if err := verifyNativeConstraint(tx, "market_install_config_receipts", "market_install_config_receipts_pkey", "PRIMARY KEY (tenant_id, subject_id, operation_key)", "p"); err != nil {
		return err
	}
	if err := verifyNativeConstraint(tx, "market_install_authorizations", "market_install_authorizations_pkey", "PRIMARY KEY (id)", "p"); err != nil {
		return err
	}
	if err := verifyNativeConstraint(tx, "market_install_authorizations", "market_install_authorizations_purpose_check", `CHECK ((purpose = 'a08.install.v1'::text))`, "c"); err != nil {
		return err
	}
	if err := verifyNativeConstraint(tx, "market_install_authorizations", "market_install_authorizations_exact_plan_pair_check", `CHECK (((plan_exact IS NULL) = (plan_encoding IS NULL)))`, "c"); err != nil {
		return err
	}
	if err := verifyNativeConstraint(tx, "market_install_authorizations", "market_install_authorizations_plan_encoding_check", `CHECK (((plan_encoding IS NULL) OR (plan_encoding = 'json-number-lexeme-v1'::text)))`, "c"); err != nil {
		return err
	}
	return nil
}
