package errcode

// DetailedError wraps an error code with additional context details
type DetailedError struct {
	Code    error
	Details map[string]interface{}
}

// Error implements the error interface
func (e *DetailedError) Error() string {
	if e.Code == nil {
		return ""
	}
	return e.Code.Error()
}

// Unwrap returns the underlying error code for use with errors.Is/As
func (e *DetailedError) Unwrap() error {
	return e.Code
}

// New creates a DetailedError with the given error code and details map
func New(code error, details map[string]interface{}) *DetailedError {
	return &DetailedError{
		Code:    code,
		Details: details,
	}
}

// NewBadIdentifier creates a DetailedError for invalid identifier with reason
func NewBadIdentifier(id, reason string) *DetailedError {
	return &DetailedError{
		Code: ErrIdentifierInvalid,
		Details: map[string]interface{}{
			"identifier": id,
			"reason":     reason,
		},
	}
}

// NewBadTransition creates a DetailedError for invalid state transition
func NewBadTransition(from, to interface{}) *DetailedError {
	return &DetailedError{
		Code: ErrBadTransition,
		Details: map[string]interface{}{
			"from": from,
			"to":   to,
		},
	}
}

// NewHasReferences creates a DetailedError indicating the resource has dependent items
func NewHasReferences(count int, resourceType string) *DetailedError {
	return &DetailedError{
		Code: ErrHasReferences,
		Details: map[string]interface{}{
			"count":         count,
			"resource_type": resourceType,
		},
	}
}

// NewVersionConflict creates a DetailedError for version mismatch
func NewVersionConflict(expected, actual int) *DetailedError {
	return &DetailedError{
		Code: ErrVersionConflict,
		Details: map[string]interface{}{
			"expected_version": expected,
			"actual_version":   actual,
		},
	}
}

// NewMetaKeyUnknown creates a DetailedError for unknown meta key
func NewMetaKeyUnknown(key string) *DetailedError {
	return &DetailedError{
		Code: ErrMetaKeyUnknown,
		Details: map[string]interface{}{
			"key": key,
		},
	}
}

// NewImmutable creates a DetailedError indicating the resource cannot be modified
func NewImmutable(reason string) *DetailedError {
	return &DetailedError{
		Code: ErrImmutable,
		Details: map[string]interface{}{
			"reason": reason,
		},
	}
}

// NewRelatedNotFound creates a DetailedError for a missing related resource
func NewRelatedNotFound(resourceType, resourceID string) *DetailedError {
	return &DetailedError{
		Code: ErrRelatedNotFound,
		Details: map[string]interface{}{
			"resource_type": resourceType,
			"resource_id":   resourceID,
		},
	}
}
