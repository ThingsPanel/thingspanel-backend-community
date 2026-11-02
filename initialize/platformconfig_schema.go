package initialize

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	componentsql "project/sql"
)

const platformConfigMigrationModule = "platformconfig-dictionary"

// ApplyPlatformConfigSchema adopts the existing public dictionary table family
// through the same transaction/exclusion mechanism as other Community owners.
// Its module/file/checksum receipt is independent of historical sys_version.
// The native host never invokes a competing migration runner on startup.
func ApplyPlatformConfigSchema(db *gorm.DB) error {
	files, err := componentsql.PlatformConfigMigrations()
	if err != nil {
		return err
	}
	return applyComponentMigrations(db, componentMigration{
		module: platformConfigMigrationModule, description: "platform dictionary owner",
		lockKey: int64(0x504c4154434f4e46), files: files, verify: verifyPlatformConfigSchema,
	})
}

func verifyPlatformConfigSchema(tx *gorm.DB) error {
	files, err := componentsql.PlatformConfigMigrations()
	if err != nil || len(files) != 1 {
		return fmt.Errorf("dictionary schema source is unavailable")
	}
	parts := strings.Split(string(files[0].Contents), "$dictionary_schema$")
	if len(parts) != 3 {
		return fmt.Errorf("dictionary schema source is invalid")
	}
	var function struct {
		Source        string
		Stable        bool
		Invoker       bool
		SQL           bool
		ConfigMatches bool
	}
	result := tx.Raw(`SELECT p.prosrc AS source,p.provolatile='s' AS stable,NOT p.prosecdef AS invoker,l.lanname='sql' AS sql,COALESCE(p.proconfig=ARRAY['search_path=pg_catalog, public']::text[],false) AS config_matches FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_language l ON l.oid=p.prolang WHERE p.oid=pg_catalog.to_regprocedure('public.platformconfig_dictionary_schema_ready()')`).Scan(&function)
	if result.Error != nil || result.RowsAffected != 1 || function.Source != parts[1] || !function.Stable || !function.Invoker || !function.SQL || !function.ConfigMatches {
		return fmt.Errorf("dictionary schema verifier is missing or changed")
	}
	var ready bool
	result = tx.Raw(`SELECT public.platformconfig_dictionary_schema_ready()`).Scan(&ready)
	if result.Error != nil {
		return fmt.Errorf("inspect dictionary schema: %w", result.Error)
	}
	if result.RowsAffected != 1 || !ready {
		return fmt.Errorf("dictionary schema is missing or incompatible")
	}
	return nil
}
