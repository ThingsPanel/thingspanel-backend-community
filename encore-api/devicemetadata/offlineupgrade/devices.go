package offlineupgrade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"encore.app/internal/tenantidentity"
	"github.com/google/uuid"
)

// DeviceIdentityInput stages identity and ordinary metadata only. Credentials,
// templates, model selection and runtime bindings require separate conversion;
// this stage cannot produce deployment acceptance or start a runtime.
type DeviceIdentityInput struct {
	SourceDatabase            string            `json:"source_database"`
	TargetDatabase            string            `json:"target_database"`
	SourceArchiveSHA256       string            `json:"source_archive_sha256"`
	TargetArchiveSHA256       string            `json:"target_archive_sha256"`
	QuarantineSHA256          string            `json:"quarantine_sha256"`
	Devices                   []json.RawMessage `json:"devices"`
	Templates                 []json.RawMessage `json:"templates"`
	ExcludedDevices           []string          `json:"excluded_devices"`
	ExcludedGatewayRelations  []DeviceRelation  `json:"excluded_gateway_relations"`
	ExcludedTemplateRelations []DeviceRelation  `json:"excluded_template_relations"`
	NameFromNumber            []string          `json:"name_from_number"`
}

type DeviceRelation struct {
	DeviceID string `json:"device_id"`
	TargetID string `json:"target_id"`
}

type sourceDevice struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	Name             *string         `json:"name"`
	DeviceNumber     string          `json:"device_number"`
	ConfigID         *string         `json:"device_config_id"`
	ParentID         *string         `json:"parent_id"`
	ProductID        *string         `json:"product_id"`
	SubdeviceAddress *string         `json:"sub_device_addr"`
	IsEnabled        string          `json:"is_enabled"`
	ActivateFlag     string          `json:"activate_flag"`
	ActivateAt       *time.Time      `json:"activate_at"`
	CreatedAt        *time.Time      `json:"created_at"`
	UpdatedAt        *time.Time      `json:"update_at"`
	Description      *string         `json:"description"`
	BatchNumber      *string         `json:"batch_number"`
	Label            *string         `json:"label"`
	Location         *string         `json:"location"`
	AdditionalInfo   json.RawMessage `json:"additional_info"`
}

type sourceTemplateKind struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	DeviceType string `json:"device_type"`
}

type DeviceIdentity struct {
	ID                   string
	TenantID             string
	Name                 string
	DeviceNumber         string
	DeviceKind           string
	Description          *string
	Labels               string  `gorm:"type:jsonb"`
	Tags                 string  `gorm:"type:jsonb"`
	CustomFields         string  `gorm:"type:jsonb"`
	Location             *string `gorm:"type:jsonb"`
	BatchNumber          *string
	AdministrativeState  string
	ActivationState      string
	ActivationTimeSource string
	ActivatedAt          *time.Time
	CreatedBy            string
	UpdatedBy            string
	ResourceVersion      int64
	CreatedAt            time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime:false"`
	DeletedAt            *time.Time
}

type PlannedDevice struct {
	Row             DeviceIdentity
	SourceID        string
	SourceRowSHA256 string
	NameFromNumber  bool
}

type DeviceIdentityPlan struct {
	Input   DeviceIdentityInput
	Devices []PlannedDevice
	Actor   string
}

func PlanDeviceIdentities(input DeviceIdentityInput) (DeviceIdentityPlan, error) {
	boundary, err := PlanGroups(GroupInput{SourceDatabase: input.SourceDatabase, TargetDatabase: input.TargetDatabase,
		SourceArchiveSHA256: input.SourceArchiveSHA256, TargetArchiveSHA256: input.TargetArchiveSHA256, QuarantineSHA256: input.QuarantineSHA256})
	p := DeviceIdentityPlan{Input: input, Actor: boundary.Actor}
	if err != nil {
		return p, err
	}
	return planDeviceIdentityRows(input)
}

func planDeviceIdentityRows(input DeviceIdentityInput) (DeviceIdentityPlan, error) {
	p := DeviceIdentityPlan{Input: input, Actor: "migration:" + input.SourceArchiveSHA256}
	excluded, err := uniqueIDs(input.ExcludedDevices)
	if err != nil {
		return p, err
	}
	names, err := uniqueIDs(input.NameFromNumber)
	if err != nil {
		return p, err
	}
	gateways, err := uniqueRelations(input.ExcludedGatewayRelations)
	if err != nil {
		return p, err
	}
	bindings, err := uniqueRelations(input.ExcludedTemplateRelations)
	if err != nil {
		return p, err
	}
	rows := map[string]sourceDevice{}
	hashes := map[string]string{}
	templates := map[string]sourceTemplateKind{}
	for _, raw := range input.Templates {
		var t sourceTemplateKind
		if json.Unmarshal(raw, &t) != nil || t.ID == "" {
			return p, fmt.Errorf("invalid source template identity")
		}
		if _, ok := templates[t.ID]; ok {
			return p, fmt.Errorf("duplicate source template identity")
		}
		templates[t.ID] = t
	}
	for _, raw := range input.Devices {
		var d sourceDevice
		if json.Unmarshal(raw, &d) != nil || d.ID == "" {
			return p, fmt.Errorf("invalid source device row")
		}
		if _, ok := rows[d.ID]; ok {
			return p, fmt.Errorf("duplicate source device identity")
		}
		rows[d.ID] = d
		sum := sha256.Sum256(raw)
		hashes[d.ID] = hex.EncodeToString(sum[:])
		if excluded[d.ID] && strings.TrimSpace(d.TenantID) != "" {
			return p, fmt.Errorf("quarantined device ownership changed")
		}
	}
	for id := range excluded {
		if _, ok := rows[id]; !ok {
			return p, fmt.Errorf("quarantined device absent")
		}
	}
	for id := range names {
		d, ok := rows[id]
		if !ok || excluded[id] || strings.TrimSpace(text(d.Name)) != "" {
			return p, fmt.Errorf("approved unnamed device changed")
		}
	}
	for id, target := range gateways {
		d, ok := rows[id]
		if !ok || text(d.ParentID) != target {
			return p, fmt.Errorf("approved gateway relationship changed")
		}
	}
	for id, target := range bindings {
		d, ok := rows[id]
		if !ok || text(d.ConfigID) != target {
			return p, fmt.Errorf("approved template relationship changed")
		}
	}
	parents := map[string]bool{}
	for id, d := range rows {
		if excluded[id] || text(d.ParentID) == "" || gateways[id] != "" {
			continue
		}
		parent, ok := rows[text(d.ParentID)]
		if !ok || excluded[parent.ID] || d.TenantID != parent.TenantID || d.ID == parent.ID {
			return p, fmt.Errorf("unresolved gateway relationship for device %s", id)
		}
		parents[parent.ID] = true
	}
	nativeIDs, numbers := map[string]bool{}, map[string]bool{}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := rows[id]
		if excluded[id] {
			continue
		}
		if d.TenantID == "" || d.TenantID != strings.TrimSpace(d.TenantID) {
			return p, fmt.Errorf("device %s has ambiguous tenant", id)
		}
		nativeID := tenantidentity.CanonicalIdentity("device", id)
		tenant := tenantidentity.CanonicalIdentity("tenant", d.TenantID)
		if nativeID == uuid.Nil.String() || tenant == uuid.Nil.String() || nativeIDs[nativeID] {
			return p, fmt.Errorf("invalid native device identity")
		}
		nativeIDs[nativeID] = true
		if !validDeviceNumber(d.DeviceNumber) {
			return p, fmt.Errorf("device %s has invalid number", id)
		}
		numberKey := tenant + "/" + d.DeviceNumber
		if numbers[numberKey] {
			return p, fmt.Errorf("duplicate tenant device number")
		}
		numbers[numberKey] = true
		name := text(d.Name)
		if names[id] {
			name = d.DeviceNumber
		}
		if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 128 {
			return p, fmt.Errorf("device %s requires reviewed display name", id)
		}
		if d.CreatedAt == nil || d.UpdatedAt == nil {
			return p, fmt.Errorf("device %s lacks source timestamps", id)
		}
		if text(d.ProductID) != "" {
			return p, fmt.Errorf("device product conversion required")
		}
		if utf8.RuneCountInString(text(d.Description)) > 2000 || utf8.RuneCountInString(text(d.BatchNumber)) > 128 {
			return p, fmt.Errorf("device %s metadata exceeds native limits", id)
		}
		kind := ""
		if config := text(d.ConfigID); config != "" && bindings[id] == "" {
			t, ok := templates[config]
			if !ok || t.TenantID != d.TenantID {
				return p, fmt.Errorf("unresolved template ownership for device %s", id)
			}
			kind = map[string]string{"1": "standalone", "2": "gateway", "3": "subdevice"}[t.DeviceType]
			if kind == "" {
				return p, fmt.Errorf("unknown template device kind for device %s", id)
			}
		}
		if parents[id] {
			if kind != "" && kind != "gateway" {
				return p, fmt.Errorf("device %s template contradicts gateway hierarchy", id)
			}
			kind = "gateway"
		}
		if text(d.ParentID) != "" && gateways[id] == "" {
			if kind == "standalone" {
				return p, fmt.Errorf("device %s template contradicts parent relation", id)
			}
			if kind == "" {
				kind = "subdevice"
			}
		}
		// Source GetVoucherForm defines no-template, unrelated devices as kind 1.
		if kind == "" {
			kind = "standalone"
		}
		state := "disabled"
		switch d.IsEnabled {
		case "enabled":
			state = "enabled"
		case "", "disabled", "enable":
		default:
			return p, fmt.Errorf("unknown device administrative state")
		}
		activationSource := ""
		activatedAt := d.ActivateAt
		switch d.ActivateFlag {
		case "active":
			activationSource = "source_record"
			if activatedAt == nil {
				activationSource = "source_unknown"
			}
		case "inactive":
			if activatedAt != nil {
				return p, fmt.Errorf("inactive device has activation timestamp")
			}
			activationSource = "not_activated"
		default:
			return p, fmt.Errorf("unknown device activation state")
		}
		tags, err := legacyTags(text(d.Label))
		if err != nil {
			return p, fmt.Errorf("device %s: %w", id, err)
		}
		location, err := legacyLocation(text(d.Location))
		if err != nil {
			return p, fmt.Errorf("device %s: %w", id, err)
		}
		fields, err := legacyCustomFields(d.AdditionalInfo)
		if err != nil {
			return p, fmt.Errorf("device %s: %w", id, err)
		}
		p.Devices = append(p.Devices, PlannedDevice{SourceID: id, SourceRowSHA256: hashes[id], NameFromNumber: names[id], Row: DeviceIdentity{
			ID: nativeID, TenantID: tenant, Name: name, DeviceNumber: d.DeviceNumber, DeviceKind: kind, Description: d.Description,
			Labels: "{}", Tags: tags, CustomFields: fields, Location: location, BatchNumber: d.BatchNumber,
			AdministrativeState: state, ActivationState: d.ActivateFlag, ActivationTimeSource: activationSource, ActivatedAt: activatedAt,
			CreatedBy: p.Actor, UpdatedBy: p.Actor, ResourceVersion: 1, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
		}})
	}
	sort.Slice(p.Devices, func(i, j int) bool { return p.Devices[i].Row.ID < p.Devices[j].Row.ID })
	return p, nil
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func uniqueIDs(ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if id == "" || out[id] {
			return nil, fmt.Errorf("invalid duplicate resolution identity")
		}
		out[id] = true
	}
	return out, nil
}
func uniqueRelations(rows []DeviceRelation) (map[string]string, error) {
	out := map[string]string{}
	for _, r := range rows {
		if r.DeviceID == "" || r.TargetID == "" || out[r.DeviceID] != "" {
			return nil, fmt.Errorf("invalid duplicate relationship resolution")
		}
		out[r.DeviceID] = r.TargetID
	}
	return out, nil
}
func validDeviceNumber(v string) bool {
	if v == "" || strings.TrimSpace(v) != v || utf8.RuneCountInString(v) > 128 {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func legacyTags(value string) (string, error) {
	tags := []string{}
	seen := map[string]bool{}
	if value != "" {
		for _, tag := range strings.Split(value, ",") {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if utf8.RuneCountInString(tag) > 128 {
				return "", fmt.Errorf("legacy tag exceeds native limit")
			}
			if !seen[tag] {
				tags = append(tags, tag)
				seen[tag] = true
			}
		}
	}
	if len(tags) > 100 {
		return "", fmt.Errorf("too many legacy tags")
	}
	data, _ := json.Marshal(tags)
	return string(data), nil
}

func legacyLocation(value string) (*string, error) {
	if strings.TrimSpace(value) == "" || value == "," {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid source location")
	}
	lon, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lat, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if e1 != nil || e2 != nil || math.IsNaN(lon) || math.IsNaN(lat) || lon < -180 || lon > 180 || lat < -90 || lat > 90 {
		return nil, fmt.Errorf("invalid source coordinates")
	}
	data, err := json.Marshal(map[string]any{"longitude": lon, "latitude": lat, "address": nil})
	if err != nil {
		return nil, fmt.Errorf("invalid source coordinates")
	}
	out := string(data)
	return &out, nil
}

func legacyCustomFields(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "{}", nil
	}
	// Some legacy transports encode JSON inside a JSON string.
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte(`"`)) {
		var encoded string
		if json.Unmarshal(raw, &encoded) != nil {
			return "", fmt.Errorf("invalid additional metadata")
		}
		raw = []byte(encoded)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return "", fmt.Errorf("additional metadata must be an object")
	}
	fields := map[string]json.RawMessage{}
	put := func(key string, value json.RawMessage) error {
		if key == "" {
			return fmt.Errorf("empty custom field name")
		}
		value = bytes.TrimSpace(value)
		if len(value) == 0 || value[0] == '{' || value[0] == '[' {
			return fmt.Errorf("nested custom field requires explicit conversion")
		}
		if old, exists := fields[key]; exists && !bytes.Equal(old, value) {
			return fmt.Errorf("conflicting custom field values")
		}
		fields[key] = value
		return nil
	}
	for key, value := range object {
		if key == "mqtt_config" {
			continue
		} // owned by the later access/runtime conversion
		if key == "extendedInfo" {
			var entries []struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			}
			if json.Unmarshal(value, &entries) == nil {
				for _, e := range entries {
					if err := put(e.Name, e.Value); err != nil {
						return "", err
					}
				}
				continue
			}
			var nested map[string]json.RawMessage
			if json.Unmarshal(value, &nested) != nil || nested == nil {
				return "", fmt.Errorf("invalid extended metadata")
			}
			for k, v := range nested {
				if err := put(k, v); err != nil {
					return "", err
				}
			}
			continue
		}
		if err := put(key, value); err != nil {
			return "", err
		}
	}
	if len(fields) > 100 {
		return "", fmt.Errorf("too many custom fields")
	}
	data, err := json.Marshal(fields)
	if err != nil || len(data) > 60000 {
		return "", fmt.Errorf("custom metadata exceeds native limits")
	}
	return string(data), nil
}
