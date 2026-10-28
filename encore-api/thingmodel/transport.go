package thingmodel

import (
	"encoding/json"
	"fmt"
	"time"

	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
)

// Transport types follow the frozen v2 command contract. They are deliberately
// distinct from the semantic core and from any Community database row.
type ModelDraft struct {
	Name            string           `json:"name"`
	DescriptionI18n *core.I18nString `json:"descriptionI18n,omitempty"`
	Category        *string          `json:"category,omitempty"`
	Fields          []ModelField     `json:"fields"`
}

type FieldAccess struct {
	Read    *bool `json:"read"`
	Write   *bool `json:"write"`
	Observe *bool `json:"observe"`
}

type ValueConstraint struct {
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	Step      *float64 `json:"step,omitempty"`
	Precision *int     `json:"precision,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	MaxLength *int     `json:"maxLength,omitempty"`
	TrueText  string   `json:"trueText,omitempty"`
	FalseText string   `json:"falseText,omitempty"`
}

type EnumOption struct {
	Code  json.RawMessage `json:"code"`
	Label core.I18nString `json:"label"`
}

type ValueType struct {
	Kind       core.ScalarKind  `json:"kind"`
	BaseKind   string           `json:"baseKind,omitempty"`
	Constraint *ValueConstraint `json:"constraint,omitempty"`
	EnumValues []EnumOption     `json:"enumValues,omitempty"`
}

type Parameter struct {
	Identifier      string           `json:"identifier"`
	NameI18n        core.I18nString  `json:"nameI18n"`
	DescriptionI18n *core.I18nString `json:"descriptionI18n,omitempty"`
	ValueType       ValueType        `json:"valueType"`
}

type Parameters struct {
	SchemaVersion int         `json:"schemaVersion"`
	Items         []Parameter `json:"items"`
}

type ModelField struct {
	FieldID         string           `json:"fieldID,omitempty"`
	Kind            core.FieldKind   `json:"kind"`
	Identifier      string           `json:"identifier"`
	NameI18n        core.I18nString  `json:"nameI18n"`
	DescriptionI18n *core.I18nString `json:"descriptionI18n,omitempty"`
	ValueType       *ValueType       `json:"valueType,omitempty"`
	Access          *FieldAccess     `json:"access"`
	Unit            string           `json:"unit,omitempty"`
	Parameters      *Parameters      `json:"parameters,omitempty"`
	MetaItems       json.RawMessage  `json:"metaItems,omitempty"`
	SortOrder       int              `json:"sortOrder,omitempty"`
}

func (d ModelDraft) coreModel() (core.Model, error) {
	if d.Fields == nil {
		return core.Model{}, fmt.Errorf("%w: fields must be an explicit array", app.ErrInvalid)
	}
	m := core.Model{Name: d.Name, Description: d.DescriptionI18n, Category: d.Category, Fields: make([]core.Field, 0, len(d.Fields))}
	for i, f := range d.Fields {
		if f.FieldID != "" || len(f.MetaItems) != 0 {
			return core.Model{}, fmt.Errorf("%w: fields[%d] contains resource metadata not supported by semantic replacement", app.ErrInvalid, i)
		}
		if f.Access == nil || f.Access.Read == nil || f.Access.Write == nil || f.Access.Observe == nil {
			return core.Model{}, fmt.Errorf("%w: fields[%d].access requires read, write and observe booleans", app.ErrInvalid, i)
		}
		v := core.Field{Kind: f.Kind, Identifier: f.Identifier, Name: f.NameI18n, Description: f.DescriptionI18n, Unit: f.Unit, SortOrder: f.SortOrder,
			Access: core.Access{Read: *f.Access.Read, Write: *f.Access.Write, Observe: *f.Access.Observe}}
		if f.ValueType != nil {
			vt := f.ValueType.coreValue()
			v.ValueType = &vt
		}
		if f.Parameters != nil {
			v.Parameters = &core.Parameters{SchemaVersion: f.Parameters.SchemaVersion, Items: make([]core.Parameter, 0, len(f.Parameters.Items))}
			for _, p := range f.Parameters.Items {
				v.Parameters.Items = append(v.Parameters.Items, core.Parameter{Identifier: p.Identifier, Name: p.NameI18n, Description: p.DescriptionI18n, ValueType: p.ValueType.coreValue()})
			}
		}
		m.Fields = append(m.Fields, v)
	}
	return m, nil
}

func (v ValueType) coreValue() core.ValueType {
	b := core.ScalarKind(v.BaseKind)
	if b == "BOOLEAN" {
		b = core.ScalarBool
	}
	out := core.ValueType{Kind: v.Kind, EnumBaseKind: b}
	if v.Constraint != nil {
		c := v.Constraint
		out.Constraint = &core.ValueConstraint{Min: c.Min, Max: c.Max, Step: c.Step, Precision: c.Precision, Pattern: c.Pattern, MaxLength: c.MaxLength, TrueText: c.TrueText, FalseText: c.FalseText}
	}
	for _, e := range v.EnumValues {
		out.EnumValues = append(out.EnumValues, core.EnumValue{Value: e.Code, Label: e.Label})
	}
	return out
}

func draftFromCore(m core.Model) ModelDraft {
	d := ModelDraft{Name: m.Name, DescriptionI18n: m.Description, Category: m.Category, Fields: make([]ModelField, 0, len(m.Fields))}
	for _, f := range m.Fields {
		r, w, o := f.Access.Read, f.Access.Write, f.Access.Observe
		v := ModelField{Kind: f.Kind, Identifier: f.Identifier, NameI18n: f.Name, DescriptionI18n: f.Description, Unit: f.Unit, SortOrder: f.SortOrder, Access: &FieldAccess{Read: &r, Write: &w, Observe: &o}}
		if f.ValueType != nil {
			vt := valueFromCore(*f.ValueType)
			v.ValueType = &vt
		}
		if f.Parameters != nil {
			v.Parameters = &Parameters{SchemaVersion: f.Parameters.SchemaVersion, Items: make([]Parameter, 0, len(f.Parameters.Items))}
			for _, p := range f.Parameters.Items {
				v.Parameters.Items = append(v.Parameters.Items, Parameter{Identifier: p.Identifier, NameI18n: p.Name, DescriptionI18n: p.Description, ValueType: valueFromCore(p.ValueType)})
			}
		}
		d.Fields = append(d.Fields, v)
	}
	return d
}

func valueFromCore(v core.ValueType) ValueType {
	b := string(v.EnumBaseKind)
	if b == "BOOL" {
		b = "BOOLEAN"
	}
	out := ValueType{Kind: v.Kind, BaseKind: b}
	if v.Constraint != nil {
		c := v.Constraint
		out.Constraint = &ValueConstraint{Min: c.Min, Max: c.Max, Step: c.Step, Precision: c.Precision, Pattern: c.Pattern, MaxLength: c.MaxLength, TrueText: c.TrueText, FalseText: c.FalseText}
	}
	for _, e := range v.EnumValues {
		out.EnumValues = append(out.EnumValues, EnumOption{Code: e.Value, Label: e.Label})
	}
	return out
}

type ModelDetail struct {
	ModelID              string     `json:"modelID"`
	Draft                ModelDraft `json:"draft"`
	DraftRevision        uint64     `json:"draftRevision"`
	Lifecycle            string     `json:"lifecycle"`
	PublishedRevision    *uint64    `json:"publishedRevision"`
	PresentationRevision uint64     `json:"presentationRevision"`
}
type ModelSummary struct {
	ModelID           string    `json:"modelID"`
	Name              string    `json:"name"`
	Lifecycle         string    `json:"lifecycle"`
	DraftRevision     uint64    `json:"draftRevision"`
	PublishedRevision *uint64   `json:"publishedRevision"`
	UpdatedAt         time.Time `json:"updatedAt"`
}
type VersionSummary struct {
	PublishedRevision uint64    `json:"publishedRevision"`
	SnapshotID        string    `json:"snapshotID"`
	Checksum          string    `json:"checksum"`
	CreatedAt         time.Time `json:"createdAt"`
	Changelog         string    `json:"changelog"`
}
type SnapshotDetail struct {
	ModelID           string           `json:"modelID"`
	PublishedRevision uint64           `json:"publishedRevision"`
	SnapshotID        string           `json:"snapshotID"`
	Checksum          string           `json:"checksum"`
	CreatedAt         time.Time        `json:"createdAt"`
	Changelog         string           `json:"changelog"`
	Model             ModelDraft       `json:"model"`
	Resources         VersionResources `json:"resources"`
}

type VersionResources struct {
	ModelID        string              `json:"modelID"`
	Revision       uint64              `json:"revision"`
	Profile        app.ResourceProfile `json:"profile"`
	WebChartConfig json.RawMessage     `json:"webChartConfig"`
	AppChartConfig json.RawMessage     `json:"appChartConfig"`
}

type ResourcesResponse struct {
	ModelID        string              `json:"modelID"`
	Revision       uint64              `json:"revision"`
	Profile        app.ResourceProfile `json:"profile"`
	WebChartConfig json.RawMessage     `json:"webChartConfig"`
	AppChartConfig json.RawMessage     `json:"appChartConfig"`
}

type UpdateResourcesRequest struct {
	IdempotencyKey   string                 `header:"Idempotency-Key"`
	ModelID          string                 `json:"modelID"`
	ExpectedRevision *uint64                `json:"expectedRevision"`
	Resources        *app.ResourcesDocument `json:"resources"`
}

func publishedRevision(r uint64) *uint64 {
	if r == 0 {
		return nil
	}
	return &r
}
