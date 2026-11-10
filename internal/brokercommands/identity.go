package brokercommands

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrIdentitySchema is an infrastructure prerequisite, not a missing device.
// Command callers must install develop's permanent identity migration 31.
var ErrIdentitySchema = errors.New("brokercommands: active device identity schema is required")

// lockCommandIdentities runs only in the command owner's transaction. Device
// SHARE locks precede reservation SHARE locks, matching device deletion's
// device-then-tombstone order. FOR KEY SHARE would not block a state-only
// tombstone update, so reservations deliberately use the stronger SHARE mode.
func lockCommandIdentities(tx *gorm.DB, tenant, logical string) error {
	ids := make([]string, 0, 8)
	seen := map[string]bool{}
	id := logical
	for depth := 0; depth < 8; depth++ {
		if seen[id] {
			return fmt.Errorf("%w: device parent cycle", ErrRejected)
		}
		seen[id] = true
		var device deviceRow
		e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND is_enabled='enabled'", id, tenant).Take(&device).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: command identity path is unavailable", ErrRejected)
		}
		if e != nil {
			return e
		}
		ids = append(ids, id)
		if device.ParentID == nil || *device.ParentID == "" {
			return lockReservations(tx, tenant, ids)
		}
		id = *device.ParentID
	}
	return fmt.Errorf("%w: device ancestry exceeds eight levels", ErrRejected)
}

func lockReservations(tx *gorm.DB, tenant string, ids []string) error {
	var schema string
	if e := tx.Raw("SELECT current_schema()").Scan(&schema).Error; e != nil {
		return e
	}
	if schema == "" || strings.HasPrefix(schema, "pg_temp_") || strings.HasPrefix(schema, "pg_toast_temp_") {
		return ErrIdentitySchema
	}
	// Qualify the application's schema explicitly so an implicit temporary
	// same-name table cannot supply fake active reservations on a pooled session.
	table := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"."device_id_reservations"`
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	for _, id := range ordered {
		var reservation struct {
			ID, TenantID, State string
			DeletedAt           *time.Time
		}
		r := tx.Table(table).Select("id,tenant_id,state,deleted_at").
			Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", id).Scan(&reservation)
		if r.Error != nil {
			var state interface{ SQLState() string }
			if errors.As(r.Error, &state) && (state.SQLState() == "42P01" || state.SQLState() == "42703") {
				return fmt.Errorf("%w: %v", ErrIdentitySchema, r.Error)
			}
			return r.Error
		}
		if r.RowsAffected != 1 || reservation.TenantID != tenant || reservation.State != "active" || reservation.DeletedAt != nil {
			return fmt.Errorf("%w: device identity reservation is unavailable", ErrRejected)
		}
	}
	return nil
}
