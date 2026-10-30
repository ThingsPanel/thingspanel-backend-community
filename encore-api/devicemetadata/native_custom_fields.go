package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"encore.app/devicemetadata/accessconfig"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// mergeNativeCustomFieldChanges changes only customFieldDefinitions and keeps
// every untouched configuration/definition value as its original JSON token.
func mergeNativeCustomFieldChanges(configuration json.RawMessage, changes NativeCustomFieldChanges) (json.RawMessage, []accessconfig.CustomFieldDefinition, []accessconfig.CustomFieldDefinition, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(configuration, &root) != nil || root == nil {
		return nil, nil, nil, &errs.Error{Code: errs.Unavailable, Message: "stored template configuration is invalid"}
	}
	definitionsRaw := root["customFieldDefinitions"]
	var oldDefinitions []accessconfig.CustomFieldDefinition
	var oldObjects []json.RawMessage
	if json.Unmarshal(definitionsRaw, &oldDefinitions) != nil || oldDefinitions == nil || json.Unmarshal(definitionsRaw, &oldObjects) != nil {
		return nil, nil, nil, &errs.Error{Code: errs.Unavailable, Message: "stored custom-field definitions are invalid"}
	}
	upserts := make(map[string]NativeCustomFieldUpsert, len(changes.Upsert))
	for _, item := range changes.Upsert {
		upserts[item.Key] = item
	}
	removed := make(map[string]bool, len(changes.Remove))
	for _, key := range changes.Remove {
		removed[key] = true
	}
	merged := make([]json.RawMessage, 0, len(oldObjects)+len(changes.Upsert))
	seen := make(map[string]bool, len(oldObjects)+len(changes.Upsert))
	for i, old := range oldDefinitions {
		if removed[old.Key] {
			continue
		}
		object := oldObjects[i]
		if patch, ok := upserts[old.Key]; ok {
			var rawObject map[string]json.RawMessage
			if json.Unmarshal(object, &rawObject) != nil || rawObject == nil {
				return nil, nil, nil, &errs.Error{Code: errs.Unavailable, Message: "stored custom-field definition is invalid"}
			}
			for name, value := range patch.Fields {
				rawObject[name] = append(json.RawMessage(nil), value...)
			}
			var err error
			object, err = json.Marshal(rawObject)
			if err != nil {
				return nil, nil, nil, err
			}
			seen[old.Key] = true
		}
		merged = append(merged, object)
	}
	for _, patch := range changes.Upsert {
		if seen[patch.Key] {
			continue
		}
		object := make(map[string]json.RawMessage, len(patch.Fields)+1)
		key, _ := json.Marshal(patch.Key)
		object["key"] = key
		for name, value := range patch.Fields {
			object[name] = append(json.RawMessage(nil), value...)
		}
		encoded, err := json.Marshal(object)
		if err != nil {
			return nil, nil, nil, err
		}
		merged = append(merged, encoded)
	}
	encodedDefinitions, err := json.Marshal(merged)
	if err != nil {
		return nil, nil, nil, err
	}
	root["customFieldDefinitions"] = encodedDefinitions
	encodedConfiguration, err := json.Marshal(root)
	if err != nil {
		return nil, nil, nil, err
	}
	var nextDefinitions []accessconfig.CustomFieldDefinition
	if json.Unmarshal(encodedDefinitions, &nextDefinitions) != nil || nextDefinitions == nil {
		return nil, nil, nil, invalid("customFieldChanges", "resulting definitions are invalid")
	}
	return encodedConfiguration, oldDefinitions, nextDefinitions, nil
}

func marshalNativeCustomFieldChanges(changes NativeCustomFieldChanges) (json.RawMessage, error) {
	upserts := make([]json.RawMessage, 0, len(changes.Upsert))
	for _, patch := range changes.Upsert {
		object := make(map[string]json.RawMessage, len(patch.Fields)+1)
		key, err := json.Marshal(patch.Key)
		if err != nil {
			return nil, err
		}
		object["key"] = key
		for name, value := range patch.Fields {
			object[name] = value
		}
		encoded, err := json.Marshal(object)
		if err != nil {
			return nil, err
		}
		upserts = append(upserts, encoded)
	}
	return json.Marshal(map[string]any{"upsert": upserts, "remove": changes.Remove})
}

// validateNativeCustomFieldChangesAgainstDevices rejects only destructive
// changes that make stored live-device values invalid. It does not lock device
// rows; writers serialize on the template FOR SHARE lock.
func validateNativeCustomFieldChangesAgainstDevices(ctx context.Context, tx *sqldb.Tx, tenantID, templateID string, oldDefinitions, nextDefinitions []accessconfig.CustomFieldDefinition) error {
	oldByKey := make(map[string]accessconfig.CustomFieldDefinition, len(oldDefinitions))
	nextByKey := make(map[string]accessconfig.CustomFieldDefinition, len(nextDefinitions))
	for _, definition := range oldDefinitions {
		oldByKey[definition.Key] = definition
	}
	for _, definition := range nextDefinitions {
		nextByKey[definition.Key] = definition
	}
	remove := make(map[string]bool)
	for key := range oldByKey {
		if _, exists := nextByKey[key]; !exists {
			remove[key] = true
		}
	}
	changed := make(map[string]bool)
	for key, old := range oldByKey {
		next, exists := nextByKey[key]
		if !exists {
			continue
		}
		// Name/description/default edits and disabling do not invalidate saved
		// values. A type/enum change or re-enable must validate existing values.
		if old.Type != next.Type || !enumValuesEqual(old.EnumValues, next.EnumValues) || (!old.Enabled && next.Enabled) {
			changed[key] = true
		}
	}
	if len(remove) == 0 && len(changed) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT d.custom_fields FROM devices d JOIN device_access_bindings b
		ON b.tenant_id=d.tenant_id AND b.device_id=d.id
		WHERE d.tenant_id=$1 AND b.template_id=$2 AND d.deleted_at IS NULL`, tenantID, templateID)
	if err != nil {
		return err
	}
	defer rows.Close()
	conflicts := make(map[string]int)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return &errs.Error{Code: errs.Unavailable, Message: "stored device custom fields are invalid"}
		}
		for key := range remove {
			if _, exists := values[key]; exists {
				conflicts[key]++
			}
		}
		for key := range changed {
			value, exists := values[key]
			if !exists {
				continue
			}
			definition := nextByKey[key]
			if !nativeCustomFieldValueMatches(definition, value) {
				conflicts[key]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(conflicts) == 0 {
		return nil
	}
	return nativeCustomFieldConflict(conflicts)
}

func enumValuesEqual(left, right []accessconfig.EnumValue) bool {
	if len(left) != len(right) {
		return false
	}
	values := make(map[string]bool, len(left))
	for _, item := range left {
		values[item.Value] = true
	}
	for _, item := range right {
		if !values[item.Value] {
			return false
		}
	}
	return true
}

func nativeCustomFieldValueMatches(definition accessconfig.CustomFieldDefinition, raw json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	var scalar any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&scalar) != nil {
		return false
	}
	switch definition.Type {
	case "string":
		_, ok := scalar.(string)
		return ok
	case "number":
		_, ok := scalar.(json.Number)
		return ok
	case "boolean":
		_, ok := scalar.(bool)
		return ok
	case "enum":
		text, ok := scalar.(string)
		if !ok {
			return false
		}
		for _, option := range definition.EnumValues {
			if option.Value == text {
				return true
			}
		}
	}
	return false
}

func nativeCustomFieldConflict(conflicts map[string]int) error {
	keys := make([]string, 0, len(conflicts))
	for key := range conflicts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s (%d devices)", key, conflicts[key]))
	}
	return &errs.Error{Code: errs.FailedPrecondition, Message: "custom field changes conflict with existing device values: " + strings.Join(parts, ", ")}
}
