package adapter

import (
	"encoding/json"
)

// ─── New service response types ──────────────────────────────────────────────

// ThingModelItem represents a single item from the new device-metadata service.
type ThingModelItem struct {
	ID              string          `json:"id"`
	ThingModelID    string          `json:"thing_model_id"`
	TenantID        string          `json:"tenant_id"`
	Type            string          `json:"type"` // PROPERTY / EVENT / COMMAND
	Identifier      string          `json:"identifier"`
	NameI18n        json.RawMessage `json:"name_i18n"` // {"default":"温度","locales":{...}}
	DescriptionI18n json.RawMessage `json:"description_i18n,omitempty"`
	ValueType       json.RawMessage `json:"value_type"` // {kind, constraint}
	Access          json.RawMessage `json:"access"`     // {read, write, observe}
	WebChartConfig  json.RawMessage `json:"web_chart_config,omitempty"`
	AppChartConfig  json.RawMessage `json:"app_chart_config,omitempty"`
	MetaItems       json.RawMessage `json:"meta_items"` // [{key,value,scope}]
	SortOrder       int             `json:"sort_order"`
}

// nameDefault extracts the "default" field from a name_i18n JSON blob.
func nameDefault(raw json.RawMessage) *string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if v, ok := m["default"].(string); ok {
		return &v
	}
	return nil
}

// valueKind extracts the "kind" field from a value_type JSON blob (INT/FLOAT/BOOL/STRING/ENUM).
func valueKind(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if v, ok := m["kind"].(string); ok {
		return v
	}
	return ""
}

// valueUnit extracts unit from value_type.constraint.
func valueUnit(raw json.RawMessage) *string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if u, ok := m["unit"].(string); ok && u != "" {
		return &u
	}
	c, ok := m["constraint"].(map[string]interface{})
	if !ok {
		return nil
	}
	if u, ok := c["unit"].(string); ok && u != "" {
		return &u
	}
	return nil
}

// accessWritable checks access.write field.
func accessWritable(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var m map[string]bool
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	return m["write"]
}

// storePolicy extracts store_policy from meta_items. Returns "history" or "latest_only".
func storePolicy(metaItems json.RawMessage) string {
	if len(metaItems) == 0 {
		return "latest_only"
	}
	var items []struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(metaItems, &items); err != nil {
		return "latest_only"
	}
	for _, it := range items {
		if it.Key == "store_policy" {
			var v string
			if err := json.Unmarshal(it.Value, &v); err == nil {
				return v
			}
		}
	}
	return "latest_only"
}

// deriveV1DataType maps new value kind to old data type string.
func deriveV1DataType(raw json.RawMessage) *string {
	k := valueKind(raw)
	m := map[string]string{
		"INT":    "Number",
		"FLOAT":  "Number",
		"BOOL":   "Boolean",
		"STRING": "String",
		"ENUM":   "String",
	}
	if v, ok := m[k]; ok {
		return &v
	}
	return nil
}

// deriveV1RWFlag maps access flags to v1 read/write flag.
func deriveV1RWFlag(raw json.RawMessage) *string {
	if accessWritable(raw) {
		rw := "RW"
		return &rw
	}
	r := "R"
	return &r
}

// ─── V1 output types (matching internal/model/device_model.http.go structure) ─

// V1Telemetry mirrors the row returned by the old GetDeviceModelListByPage for telemetry.
type V1Telemetry struct {
	ID             string  `json:"id"`
	DataName       *string `json:"data_name"`
	DataIdentifier string  `json:"data_identifier"`
	ReadWriteFlag  *string `json:"read_write_flag"`
	DataType       *string `json:"data_type"`
	Unit           *string `json:"unit"`
	Description    *string `json:"description"`
	Remark         *string `json:"remark"`
}

// V1Attribute mirrors the row for attributes.
type V1Attribute = V1Telemetry

// V1Event mirrors the row for events.
type V1Event struct {
	ID             string  `json:"id"`
	DataName       *string `json:"data_name"`
	DataIdentifier string  `json:"data_identifier"`
	Params         *string `json:"params"`
	Description    *string `json:"description"`
}

// V1Command mirrors the row for commands.
type V1Command = V1Event

// ─── Translation functions ────────────────────────────────────────────────────

// TranslateToV1Telemetry converts new-model PROPERTY items with store_policy=history
// into the legacy telemetry list format.
func TranslateToV1Telemetry(items []*ThingModelItem) []*V1Telemetry {
	var out []*V1Telemetry
	for _, it := range items {
		if it.Type != "PROPERTY" {
			continue
		}
		if storePolicy(it.MetaItems) != "history" {
			continue
		}
		out = append(out, &V1Telemetry{
			ID:             it.ID,
			DataName:       nameDefault(it.NameI18n),
			DataIdentifier: it.Identifier,
			ReadWriteFlag:  deriveV1RWFlag(it.Access),
			DataType:       deriveV1DataType(it.ValueType),
			Unit:           valueUnit(it.ValueType),
		})
	}
	return out
}

// TranslateToV1Attribute converts PROPERTY items with store_policy=latest_only.
func TranslateToV1Attribute(items []*ThingModelItem) []*V1Attribute {
	var out []*V1Attribute
	for _, it := range items {
		if it.Type != "PROPERTY" {
			continue
		}
		if storePolicy(it.MetaItems) == "history" {
			continue
		}
		out = append(out, &V1Attribute{
			ID:             it.ID,
			DataName:       nameDefault(it.NameI18n),
			DataIdentifier: it.Identifier,
			ReadWriteFlag:  deriveV1RWFlag(it.Access),
			DataType:       deriveV1DataType(it.ValueType),
			Unit:           valueUnit(it.ValueType),
		})
	}
	return out
}

// TranslateToV1Event converts EVENT items.
func TranslateToV1Event(items []*ThingModelItem) []*V1Event {
	var out []*V1Event
	for _, it := range items {
		if it.Type != "EVENT" {
			continue
		}
		out = append(out, &V1Event{
			ID:             it.ID,
			DataName:       nameDefault(it.NameI18n),
			DataIdentifier: it.Identifier,
		})
	}
	return out
}

// TranslateToV1Command converts COMMAND items.
func TranslateToV1Command(items []*ThingModelItem) []*V1Command {
	var out []*V1Command
	for _, it := range items {
		if it.Type != "COMMAND" {
			continue
		}
		out = append(out, &V1Command{
			ID:             it.ID,
			DataName:       nameDefault(it.NameI18n),
			DataIdentifier: it.Identifier,
		})
	}
	return out
}
