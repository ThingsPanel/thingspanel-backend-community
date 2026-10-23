package devicemetadata

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"encore.dev/beta/errs"
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	localePattern     = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)
	regexCache        sync.Map
)

var reservedIdentifiers = map[string]struct{}{
	"id":          {},
	"type":        {},
	"tenant_id":   {},
	"device_id":   {},
	"template_id": {},
	"ts":          {},
	"time":        {},
	"timestamp":   {},
	"_meta":       {},
	"__internal":  {},
	"created_at":  {},
	"updated_at":  {},
}

type I18nString struct {
	Default string            `json:"default"`
	Locales map[string]string `json:"locales,omitempty"`
}

type ValueType struct {
	Kind       string           `json:"kind"`
	Constraint *ValueConstraint `json:"constraint,omitempty"`
	EnumValues []EnumOption     `json:"enum_values,omitempty"`
}

type ValueConstraint struct {
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	Step      *float64 `json:"step,omitempty"`
	Precision *int     `json:"precision,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	MaxLength *int     `json:"max_length,omitempty"`
	TrueText  string   `json:"true_text,omitempty"`
	FalseText string   `json:"false_text,omitempty"`
}

type EnumOption struct {
	Code  string     `json:"code"`
	Label I18nString `json:"label"`
}

type Access struct {
	Read    bool `json:"read"`
	Write   bool `json:"write"`
	Observe bool `json:"observe"`
}

type MetaItem struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
	Scope string          `json:"scope,omitempty"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type validateResp struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

type validateThingModelItemReq struct {
	ThingModelID string          `json:"thing_model_id,omitempty"`
	ItemID       string          `json:"item_id,omitempty"`
	Type         string          `json:"type"`
	Identifier   string          `json:"identifier"`
	NameI18n     json.RawMessage `json:"name_i18n"`
	ValueType    json.RawMessage `json:"value_type"`
	Parameters   json.RawMessage `json:"parameters,omitempty"`
	Access       json.RawMessage `json:"access"`
	MetaItems    json.RawMessage `json:"meta_items,omitempty"`
}

type validateThingModelReq struct {
	Name            string                      `json:"name,omitempty"`
	DescriptionI18n json.RawMessage             `json:"description_i18n,omitempty"`
	Category        string                      `json:"category,omitempty"`
	Items           []validateThingModelItemReq `json:"items"`
}

type validateDeviceTemplateReq struct {
	Name                 string `json:"name"`
	ThingModelID         string `json:"thing_model_id"`
	ThingModelSnapshotID string `json:"thing_model_snapshot_id"`
}

func ValidateIdentifier(id string) error {
	if strings.TrimSpace(id) == "" {
		return newValidationIssue("THING_MODEL_IDENTIFIER_INVALID", "identifier is required")
	}
	if !identifierPattern.MatchString(id) {
		return newValidationIssue("THING_MODEL_IDENTIFIER_INVALID", "identifier must match ^[a-z][a-z0-9_]{0,63}$")
	}
	if _, ok := reservedIdentifiers[id]; ok {
		return newValidationIssue("THING_MODEL_IDENTIFIER_RESERVED", fmt.Sprintf("identifier %q is reserved", id))
	}
	return nil
}

func (s *I18nString) Validate() error {
	return s.validateWithMax(128)
}

func (s *I18nString) ValidateAsDescription() error {
	return s.validateWithMax(1024)
}

func (s *I18nString) validateWithMax(max int) error {
	if s == nil {
		return newValidationIssue("THING_MODEL_I18N_INVALID", "default is required")
	}
	if strings.TrimSpace(s.Default) == "" {
		return newValidationIssue("THING_MODEL_I18N_INVALID", "default is required")
	}
	if len(s.Default) > max {
		return newValidationIssue("THING_MODEL_I18N_INVALID", fmt.Sprintf("default too long (max %d)", max))
	}
	for locale, text := range s.Locales {
		if !localePattern.MatchString(locale) {
			return newValidationIssue("THING_MODEL_I18N_INVALID", "bad locale: "+locale)
		}
		if len(text) > max {
			return newValidationIssue("THING_MODEL_I18N_INVALID", fmt.Sprintf("locale text too long: %s", locale))
		}
	}
	return nil
}

func (s *I18nString) Resolve(locale string) string {
	if s == nil {
		return ""
	}
	if v, ok := s.Locales[locale]; ok && v != "" {
		return v
	}
	if idx := strings.Index(locale, "-"); idx > 0 {
		if v, ok := s.Locales[locale[:idx]]; ok && v != "" {
			return v
		}
	}
	return s.Default
}

func (vt *ValueType) Validate() error {
	kind := strings.ToUpper(strings.TrimSpace(vt.Kind))
	switch kind {
	case "INT", "FLOAT":
		if vt.Constraint != nil {
			if vt.Constraint.Min != nil && vt.Constraint.Max != nil && *vt.Constraint.Min > *vt.Constraint.Max {
				return newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "min > max")
			}
			if vt.Constraint.Step != nil && *vt.Constraint.Step <= 0 {
				return newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "step must > 0")
			}
		}
	case "STRING":
		if vt.Constraint != nil && vt.Constraint.Pattern != "" {
			if _, err := compileCachedRegex(vt.Constraint.Pattern); err != nil {
				return newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "pattern not compilable: "+err.Error())
			}
		}
	case "ENUM":
		if len(vt.EnumValues) == 0 {
			return newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "enum_values empty")
		}
		seen := make(map[string]struct{}, len(vt.EnumValues))
		for _, opt := range vt.EnumValues {
			if strings.TrimSpace(opt.Code) == "" {
				return newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "enum code is required")
			}
			if err := opt.Label.Validate(); err != nil {
				return err
			}
			if _, ok := seen[opt.Code]; ok {
				return newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "duplicate enum code: "+opt.Code)
			}
			seen[opt.Code] = struct{}{}
		}
	case "BOOL":
	default:
		return newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "unsupported kind: "+vt.Kind)
	}
	return nil
}

func (vt *ValueType) ValidateValue(value json.RawMessage) error {
	if err := vt.Validate(); err != nil {
		return err
	}
	switch strings.ToUpper(strings.TrimSpace(vt.Kind)) {
	case "INT":
		var v int64
		if err := json.Unmarshal(value, &v); err != nil {
			return newValidationIssue("THING_MODEL_VALUE_INVALID", "not int")
		}
		return validateNumericConstraint(float64(v), vt.Constraint)
	case "FLOAT":
		var v float64
		if err := json.Unmarshal(value, &v); err != nil {
			return newValidationIssue("THING_MODEL_VALUE_INVALID", "not float")
		}
		return validateNumericConstraint(v, vt.Constraint)
	case "BOOL":
		var v bool
		if err := json.Unmarshal(value, &v); err != nil {
			return newValidationIssue("THING_MODEL_VALUE_INVALID", "not bool")
		}
		return nil
	case "STRING":
		var v string
		if err := json.Unmarshal(value, &v); err != nil {
			return newValidationIssue("THING_MODEL_VALUE_INVALID", "not string")
		}
		if vt.Constraint != nil {
			if vt.Constraint.MaxLength != nil && len(v) > *vt.Constraint.MaxLength {
				return newValidationIssue("THING_MODEL_VALUE_INVALID", "max_length exceeded")
			}
			if vt.Constraint.Pattern != "" {
				re, err := compileCachedRegex(vt.Constraint.Pattern)
				if err != nil {
					return newValidationIssue("THING_MODEL_VALUE_INVALID", "pattern not compilable: "+err.Error())
				}
				if !re.MatchString(v) {
					return newValidationIssue("THING_MODEL_VALUE_INVALID", "pattern mismatch")
				}
			}
		}
		return nil
	case "ENUM":
		var v string
		if err := json.Unmarshal(value, &v); err != nil {
			return newValidationIssue("THING_MODEL_VALUE_INVALID", "not string (enum)")
		}
		for _, opt := range vt.EnumValues {
			if opt.Code == v {
				return nil
			}
		}
		return newValidationIssue("THING_MODEL_VALUE_INVALID", "not in enum")
	}
	return newValidationIssue("THING_MODEL_VALUE_INVALID", "unsupported kind: "+vt.Kind)
}

func ValidateAccessForType(itemType string, access Access) error {
	switch strings.ToUpper(strings.TrimSpace(itemType)) {
	case "PROPERTY", "TELEMETRY":
		if !access.Read {
			return newValidationIssue("THING_MODEL_ACCESS_INVALID", "TELEMETRY must have read=true")
		}
	case "ATTRIBUTE":
		if !access.Read && !access.Write {
			return newValidationIssue("THING_MODEL_ACCESS_INVALID", "ATTRIBUTE must have read=true or write=true")
		}
	case "EVENT":
		if access.Read || access.Write {
			return newValidationIssue("THING_MODEL_ACCESS_INVALID", "EVENT must have read=false and write=false")
		}
		if !access.Observe {
			return newValidationIssue("THING_MODEL_ACCESS_INVALID", "EVENT must have observe=true")
		}
	case "COMMAND":
		if !access.Write {
			return newValidationIssue("THING_MODEL_ACCESS_INVALID", "COMMAND must have write=true")
		}
	default:
		return newValidationIssue("THING_MODEL_TYPE_INVALID", "unsupported item type: "+itemType)
	}
	return nil
}

func parseI18n(raw json.RawMessage) (*I18nString, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out I18nString
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, newValidationIssue("THING_MODEL_I18N_INVALID", "invalid i18n payload")
	}
	return &out, nil
}

func parseValueType(raw json.RawMessage) (*ValueType, error) {
	if len(raw) == 0 {
		return nil, newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "value_type is required")
	}
	var out ValueType
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, newValidationIssue("THING_MODEL_VALUE_TYPE_INVALID", "invalid value_type payload")
	}
	return &out, nil
}

func parseAccess(raw json.RawMessage) (Access, error) {
	if len(raw) == 0 {
		return Access{}, newValidationIssue("THING_MODEL_ACCESS_INVALID", "access is required")
	}
	var out Access
	if err := json.Unmarshal(raw, &out); err != nil {
		return Access{}, newValidationIssue("THING_MODEL_ACCESS_INVALID", "invalid access payload")
	}
	return out, nil
}

func parseMetaItems(raw json.RawMessage) ([]MetaItem, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []MetaItem
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, newValidationIssue("THING_MODEL_META_ITEM_INVALID", "invalid meta_items payload")
	}
	return out, nil
}

func compileCachedRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := regexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	actual, _ := regexCache.LoadOrStore(pattern, re)
	return actual.(*regexp.Regexp), nil
}

func validateNumericConstraint(v float64, c *ValueConstraint) error {
	if c == nil {
		return nil
	}
	if c.Min != nil && v < *c.Min {
		return newValidationIssue("THING_MODEL_VALUE_INVALID", "below min")
	}
	if c.Max != nil && v > *c.Max {
		return newValidationIssue("THING_MODEL_VALUE_INVALID", "above max")
	}
	return nil
}

func newValidationIssue(code, message string) error {
	return &errs.Error{Code: errs.InvalidArgument, Meta: errs.Metadata{"code": code}, Message: message}
}

func validationCode(err error, fallback string) string {
	var e *errs.Error
	if errors.As(err, &e) && e.Meta != nil {
		if code, ok := e.Meta["code"].(string); ok && code != "" {
			return code
		}
	}
	return fallback
}

func appendValidationError(dst []ValidationError, field string, err error, fallbackCode string) []ValidationError {
	if err == nil {
		return dst
	}
	return append(dst, ValidationError{
		Field:   field,
		Code:    validationCode(err, fallbackCode),
		Message: err.Error(),
	})
}

func sortValidationErrors(errs []ValidationError) {
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Field == errs[j].Field {
			return errs[i].Code < errs[j].Code
		}
		return errs[i].Field < errs[j].Field
	})
}
