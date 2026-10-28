// Package core contains the ThingModel domain values and rules. It deliberately
// depends only on the Go standard library.
package core

import "encoding/json"

type ModelID string
type TenantID string
type SnapshotID string

// Principal is trusted identity supplied by an outer adapter. Core never loads
// identity from a request, global auth state, or a session store.
type Principal struct {
	SubjectID    string   `json:"subject_id"`
	TenantID     TenantID `json:"tenant_id"`
	Capabilities []string `json:"capabilities"`
}

type Capability string

const (
	CapabilityRead    Capability = "thingmodel.read"
	CapabilityWrite   Capability = "thingmodel.write"
	CapabilityPublish Capability = "thingmodel.publish"
	CapabilityDelete  Capability = "thingmodel.delete"
	CapabilityBind    Capability = "template.bind"
)

type I18nString struct {
	Default string            `json:"default"`
	Locales map[string]string `json:"locales,omitempty"`
}

type Model struct {
	ID                ModelID     `json:"id"`
	TenantID          TenantID    `json:"tenant_id"`
	Name              string      `json:"name"`
	Description       *I18nString `json:"description,omitempty"`
	Category          *string     `json:"category,omitempty"`
	Fields            []Field     `json:"fields"`
	DraftRevision     uint64      `json:"draft_revision"`
	PublishedRevision uint64      `json:"published_revision"`
}

type FieldKind string

const (
	FieldTelemetry FieldKind = "TELEMETRY"
	FieldAttribute FieldKind = "ATTRIBUTE"
	FieldEvent     FieldKind = "EVENT"
	FieldCommand   FieldKind = "COMMAND"
)

type Access struct {
	Read    bool `json:"read"`
	Write   bool `json:"write"`
	Observe bool `json:"observe"`
}

type Field struct {
	Kind        FieldKind   `json:"kind"`
	Identifier  string      `json:"identifier"`
	Name        I18nString  `json:"name"`
	Description *I18nString `json:"description,omitempty"`
	Unit        string      `json:"unit,omitempty"`
	Access      Access      `json:"access"`
	ValueType   *ValueType  `json:"value_type,omitempty"`
	Parameters  *Parameters `json:"parameters,omitempty"`
	SortOrder   int         `json:"sort_order"`
}

type ScalarKind string

const (
	ScalarInt    ScalarKind = "INT"
	ScalarFloat  ScalarKind = "FLOAT"
	ScalarNumber ScalarKind = "NUMBER"
	ScalarBool   ScalarKind = "BOOL"
	ScalarString ScalarKind = "STRING"
	ScalarEnum   ScalarKind = "ENUM"
)

type ValueType struct {
	Kind         ScalarKind       `json:"kind"`
	Constraint   *ValueConstraint `json:"constraint,omitempty"`
	EnumBaseKind ScalarKind       `json:"enum_base_kind,omitempty"`
	EnumValues   []EnumValue      `json:"enum_values,omitempty"`
}

type ValueConstraint struct {
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	Step      *float64 `json:"step,omitempty"`
	Precision *int     `json:"precision,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	MaxLength *int     `json:"max_length,omitempty"`
	TrueText  string   `json:"true_text,omitempty"`
	FalseText string   `json:"false_text,omitempty"`
}

// EnumValue.Value is one JSON scalar whose Go/JSON type is checked against the
// containing ValueType.EnumBaseKind. Labels are localized semantic values
// needed when a device renders an enum option.
type EnumValue struct {
	Value json.RawMessage `json:"value"`
	Label I18nString      `json:"label"`
}

// ParametersVersion identifies the flat parameter contract frozen for v1.
const ParametersVersion = 1

type Parameters struct {
	SchemaVersion int         `json:"schema_version"`
	Items         []Parameter `json:"items"`
}

type Parameter struct {
	Identifier  string      `json:"identifier"`
	Name        I18nString  `json:"name"`
	Description *I18nString `json:"description,omitempty"`
	ValueType   ValueType   `json:"value_type"`
}

type Snapshot struct {
	ID          SnapshotID  `json:"id"`
	ModelID     ModelID     `json:"model_id"`
	TenantID    TenantID    `json:"tenant_id"`
	Name        string      `json:"name"`
	Description *I18nString `json:"description,omitempty"`
	Category    *string     `json:"category,omitempty"`
	Revision    uint64      `json:"revision"`
	Checksum    string      `json:"checksum"`
	Fields      []Field     `json:"fields"`
	Content     []byte      `json:"content"`
}

type ValidationIssue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
