package initialize

import (
	"fmt"
	"gorm.io/gorm"
	componentsql "project/sql"
	"strings"
)

const deviceIdentityMigrationModule = "device-identity"

func ApplyDeviceIdentitySchema(db *gorm.DB) error {
	files, err := componentsql.DeviceIdentityMigrations()
	if err != nil {
		return err
	}
	return applyComponentMigrations(db, componentMigration{
		module: deviceIdentityMigrationModule, description: "permanent device identity",
		lockKey: int64(0x4445564943454944), files: files, verify: verifyDeviceIdentitySchema,
	})
}

func verifyDeviceIdentitySchema(tx *gorm.DB) error {
	for name, expected := range map[string]expectedNativeColumn{
		"id":         {dataType: "text", nullable: "NO"},
		"tenant_id":  {dataType: "text", nullable: "NO"},
		"state":      {dataType: "character varying", length: 16, nullable: "NO"},
		"created_at": {dataType: "timestamp with time zone", nullable: "NO"},
		"deleted_at": {dataType: "timestamp with time zone", nullable: "YES"},
	} {
		if err := verifyNativeColumn(tx, "device_id_reservations", name, expected); err != nil {
			return err
		}
	}
	if err := verifyNativeConstraint(tx, "device_id_reservations", "device_id_reservations_pkey", "PRIMARY KEY (id)", "p"); err != nil {
		return err
	}
	for name, definition := range map[string]string{
		"device_id_reservations_state_check":     `CHECK (((state)::text = ANY ((ARRAY['active'::character varying, 'tombstoned'::character varying])::text[])))`,
		"device_id_reservations_lifecycle_check": `CHECK (((((state)::text = 'active'::text) AND (deleted_at IS NULL)) OR (((state)::text = 'tombstoned'::text) AND (deleted_at IS NOT NULL))))`,
	} {
		if err := verifyNativeConstraint(tx, "device_id_reservations", name, definition, "c"); err != nil {
			return err
		}
	}
	var conflicts int64
	if err := tx.Raw(`SELECT count(*) FROM devices d LEFT JOIN device_id_reservations r ON r.id=d.id
		WHERE r.id IS NULL OR r.tenant_id<>d.tenant_id OR r.state<>'active'`).Scan(&conflicts).Error; err != nil {
		return err
	}
	if conflicts != 0 {
		return fmt.Errorf("device identity reservations do not match %d current devices", conflicts)
	}
	var trigger struct {
		Enabled    string
		Type       int
		Definition string
		Body       string
		Config     string
	}
	result := tx.Raw(`SELECT t.tgenabled::text AS enabled,t.tgtype::int AS type,
		pg_get_triggerdef(t.oid) AS definition,p.prosrc AS body,array_to_string(p.proconfig,';') AS config
		FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_proc p ON p.oid=t.tgfoid JOIN pg_namespace pn ON pn.oid=p.pronamespace
		WHERE n.nspname='public' AND c.relname='devices' AND t.tgname='devices_identity_fence'
		AND pn.nspname='public' AND p.proname='enforce_device_identity_fence' AND NOT p.prosecdef`).Scan(&trigger)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 || trigger.Enabled != "O" || trigger.Type != 31 || trigger.Config != "search_path=public, pg_temp" ||
		trigger.Definition != "CREATE TRIGGER devices_identity_fence BEFORE INSERT OR DELETE OR UPDATE OF id, tenant_id ON public.devices FOR EACH ROW EXECUTE FUNCTION enforce_device_identity_fence()" {
		return fmt.Errorf("device identity writer fence is missing or has changed")
	}
	files, err := componentsql.DeviceIdentityMigrations()
	if err != nil {
		return err
	}
	parts := strings.SplitN(string(files[0].Contents), "AS $$", 2)
	if len(parts) != 2 {
		return fmt.Errorf("missing identity fence function source")
	}
	body := strings.SplitN(parts[1], "END $$;", 2)
	if len(body) != 2 || strings.TrimSpace(trigger.Body) != strings.TrimSpace(body[0]+"END ") {
		return fmt.Errorf("device identity writer fence function has changed")
	}
	return nil
}
