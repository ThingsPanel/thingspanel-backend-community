package storage

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// sqlFlavor holds the few spellings that differ between PostgreSQL and MySQL in the intent SQL.
type sqlFlavor struct{ mysql bool }

func flavorOf(db *gorm.DB) sqlFlavor { return sqlFlavor{mysql: db.Dialector.Name() == "mysql"} }

// key quotes the column named key, a reserved word in MySQL.
func (f sqlFlavor) key() string {
	if f.mysql {
		return "`key`"
	}
	return `"key"`
}

func (f sqlFlavor) now() string {
	if f.mysql {
		return "NOW(6)"
	}
	return "NOW()"
}

func (f sqlFlavor) forUpdate(alias string) string {
	if f.mysql {
		return "FOR UPDATE SKIP LOCKED"
	}
	return "FOR UPDATE OF " + alias + " SKIP LOCKED"
}

// leaseExpr returns the expression and argument for "now plus a lease of whole seconds".
func (f sqlFlavor) leaseExpr(seconds float64) (string, any) {
	if f.mysql {
		return "NOW(6) + INTERVAL ? SECOND", int64(seconds)
	}
	return "NOW() + make_interval(secs => ?)", seconds
}

const (
	intentTenantMax = 64
	intentDeviceMax = 64
	intentKeyMax    = 255
)

// insertIntentsMySQL stores intents with INSERT IGNORE and returns the identities this call created.
//
// INSERT IGNORE cannot report which rows it skipped, and a DO NOTHING upsert reports one affected row
// for an existing row on clientFoundRows connections. Every row therefore carries a token unique to
// the call, and the call reads back the rows holding its token. IGNORE would also turn an over-long
// value into a silently truncated one, so lengths are checked first.
func insertIntentsMySQL(tx *gorm.DB, generation int64, points []TelemetryData) (map[intentIdentity]bool, error) {
	for _, p := range points {
		switch {
		case p.TenantID == "":
			return nil, errors.New("history intent requires a tenant")
		case utf8.RuneCountInString(p.TenantID) > intentTenantMax, utf8.RuneCountInString(p.DeviceID) > intentDeviceMax, utf8.RuneCountInString(p.Key) > intentKeyMax:
			return nil, errors.New("history intent tenant, device or key is too long for the MySQL schema")
		}
	}
	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	const chunk = 500
	for offset := 0; offset < len(points); offset += chunk {
		end := offset + chunk
		if end > len(points) {
			end = len(points)
		}
		values := make([]string, 0, end-offset)
		args := make([]any, 0, (end-offset)*9)
		for _, p := range points[offset:end] {
			values = append(values, "(?,?,?,?,?,?,?,?,?)")
			args = append(args, p.TenantID, p.DeviceID, p.Key, p.TS, p.BoolV, p.NumberV, p.StringV, generation, token)
		}
		if err := tx.Exec("INSERT IGNORE INTO telemetry_history_intents (tenant_id, device_id, `key`, ts, bool_v, number_v, string_v, generation, writer_token) VALUES "+strings.Join(values, ","), args...).Error; err != nil {
			return nil, err
		}
	}
	var created []struct {
		DeviceID string `gorm:"column:device_id"`
		Key      string `gorm:"column:key"`
		TS       int64  `gorm:"column:ts"`
	}
	if err := tx.Raw("SELECT device_id, `key`, ts FROM telemetry_history_intents WHERE writer_token = ?", token).Scan(&created).Error; err != nil {
		return nil, err
	}
	out := make(map[intentIdentity]bool, len(created))
	for _, row := range created {
		out[intentIdentity{row.DeviceID, row.Key, row.TS}] = true
	}
	return out, nil
}

// upsertCurrentMySQL moves the snapshot forward only for rows whose timestamp is not older than the
// stored one (the PostgreSQL form is ON CONFLICT ... WHERE ts <= excluded.ts). The timestamp is
// assigned last because MySQL evaluates the assignments left to right against the updated row.
func upsertCurrentMySQL(tx *gorm.DB, rows []TelemetryCurrentData) error {
	const chunk = 500
	for offset := 0; offset < len(rows); offset += chunk {
		end := offset + chunk
		if end > len(rows) {
			end = len(rows)
		}
		values := make([]string, 0, end-offset)
		args := make([]any, 0, (end-offset)*7)
		for _, r := range rows[offset:end] {
			values = append(values, "(?,?,?,?,?,?,?)")
			args = append(args, r.DeviceID, r.Key, r.TS.UTC(), r.BoolV, r.NumberV, r.StringV, r.TenantID)
		}
		keep := func(column string) string {
			return fmt.Sprintf("%s=IF(new.ts>=telemetry_current_datas.ts,new.%s,telemetry_current_datas.%s)", column, column, column)
		}
		statement := "INSERT INTO telemetry_current_datas (device_id, `key`, ts, bool_v, number_v, string_v, tenant_id) VALUES " + strings.Join(values, ",") +
			" AS new ON DUPLICATE KEY UPDATE " + strings.Join([]string{keep("bool_v"), keep("number_v"), keep("string_v"), keep("tenant_id"), keep("ts")}, ",")
		if err := tx.Exec(statement, args...).Error; err != nil {
			return err
		}
	}
	return nil
}
