package errcode

import "errors"

// Thing Model errors
var (
	ErrNotFound                = errors.New("THING_MODEL_NOT_FOUND")
	ErrSnapshotNotFound        = errors.New("THING_MODEL_SNAPSHOT_NOT_FOUND")
	ErrIdentifierDuplicate     = errors.New("THING_MODEL_IDENTIFIER_DUPLICATE")
	ErrIdentifierInvalid       = errors.New("THING_MODEL_IDENTIFIER_INVALID")
	ErrIdentifierReserved      = errors.New("THING_MODEL_IDENTIFIER_RESERVED")
	ErrValueTypeInvalid        = errors.New("THING_MODEL_VALUE_TYPE_INVALID")
	ErrAccessInvalid           = errors.New("THING_MODEL_ACCESS_INVALID")
	ErrValueInvalid            = errors.New("THING_MODEL_VALUE_INVALID")
	ErrRelatedNotFound         = errors.New("THING_MODEL_RELATED_NOT_FOUND")
	ErrBadAccess               = errors.New("THING_MODEL_BAD_ACCESS")
	ErrImmutable               = errors.New("THING_MODEL_IMMUTABLE")
	ErrBadTransition           = errors.New("THING_MODEL_BAD_TRANSITION")
	ErrVersionConflict         = errors.New("THING_MODEL_VERSION_CONFLICT")
	ErrMetaKeyUnknown          = errors.New("THING_MODEL_META_KEY_UNKNOWN")
	ErrMetaValueInvalid        = errors.New("THING_MODEL_META_VALUE_INVALID")
	ErrHasReferences           = errors.New("THING_MODEL_HAS_REFERENCES")
)

// Template errors
var (
	ErrTemplateNotFound  = errors.New("DEVICE_TEMPLATE_NOT_FOUND")
	ErrTemplateArchived  = errors.New("DEVICE_TEMPLATE_ARCHIVED")
)

// Product errors
var (
	ErrProductNotFound          = errors.New("PRODUCT_NOT_FOUND")
	ErrProductHasDevices        = errors.New("PRODUCT_HAS_DEVICES")
	ErrProductTemplateMismatch  = errors.New("PRODUCT_TEMPLATE_MISMATCH")
)

// Tenant errors
var (
	ErrTenantForbidden = errors.New("TENANT_FORBIDDEN")
)

// System errors
var (
	ErrUnknownProtocol = errors.New("UNKNOWN_PROTOCOL")
	ErrI18nInvalid     = errors.New("I18N_INVALID")
)

// Device errors
var (
	ErrDeviceNotFound = errors.New("DEVICE_NOT_FOUND")
)

// AllErrorCodes is a collection of all error codes for reference
var AllErrorCodes = []error{
	// Thing Model errors
	ErrNotFound,
	ErrSnapshotNotFound,
	ErrIdentifierDuplicate,
	ErrIdentifierInvalid,
	ErrIdentifierReserved,
	ErrValueTypeInvalid,
	ErrAccessInvalid,
	ErrValueInvalid,
	ErrRelatedNotFound,
	ErrBadAccess,
	ErrImmutable,
	ErrBadTransition,
	ErrVersionConflict,
	ErrMetaKeyUnknown,
	ErrMetaValueInvalid,
	ErrHasReferences,
	// Template errors
	ErrTemplateNotFound,
	ErrTemplateArchived,
	// Product errors
	ErrProductNotFound,
	ErrProductHasDevices,
	ErrProductTemplateMismatch,
	// Tenant errors
	ErrTenantForbidden,
	// System errors
	ErrUnknownProtocol,
	ErrI18nInvalid,
	// Device errors
	ErrDeviceNotFound,
}
