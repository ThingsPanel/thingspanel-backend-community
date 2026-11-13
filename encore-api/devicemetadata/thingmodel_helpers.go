package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"encore.app/thingmodel/core"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

const maxSnapshotSizeBytes = 5 * 1024 * 1024

type snapshotContent struct {
	ThingModelID string                `json:"thing_model_id"`
	Name         string                `json:"name"`
	Description  *I18nString           `json:"description,omitempty"`
	Category     string                `json:"category,omitempty"`
	Version      int                   `json:"version"`
	Items        []snapshotContentItem `json:"items"`
}

type snapshotContentItem struct {
	Type            string          `json:"type"`
	Identifier      string          `json:"identifier"`
	NameI18n        json.RawMessage `json:"name_i18n"`
	DescriptionI18n json.RawMessage `json:"description_i18n,omitempty"`
	ValueType       json.RawMessage `json:"value_type"`
	Parameters      json.RawMessage `json:"parameters,omitempty"`
	Access          json.RawMessage `json:"access"`
	WebChartConfig  json.RawMessage `json:"web_chart_config,omitempty"`
	AppChartConfig  json.RawMessage `json:"app_chart_config,omitempty"`
	MetaItems       json.RawMessage `json:"meta_items,omitempty"`
	SortOrder       int             `json:"sort_order"`
}

type diffChangedItem struct {
	Identifier string          `json:"identifier"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
}

type structuredDiffResponse struct {
	FromVersion  int                   `json:"from_version"`
	ToVersion    int                   `json:"to_version"`
	AddedItems   []snapshotContentItem `json:"added_items,omitempty"`
	RemovedItems []snapshotContentItem `json:"removed_items,omitempty"`
	ChangedItems []diffChangedItem     `json:"changed_items,omitempty"`
}

func validateThingModelPayload(req *ThingModel) []ValidationError {
	errs := make([]ValidationError, 0, 4)
	if strings.TrimSpace(req.Name) == "" {
		errs = appendValidationError(errs, "name", newValidationIssue("THING_MODEL_NAME_INVALID", "name is required"), "THING_MODEL_NAME_INVALID")
	}
	if raw := req.DescriptionI18n; len(raw) > 0 {
		desc, err := parseI18n(raw)
		if err != nil {
			errs = appendValidationError(errs, "description_i18n", err, "THING_MODEL_I18N_INVALID")
		} else {
			errs = appendValidationError(errs, "description_i18n", desc.ValidateAsDescription(), "THING_MODEL_I18N_INVALID")
		}
	}
	return errs
}

func validateThingModelItemPayload(req *ThingModelItem) []ValidationError {
	return validateThingModelItemPayloadMode(req, false)
}

func validateThingModelItemPayloadMode(req *ThingModelItem, allowLegacySnapshot bool) []ValidationError {
	errs := make([]ValidationError, 0, 8)
	errs = appendValidationError(errs, "type", validateItemKind(req.Type), "THING_MODEL_TYPE_INVALID")
	errs = appendValidationError(errs, "identifier", ValidateIdentifier(req.Identifier), "THING_MODEL_IDENTIFIER_INVALID")

	name, err := parseI18n(req.NameI18n)
	if err != nil {
		errs = appendValidationError(errs, "name_i18n", err, "THING_MODEL_I18N_INVALID")
	} else {
		errs = appendValidationError(errs, "name_i18n", name.Validate(), "THING_MODEL_I18N_INVALID")
	}

	if desc, err := parseI18n(req.DescriptionI18n); err != nil {
		errs = appendValidationError(errs, "description_i18n", err, "THING_MODEL_I18N_INVALID")
	} else if desc != nil {
		errs = appendValidationError(errs, "description_i18n", desc.ValidateAsDescription(), "THING_MODEL_I18N_INVALID")
	}

	kind := strings.ToUpper(strings.TrimSpace(req.Type))
	legacyItem := kind == "PROPERTY" || (allowLegacySnapshot && (kind == "EVENT" || kind == "COMMAND") && len(req.Parameters) == 0)
	if kind == "PROPERTY" && len(req.Parameters) > 0 && strings.TrimSpace(string(req.Parameters)) != "null" {
		errs = append(errs, ValidationError{Field: "parameters", Code: "FIELD_PARAMETERS_UNEXPECTED", Message: "legacy PROPERTY fields do not have parameters"})
	}
	if legacyItem {
		vt, err := parseValueType(req.ValueType)
		if err != nil {
			errs = appendValidationError(errs, "value_type", err, "THING_MODEL_VALUE_TYPE_INVALID")
		} else {
			errs = appendValidationError(errs, "value_type", vt.Validate(), "THING_MODEL_VALUE_TYPE_INVALID")
		}
	} else {
		var description *core.I18nString
		if parsed, err := parseI18n(req.DescriptionI18n); err == nil && parsed != nil {
			description = &core.I18nString{Default: parsed.Default, Locales: parsed.Locales}
		}
		nameCore := core.I18nString{}
		if parsed, err := parseI18n(req.NameI18n); err == nil && parsed != nil {
			nameCore = core.I18nString{Default: parsed.Default, Locales: parsed.Locales}
		}
		var accessCore core.Access
		if parsed, err := parseAccess(req.Access); err == nil {
			accessCore = core.Access{Read: parsed.Read, Write: parsed.Write, Observe: parsed.Observe}
		}
		field := core.Field{Kind: core.FieldKind(kind), Identifier: req.Identifier, Name: nameCore, Description: description, Access: accessCore}
		if kind == "TELEMETRY" || kind == "ATTRIBUTE" {
			if len(req.Parameters) > 0 && strings.TrimSpace(string(req.Parameters)) != "null" {
				errs = append(errs, ValidationError{Field: "parameters", Code: "FIELD_PARAMETERS_UNEXPECTED", Message: "TELEMETRY and ATTRIBUTE fields do not have parameters"})
			}
			var valueType core.ValueType
			if len(req.ValueType) == 0 || string(req.ValueType) == "null" {
				errs = append(errs, ValidationError{Field: "value_type", Code: "FIELD_VALUE_TYPE_REQUIRED", Message: "scalar field requires value_type"})
			} else if err := json.Unmarshal(req.ValueType, &valueType); err != nil {
				errs = append(errs, ValidationError{Field: "value_type", Code: "THING_MODEL_VALUE_TYPE_INVALID", Message: "invalid value_type payload"})
			} else {
				field.ValueType = &valueType
				if legacyVT, err := parseValueType(req.ValueType); err == nil && legacyVT.Constraint != nil {
					field.Unit = legacyVT.Constraint.Unit
				}
			}
		} else {
			if len(req.ValueType) > 0 && strings.TrimSpace(string(req.ValueType)) != "null" {
				errs = append(errs, ValidationError{Field: "value_type", Code: "FIELD_VALUE_TYPE_UNEXPECTED", Message: "EVENT and COMMAND fields use typed parameters"})
			}
			var parameters core.Parameters
			if len(req.Parameters) > 0 && string(req.Parameters) != "null" {
				if err := json.Unmarshal(req.Parameters, &parameters); err != nil {
					errs = append(errs, ValidationError{Field: "parameters", Code: "PARAMETERS_INVALID", Message: "invalid parameters payload"})
				} else {
					field.Parameters = &parameters
				}
			}
		}
		for _, issue := range core.ValidateField(field) {
			errs = append(errs, ValidationError{Field: issue.Path, Code: issue.Code, Message: issue.Message})
		}
	}

	access, err := parseAccess(req.Access)
	if err != nil {
		errs = appendValidationError(errs, "access", err, "THING_MODEL_ACCESS_INVALID")
	} else {
		errs = appendValidationError(errs, "access", ValidateAccessForType(req.Type, access), "THING_MODEL_ACCESS_INVALID")
	}

	if _, err := parseMetaItems(req.MetaItems); err != nil {
		errs = appendValidationError(errs, "meta_items", err, "THING_MODEL_META_ITEM_INVALID")
	}
	return errs
}

func validateInlineThingModelItem(req validateThingModelItemReq) []ValidationError {
	item := ThingModelItem{
		Type:       req.Type,
		Identifier: req.Identifier,
		NameI18n:   req.NameI18n,
		ValueType:  req.ValueType,
		Parameters: req.Parameters,
		Access:     req.Access,
		MetaItems:  req.MetaItems,
	}
	return validateThingModelItemPayload(&item)
}

func buildSnapshotContent(model *ThingModel, items []ThingModelItem, version int) (*snapshotContent, error) {
	var desc *I18nString
	if len(model.DescriptionI18n) > 0 {
		parsed, err := parseI18n(model.DescriptionI18n)
		if err != nil {
			return nil, err
		}
		desc = parsed
	}
	payload := &snapshotContent{
		ThingModelID: model.ID,
		Name:         model.Name,
		Description:  desc,
		Category:     model.Category,
		Version:      version,
		Items:        make([]snapshotContentItem, 0, len(items)),
	}
	for _, item := range items {
		payload.Items = append(payload.Items, snapshotContentItem{
			Type:            item.Type,
			Identifier:      item.Identifier,
			NameI18n:        item.NameI18n,
			DescriptionI18n: item.DescriptionI18n,
			ValueType:       item.ValueType,
			Parameters:      item.Parameters,
			Access:          item.Access,
			WebChartConfig:  item.WebChartConfig,
			AppChartConfig:  item.AppChartConfig,
			MetaItems:       item.MetaItems,
			SortOrder:       item.SortOrder,
		})
	}
	return payload, nil
}

func validateSnapshotPayloadSize(content json.RawMessage) error {
	if len(content) > maxSnapshotSizeBytes {
		return &errs.Error{Code: errs.InvalidArgument, Message: "snapshot content exceeds 5MB limit"}
	}
	return nil
}

func validateSnapshotItems(items []ThingModelItem) error {
	for _, item := range items {
		if issues := validateThingModelItemPayloadMode(&item, true); len(issues) > 0 {
			return &errs.Error{Code: errs.InvalidArgument, Message: issues[0].Message}
		}
	}
	return nil
}

func parseSnapshotContent(raw json.RawMessage) (*snapshotContent, error) {
	var out snapshotContent
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "snapshot content is invalid"}
	}
	return &out, nil
}

func itemFromSnapshotContent(tenantID, thingModelID string, item snapshotContentItem) ThingModelItem {
	return ThingModelItem{
		ThingModelID:    thingModelID,
		TenantID:        tenantID,
		Type:            item.Type,
		Identifier:      item.Identifier,
		NameI18n:        item.NameI18n,
		DescriptionI18n: item.DescriptionI18n,
		ValueType:       item.ValueType,
		Parameters:      item.Parameters,
		Access:          item.Access,
		WebChartConfig:  item.WebChartConfig,
		AppChartConfig:  item.AppChartConfig,
		MetaItems:       item.MetaItems,
		SortOrder:       item.SortOrder,
	}
}

func restoreThingModelItemsFromSnapshot(ctx context.Context, tx *sqldb.Tx, tenantID, thingModelID string, content *snapshotContent) error {
	if _, err := tx.Exec(ctx, `DELETE FROM thing_model_items WHERE tenant_id = $1 AND thing_model_id = $2`, tenantID, thingModelID); err != nil {
		return err
	}
	for _, item := range content.Items {
		if err := insertThingModelItemTx(ctx, tx, tenantID, thingModelID, &ThingModelItem{
			Type:            item.Type,
			Identifier:      item.Identifier,
			NameI18n:        item.NameI18n,
			DescriptionI18n: item.DescriptionI18n,
			ValueType:       item.ValueType,
			Parameters:      item.Parameters,
			Access:          item.Access,
			WebChartConfig:  item.WebChartConfig,
			AppChartConfig:  item.AppChartConfig,
			MetaItems:       item.MetaItems,
			SortOrder:       item.SortOrder,
		}); err != nil {
			return err
		}
	}
	return nil
}

func computeVersionDiff(from, to *snapshotContent) *structuredDiffResponse {
	fromMap := make(map[string]snapshotContentItem, len(from.Items))
	toMap := make(map[string]snapshotContentItem, len(to.Items))
	for _, item := range from.Items {
		fromMap[item.Identifier] = item
	}
	for _, item := range to.Items {
		toMap[item.Identifier] = item
	}

	diff := &structuredDiffResponse{FromVersion: from.Version, ToVersion: to.Version}
	for id, item := range toMap {
		old, exists := fromMap[id]
		if !exists {
			diff.AddedItems = append(diff.AddedItems, item)
			continue
		}
		if !snapshotItemsEqual(old, item) {
			before, _ := json.Marshal(old)
			after, _ := json.Marshal(item)
			diff.ChangedItems = append(diff.ChangedItems, diffChangedItem{Identifier: id, Before: before, After: after})
		}
	}
	for id, item := range fromMap {
		if _, exists := toMap[id]; !exists {
			diff.RemovedItems = append(diff.RemovedItems, item)
		}
	}
	return diff
}

func snapshotItemsEqual(a, b snapshotContentItem) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func findDuplicateIdentifiers(items []validateThingModelItemReq) []ValidationError {
	errs := make([]ValidationError, 0)
	seen := make(map[string]int)
	for i, item := range items {
		if prev, ok := seen[item.Identifier]; ok {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("items[%d].identifier", i),
				Code:    "THING_MODEL_IDENTIFIER_DUPLICATE",
				Message: fmt.Sprintf("identifier conflicts with items[%d]", prev),
			})
			continue
		}
		seen[item.Identifier] = i
	}
	return errs
}

func checkIdentifierExists(ctx context.Context, tenantID, thingModelID, identifier, excludeID string) (bool, error) {
	var existingID string
	err := db.QueryRow(ctx, `
		SELECT id
		FROM thing_model_items
		WHERE tenant_id = $1 AND thing_model_id = $2 AND identifier = $3
		LIMIT 1
	`, tenantID, thingModelID, identifier).Scan(&existingID)
	if err != nil {
		if wrap := wrapDBErr(err, "thing model item not found"); wrap != nil {
			var encoreErr *errs.Error
			if errors.As(wrap, &encoreErr) && encoreErr.Code == errs.NotFound {
				return false, nil
			}
			return false, wrap
		}
	}
	if excludeID != "" && existingID == excludeID {
		return false, nil
	}
	return existingID != "", nil
}

func validateMetaItems(ctx context.Context, tenantID string, items []MetaItem) []ValidationError {
	errs := make([]ValidationError, 0)
	for i, item := range items {
		field := fmt.Sprintf("meta_items[%d]", i)
		if strings.TrimSpace(item.Key) == "" {
			errs = append(errs, ValidationError{Field: field, Code: "THING_MODEL_META_ITEM_INVALID", Message: "meta item key is required"})
			continue
		}
		keyDef, err := getMetaItemKeyByKey(ctx, tenantID, item.Key)
		if err != nil {
			errs = append(errs, ValidationError{Field: field, Code: "THING_MODEL_META_KEY_UNKNOWN", Message: "unknown meta item key: " + item.Key})
			continue
		}
		if err := validateMetaValueAgainstSchema(item.Value, keyDef.ValueSchema); err != nil {
			errs = append(errs, ValidationError{Field: field, Code: "THING_MODEL_META_ITEM_INVALID", Message: err.Error()})
		}
	}
	return errs
}

func validateMetaValueAgainstSchema(value json.RawMessage, schemaRaw json.RawMessage) error {
	if len(schemaRaw) == 0 {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return nil
	}
	var v any
	if len(value) == 0 {
		return newValidationIssue("THING_MODEL_META_ITEM_INVALID", "meta item value is required")
	}
	if err := json.Unmarshal(value, &v); err != nil {
		return newValidationIssue("THING_MODEL_META_ITEM_INVALID", "meta item value is not valid JSON")
	}
	if enums, ok := schema["enum"].([]any); ok && len(enums) > 0 {
		for _, enumValue := range enums {
			if fmt.Sprint(enumValue) == fmt.Sprint(v) {
				return nil
			}
		}
		return newValidationIssue("THING_MODEL_META_ITEM_INVALID", "meta item value not in enum")
	}
	switch schema["type"] {
	case "string":
		if _, ok := v.(string); !ok {
			return newValidationIssue("THING_MODEL_META_ITEM_INVALID", "meta item value must be string")
		}
	case "number":
		if _, ok := v.(float64); !ok {
			return newValidationIssue("THING_MODEL_META_ITEM_INVALID", "meta item value must be number")
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return newValidationIssue("THING_MODEL_META_ITEM_INVALID", "meta item value must be object")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return newValidationIssue("THING_MODEL_META_ITEM_INVALID", "meta item value must be boolean")
		}
	}
	return nil
}
