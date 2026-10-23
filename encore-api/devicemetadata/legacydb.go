package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// legacyPool is a lazy connection pool to the ThingsPanel main database.
var (
	legacyPool     *pgxpool.Pool
	legacyPoolOnce sync.Once
	legacyPoolErr  error
)

func getLegacyDB() (*pgxpool.Pool, error) {
	legacyPoolOnce.Do(func() {
		dsn := strings.TrimSpace(os.Getenv("TP_LEGACY_DATABASE_URL"))
		if dsn == "" {
			legacyPoolErr = fmt.Errorf("TP_LEGACY_DATABASE_URL not configured; legacy migration not available")
			return
		}
		pool, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			legacyPoolErr = fmt.Errorf("legacy DB connect: %w", err)
			return
		}
		if err := pool.Ping(context.Background()); err != nil {
			pool.Close()
			legacyPoolErr = fmt.Errorf("legacy DB ping: %w", err)
			return
		}
		legacyPool = pool
	})
	return legacyPool, legacyPoolErr
}

// legacyDeviceTemplate is a simplified view of the ThingsPanel v1 device_templates row.
type legacyDeviceTemplate struct {
	ID          string
	Name        string
	TenantID    string
	Description string
	Label       string
	Brand       string
	ModelNumber string
}

// legacyModelItem holds a row from any of the four v1 model sub-tables.
type legacyModelItem struct {
	DataName       string
	DataIdentifier string
	DataType       string  // String | Number | Boolean | Enum
	ReadWriteFlag  string  // R / W / RW
	Unit           string
	Description    string
	Params         string // JSON params for events/commands
	ItemType       string // TELEMETRY | ATTRIBUTE | EVENT | COMMAND
}

// listLegacyTemplates returns all un-migrated device templates for a tenant from the legacy DB.
// "Un-migrated" means the template ID is not present in migration_log.
func listLegacyTemplates(ctx context.Context, tenantID string) ([]legacyDeviceTemplate, error) {
	ldb, err := getLegacyDB()
	if err != nil {
		return nil, err
	}

	rows, err := ldb.Query(ctx, `
		SELECT id, name, tenant_id,
		       COALESCE(description,''), COALESCE(label,''),
		       COALESCE(brand,''), COALESCE(model_number,'')
		FROM device_templates
		WHERE tenant_id = $1
		ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list legacy templates: %w", err)
	}
	defer rows.Close()

	var out []legacyDeviceTemplate
	for rows.Next() {
		var t legacyDeviceTemplate
		if err = rows.Scan(&t.ID, &t.Name, &t.TenantID, &t.Description, &t.Label, &t.Brand, &t.ModelNumber); err != nil {
			return nil, fmt.Errorf("scan legacy template: %w", err)
		}
		out = append(out, t)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy templates: %w", err)
	}
	return out, nil
}

// readLegacyModelItems reads all four model sub-tables for a given device template.
func readLegacyModelItems(ctx context.Context, templateID string) ([]legacyModelItem, error) {
	ldb, err := getLegacyDB()
	if err != nil {
		return nil, err
	}

	var all []legacyModelItem

	// Telemetry
	rows, err := ldb.Query(ctx, `
		SELECT COALESCE(data_name,''), data_identifier,
		       COALESCE(data_type,'String'), COALESCE(read_write_flag,'RW'),
		       COALESCE(unit,''), COALESCE(description,'')
		FROM device_model_telemetry WHERE device_template_id = $1 ORDER BY created_at`, templateID)
	if err != nil {
		return nil, fmt.Errorf("read telemetry: %w", err)
	}
	for rows.Next() {
		var m legacyModelItem
		m.ItemType = "TELEMETRY"
		if err = rows.Scan(&m.DataName, &m.DataIdentifier, &m.DataType, &m.ReadWriteFlag, &m.Unit, &m.Description); err != nil {
			return nil, err
		}
		all = append(all, m)
	}
	rows.Close()

	// Attributes
	rows, err = ldb.Query(ctx, `
		SELECT COALESCE(data_name,''), data_identifier,
		       COALESCE(data_type,'String'), COALESCE(read_write_flag,'RW'),
		       COALESCE(unit,''), COALESCE(description,'')
		FROM device_model_attributes WHERE device_template_id = $1 ORDER BY created_at`, templateID)
	if err != nil {
		return nil, fmt.Errorf("read attributes: %w", err)
	}
	for rows.Next() {
		var m legacyModelItem
		m.ItemType = "ATTRIBUTE"
		if err = rows.Scan(&m.DataName, &m.DataIdentifier, &m.DataType, &m.ReadWriteFlag, &m.Unit, &m.Description); err != nil {
			return nil, err
		}
		all = append(all, m)
	}
	rows.Close()

	// Events — params may be jsonb; cast to text before COALESCE to avoid "invalid input for type json"
	rows, err = ldb.Query(ctx, `
		SELECT COALESCE(data_name,''), data_identifier,
		       '', 'R',
		       '', COALESCE(description,''), COALESCE(params::text,'')
		FROM device_model_events WHERE device_template_id = $1 ORDER BY created_at`, templateID)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	for rows.Next() {
		var m legacyModelItem
		m.ItemType = "EVENT"
		if err = rows.Scan(&m.DataName, &m.DataIdentifier, &m.DataType, &m.ReadWriteFlag, &m.Unit, &m.Description, &m.Params); err != nil {
			return nil, err
		}
		all = append(all, m)
	}
	rows.Close()

	// Commands — same jsonb cast
	rows, err = ldb.Query(ctx, `
		SELECT COALESCE(data_name,''), data_identifier,
		       '', 'W',
		       '', COALESCE(description,''), COALESCE(params::text,'')
		FROM device_model_commands WHERE device_template_id = $1 ORDER BY created_at`, templateID)
	if err != nil {
		return nil, fmt.Errorf("read commands: %w", err)
	}
	for rows.Next() {
		var m legacyModelItem
		m.ItemType = "COMMAND"
		if err = rows.Scan(&m.DataName, &m.DataIdentifier, &m.DataType, &m.ReadWriteFlag, &m.Unit, &m.Description, &m.Params); err != nil {
			return nil, err
		}
		all = append(all, m)
	}
	rows.Close()

	return all, nil
}

// convertLegacyItems converts raw legacy rows into ThingModelItems, deduplicating identifiers.
func convertLegacyItems(raw []legacyModelItem) (items []ThingModelItem, renames []IdentifierRename, warnings []string) {
	seen := map[string]bool{}
	for i, m := range raw {
		id := sanitizeIdentifier(m.DataIdentifier)
		if id == "" {
			warnings = append(warnings, fmt.Sprintf("skipped blank identifier (type=%s)", m.ItemType))
			continue
		}

		// Deduplicate
		original := id
		for seen[id] {
			id = fmt.Sprintf("%s_%d", original, i)
		}
		if id != original {
			renames = append(renames, IdentifierRename{OldIdentifier: original, NewIdentifier: id, Reason: "duplicate identifier"})
		}
		seen[id] = true

		nameI18n := json.RawMessage(fmt.Sprintf(`{"zh-CN":%s,"en":%s}`, jsonStr(m.DataName), jsonStr(m.DataName)))

		valueType := buildValueType(m.DataType, m.Unit, m.Params, m.ItemType)
		access := buildAccess(m.ReadWriteFlag, m.ItemType)

		var descI18n json.RawMessage
		if m.Description != "" {
			descI18n = json.RawMessage(fmt.Sprintf(`{"zh-CN":%s,"en":%s}`, jsonStr(m.Description), jsonStr(m.Description)))
		}

		items = append(items, ThingModelItem{
			Type:            m.ItemType,
			Identifier:      id,
			NameI18n:        nameI18n,
			DescriptionI18n: descI18n,
			ValueType:       valueType,
			Access:          access,
			MetaItems:       json.RawMessage(`[]`),
			SortOrder:       i,
		})
	}
	return
}

func buildValueType(dataType, unit, params, itemType string) json.RawMessage {
	dt := mapDataType(dataType)
	if itemType == "EVENT" || itemType == "COMMAND" {
		// For events/commands, params may be a JSON array of parameter specs
		if params != "" && params != "[]" && params != "null" {
			return json.RawMessage(fmt.Sprintf(`{"type":"STRUCT","specs":%s}`, params))
		}
		return json.RawMessage(`{"type":"OBJECT"}`)
	}
	if unit != "" {
		return json.RawMessage(fmt.Sprintf(`{"type":%s,"unit":%s}`, jsonStr(dt), jsonStr(unit)))
	}
	return json.RawMessage(fmt.Sprintf(`{"type":%s}`, jsonStr(dt)))
}

func buildAccess(rwFlag, itemType string) json.RawMessage {
	switch itemType {
	case "COMMAND":
		return json.RawMessage(`{"write":true,"read":false}`)
	case "EVENT":
		return json.RawMessage(`{"write":false,"read":true}`)
	}
	r := strings.Contains(rwFlag, "R")
	w := strings.Contains(rwFlag, "W")
	if !r && !w {
		r = true // default to readable
	}
	return json.RawMessage(fmt.Sprintf(`{"read":%v,"write":%v}`, r, w))
}

func mapDataType(dt string) string {
	switch strings.ToLower(strings.TrimSpace(dt)) {
	case "number", "float", "double", "int", "integer":
		return "NUMBER"
	case "boolean", "bool":
		return "BOOLEAN"
	case "enum":
		return "ENUM"
	default:
		return "STRING"
	}
}

func sanitizeIdentifier(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '.' {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// readLegacyCustomCommands returns custom command controls for a legacy template.
func readLegacyCustomCommands(ctx context.Context, templateID string) ([]CustomCommandControl, error) {
	ldb, err := getLegacyDB()
	if err != nil {
		return nil, err
	}

	rows, err := ldb.Query(ctx, `
		SELECT id, COALESCE(buttom_name,''), COALESCE(data_identifier,''),
		       COALESCE(description,''), COALESCE(instruct,''), COALESCE(enable_status,'enable')
		FROM device_model_custom_commands WHERE device_template_id = $1 ORDER BY id`, templateID)
	if err != nil {
		return nil, fmt.Errorf("read custom commands: %w", err)
	}
	defer rows.Close()

	var out []CustomCommandControl
	for rows.Next() {
		var c CustomCommandControl
		var legacyID, dataIdentifier string
		if err = rows.Scan(&legacyID, &c.Name, &dataIdentifier, &c.Description, &c.Content, &c.EnableStatus); err != nil {
			return nil, err
		}
		if c.Name == "" {
			c.Name = dataIdentifier
		}
		c.ControlType = "command"
		c.CreatedAt = time.Now()
		c.UpdatedAt = time.Now()
		out = append(out, c)
	}
	return out, nil
}
