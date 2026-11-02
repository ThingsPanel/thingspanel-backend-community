package initialize

import (
	"gorm.io/gorm"
	componentsql "project/sql"
)

const serviceAccessMigrationModule = "service-access"

// ApplyServiceAccessSchema registers a new owner-specific version in the
// existing component runner. It does not adopt or change historical migrations.
func ApplyServiceAccessSchema(db *gorm.DB) error {
	files, err := componentsql.ServiceAccessMigrations()
	if err != nil {
		return err
	}
	return applyComponentMigrations(db, componentMigration{
		module: serviceAccessMigrationModule, description: "service access creation receipts",
		lockKey: int64(0x5356434143434553), files: files, verify: verifyServiceAccessSchema,
	})
}

func verifyServiceAccessSchema(tx *gorm.DB) error {
	for name, expected := range map[string]expectedNativeColumn{
		"tenant_id":     {dataType: "text", nullable: "NO"},
		"operation_key": {dataType: "character varying", length: 200, nullable: "NO"},
		"subject_id":    {dataType: "text", nullable: "NO"},
		"request_hash":  {dataType: "character", length: 64, nullable: "NO"},
		"access_id":     {dataType: "character varying", length: 36, nullable: "NO"},
		"resource_hash": {dataType: "character", length: 64, nullable: "NO"},
		"created_at":    {dataType: "timestamp with time zone", nullable: "NO"},
	} {
		if err := verifyNativeColumn(tx, "service_access_create_receipts", name, expected); err != nil {
			return err
		}
	}
	for _, constraint := range []struct{ name, definition, kind string }{
		{"service_access_create_receipts_pkey", "PRIMARY KEY (tenant_id, operation_key)", "p"},
		{"service_access_create_receipts_access_unique", "UNIQUE (access_id)", "u"},
		{"service_access_create_receipts_tenant_check", "CHECK ((length(tenant_id) > 0))", "c"},
		{"service_access_create_receipts_subject_check", "CHECK ((length(subject_id) > 0))", "c"},
		{"service_access_create_receipts_key_check", "CHECK (((operation_key)::text ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$'::text))", "c"},
		{"service_access_create_receipts_request_hash_check", "CHECK ((request_hash ~ '^[0-9a-f]{64}$'::text))", "c"},
		{"service_access_create_receipts_resource_hash_check", "CHECK ((resource_hash ~ '^[0-9a-f]{64}$'::text))", "c"},
	} {
		if err := verifyNativeConstraint(tx, "service_access_create_receipts", constraint.name, constraint.definition, constraint.kind); err != nil {
			return err
		}
	}
	return nil
}
