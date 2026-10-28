package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
)

var ErrUnauthorized = errors.New("thingmodel: principal is not authorized")

// Authorize checks the trusted principal supplied by an outer adapter. It does
// not resolve roles, tenant aliases, or session state.
func Authorize(principal Principal, target TenantID, capability Capability) error {
	if principal.SubjectID == "" || principal.TenantID == "" || target == "" || principal.TenantID != target {
		return ErrUnauthorized
	}
	for _, granted := range principal.Capabilities {
		if granted == string(capability) {
			return nil
		}
	}
	return ErrUnauthorized
}

type snapshotPayload struct {
	ModelID     ModelID     `json:"model_id"`
	TenantID    TenantID    `json:"tenant_id"`
	Name        string      `json:"name"`
	Description *I18nString `json:"description,omitempty"`
	Category    *string     `json:"category,omitempty"`
	Revision    uint64      `json:"revision"`
	Fields      []Field     `json:"fields"`
}

// BuildSnapshot creates a detached semantic snapshot candidate. Revision is
// PublishedRevision+1; an application transaction must lock/compare the model
// and persist this candidate atomically before treating it as published.
// Field order is canonicalized by SortOrder, kind, then bytewise identifier; parameter and
// enum order is retained because those row orders are part of their contract.
func BuildSnapshot(model Model, id SnapshotID) (Snapshot, error) {
	if id == "" {
		return Snapshot{}, fmt.Errorf("SNAPSHOT_ID_REQUIRED: snapshot id is required")
	}
	if issues := ValidateModel(model); len(issues) != 0 {
		return Snapshot{}, fmt.Errorf("%s: %s", issues[0].Code, issues[0].Message)
	}
	if model.PublishedRevision == math.MaxUint64 {
		return Snapshot{}, errors.New("SNAPSHOT_REVISION_OVERFLOW: published revision cannot advance")
	}
	fields := cloneFields(model.Fields)
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].SortOrder != fields[j].SortOrder {
			return fields[i].SortOrder < fields[j].SortOrder
		}
		if fields[i].Kind != fields[j].Kind {
			return fields[i].Kind < fields[j].Kind
		}
		return fields[i].Identifier < fields[j].Identifier
	})
	revision := model.PublishedRevision + 1
	payload := snapshotPayload{
		ModelID: model.ID, TenantID: model.TenantID,
		Name: model.Name, Description: cloneI18nPointer(model.Description),
		Category: cloneString(model.Category), Revision: revision, Fields: fields,
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return Snapshot{}, fmt.Errorf("SNAPSHOT_SERIALIZE_FAILED: %w", err)
	}
	digest := sha256.Sum256(content)
	return Snapshot{
		ID: id, ModelID: model.ID, TenantID: model.TenantID,
		Name: model.Name, Description: cloneI18nPointer(model.Description),
		Category: cloneString(model.Category), Revision: revision,
		Checksum: hex.EncodeToString(digest[:]), Fields: fields,
		Content: append([]byte(nil), content...),
	}, nil
}

func cloneFields(fields []Field) []Field {
	if fields == nil {
		return nil
	}
	out := make([]Field, len(fields))
	for i, field := range fields {
		out[i] = field
		out[i].Name = cloneI18n(field.Name)
		out[i].Description = cloneI18nPointer(field.Description)
		if field.ValueType != nil {
			vt := cloneValueType(*field.ValueType)
			out[i].ValueType = &vt
		}
		if field.Parameters != nil {
			params := &Parameters{SchemaVersion: field.Parameters.SchemaVersion}
			if field.Parameters.Items != nil {
				params.Items = make([]Parameter, len(field.Parameters.Items))
				for j, item := range field.Parameters.Items {
					params.Items[j] = item
					params.Items[j].Name = cloneI18n(item.Name)
					params.Items[j].Description = cloneI18nPointer(item.Description)
					params.Items[j].ValueType = cloneValueType(item.ValueType)
				}
			}
			out[i].Parameters = params
		}
	}
	return out
}

func cloneValueType(valueType ValueType) ValueType {
	out := valueType
	if valueType.Constraint != nil {
		constraint := *valueType.Constraint
		constraint.Min = cloneFloat(valueType.Constraint.Min)
		constraint.Max = cloneFloat(valueType.Constraint.Max)
		constraint.Step = cloneFloat(valueType.Constraint.Step)
		constraint.Precision = cloneInt(valueType.Constraint.Precision)
		constraint.MaxLength = cloneInt(valueType.Constraint.MaxLength)
		out.Constraint = &constraint
	}
	if valueType.EnumValues != nil {
		out.EnumValues = make([]EnumValue, len(valueType.EnumValues))
		for i, option := range valueType.EnumValues {
			out.EnumValues[i].Value = append(json.RawMessage(nil), option.Value...)
			out.EnumValues[i].Label = cloneI18n(option.Label)
		}
	}
	return out
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneI18n(value I18nString) I18nString {
	out := value
	if value.Locales != nil {
		out.Locales = make(map[string]string, len(value.Locales))
		for locale, text := range value.Locales {
			out.Locales[locale] = text
		}
	}
	return out
}

func cloneI18nPointer(value *I18nString) *I18nString {
	if value == nil {
		return nil
	}
	cloned := cloneI18n(*value)
	return &cloned
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
