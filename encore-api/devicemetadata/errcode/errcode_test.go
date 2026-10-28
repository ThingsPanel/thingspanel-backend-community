package errcode

import (
	"errors"
	"net/http"
	"testing"
)

func TestAllErrorCodesHaveMessages(t *testing.T) {
	for _, code := range AllErrorCodes {
		codeStr := code.Error()

		// Check zh-CN message
		zhMsg := Message(code, "zh-CN")
		if zhMsg == codeStr {
			t.Errorf("No zh-CN message for error code: %s", codeStr)
		}

		// Check en-US message
		enMsg := Message(code, "en-US")
		if enMsg == codeStr {
			t.Errorf("No en-US message for error code: %s", codeStr)
		}

		// Check both are different from code
		if zhMsg == "" {
			t.Errorf("Empty zh-CN message for error code: %s", codeStr)
		}
		if enMsg == "" {
			t.Errorf("Empty en-US message for error code: %s", codeStr)
		}
	}
}

func TestMessageLocaleResolution(t *testing.T) {
	tests := []struct {
		name            string
		code            error
		locale          string
		expectedMessage string
	}{
		{"Exact match zh-CN", ErrNotFound, "zh-CN", "物模型未找到"},
		{"Exact match en-US", ErrNotFound, "en-US", "Thing model not found"},
		{"Empty locale defaults to zh-CN", ErrNotFound, "", "物模型未找到"},
		{"Unsupported locale falls back to zh-CN", ErrNotFound, "ja-JP", "物模型未找到"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Message(tt.code, tt.locale)
			if msg != tt.expectedMessage {
				t.Errorf("Expected '%s', got '%s'", tt.expectedMessage, msg)
			}
		})
	}
}

func TestMessageNilCode(t *testing.T) {
	msg := Message(nil, "zh-CN")
	if msg != "" {
		t.Errorf("Expected empty string for nil code, got: %s", msg)
	}
}

func TestDetailedErrorUnwrap(t *testing.T) {
	details := map[string]interface{}{"id": "test-123", "reason": "invalid format"}
	err := New(ErrIdentifierInvalid, details)

	// Test Error() method
	if err.Error() != "THING_MODEL_IDENTIFIER_INVALID" {
		t.Errorf("Expected error message, got: %s", err.Error())
	}

	// Test Unwrap() method
	unwrapped := err.Unwrap()
	if unwrapped != ErrIdentifierInvalid {
		t.Errorf("Expected unwrapped error to be ErrIdentifierInvalid, got: %v", unwrapped)
	}

	// Test errors.Is() with Unwrap
	if !errors.Is(err, ErrIdentifierInvalid) {
		t.Errorf("errors.Is(err, ErrIdentifierInvalid) should be true")
	}

	// Verify details are preserved
	if len(err.Details) != 2 {
		t.Errorf("Expected 2 details, got: %d", len(err.Details))
	}
	if err.Details["id"] != "test-123" {
		t.Errorf("Expected id detail to be 'test-123', got: %v", err.Details["id"])
	}
}

func TestConvenienceConstructors(t *testing.T) {
	tests := []struct {
		name         string
		constructor  func() error
		expectedCode error
		checkDetails func(t *testing.T, err *DetailedError)
	}{
		{
			name: "NewBadIdentifier",
			constructor: func() error {
				return NewBadIdentifier("invalid-id", "contains uppercase")
			},
			expectedCode: ErrIdentifierInvalid,
			checkDetails: func(t *testing.T, err *DetailedError) {
				if err.Details["identifier"] != "invalid-id" {
					t.Errorf("Expected identifier 'invalid-id', got: %v", err.Details["identifier"])
				}
				if err.Details["reason"] != "contains uppercase" {
					t.Errorf("Expected reason 'contains uppercase', got: %v", err.Details["reason"])
				}
			},
		},
		{
			name: "NewBadTransition",
			constructor: func() error {
				return NewBadTransition("DRAFT", "ARCHIVED")
			},
			expectedCode: ErrBadTransition,
			checkDetails: func(t *testing.T, err *DetailedError) {
				if err.Details["from"] != "DRAFT" {
					t.Errorf("Expected from 'DRAFT', got: %v", err.Details["from"])
				}
				if err.Details["to"] != "ARCHIVED" {
					t.Errorf("Expected to 'ARCHIVED', got: %v", err.Details["to"])
				}
			},
		},
		{
			name: "NewHasReferences",
			constructor: func() error {
				return NewHasReferences(5, "device")
			},
			expectedCode: ErrHasReferences,
			checkDetails: func(t *testing.T, err *DetailedError) {
				if err.Details["count"] != 5 {
					t.Errorf("Expected count 5, got: %v", err.Details["count"])
				}
				if err.Details["resource_type"] != "device" {
					t.Errorf("Expected resource_type 'device', got: %v", err.Details["resource_type"])
				}
			},
		},
		{
			name: "NewVersionConflict",
			constructor: func() error {
				return NewVersionConflict(10, 9)
			},
			expectedCode: ErrVersionConflict,
			checkDetails: func(t *testing.T, err *DetailedError) {
				if err.Details["expected_version"] != 10 {
					t.Errorf("Expected expected_version 10, got: %v", err.Details["expected_version"])
				}
				if err.Details["actual_version"] != 9 {
					t.Errorf("Expected actual_version 9, got: %v", err.Details["actual_version"])
				}
			},
		},
		{
			name: "NewMetaKeyUnknown",
			constructor: func() error {
				return NewMetaKeyUnknown("custom_field")
			},
			expectedCode: ErrMetaKeyUnknown,
			checkDetails: func(t *testing.T, err *DetailedError) {
				if err.Details["key"] != "custom_field" {
					t.Errorf("Expected key 'custom_field', got: %v", err.Details["key"])
				}
			},
		},
		{
			name: "NewImmutable",
			constructor: func() error {
				return NewImmutable("Published snapshots cannot be modified")
			},
			expectedCode: ErrImmutable,
			checkDetails: func(t *testing.T, err *DetailedError) {
				if err.Details["reason"] != "Published snapshots cannot be modified" {
					t.Errorf("Expected reason, got: %v", err.Details["reason"])
				}
			},
		},
		{
			name: "NewRelatedNotFound",
			constructor: func() error {
				return NewRelatedNotFound("thing_model", "tm-123")
			},
			expectedCode: ErrRelatedNotFound,
			checkDetails: func(t *testing.T, err *DetailedError) {
				if err.Details["resource_type"] != "thing_model" {
					t.Errorf("Expected resource_type 'thing_model', got: %v", err.Details["resource_type"])
				}
				if err.Details["resource_id"] != "tm-123" {
					t.Errorf("Expected resource_id 'tm-123', got: %v", err.Details["resource_id"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.constructor()
			if !errors.Is(err, tt.expectedCode) {
				t.Errorf("Expected errors.Is to return true for %v, got false", tt.expectedCode)
			}

			if detailedErr, ok := err.(*DetailedError); ok {
				tt.checkDetails(t, detailedErr)
			} else {
				t.Errorf("Expected *DetailedError, got: %T", err)
			}
		})
	}
}

func TestHTTPStatusCode(t *testing.T) {
	tests := []struct {
		code           error
		expectedStatus int
		description    string
	}{
		{ErrNotFound, http.StatusNotFound, "404 for NotFound"},
		{ErrSnapshotNotFound, http.StatusNotFound, "404 for SnapshotNotFound"},
		{ErrTemplateNotFound, http.StatusNotFound, "404 for TemplateNotFound"},
		{ErrProductNotFound, http.StatusNotFound, "404 for ProductNotFound"},
		{ErrDeviceNotFound, http.StatusNotFound, "404 for DeviceNotFound"},

		{ErrIdentifierInvalid, http.StatusBadRequest, "400 for IdentifierInvalid"},
		{ErrValueTypeInvalid, http.StatusBadRequest, "400 for ValueTypeInvalid"},
		{ErrAccessInvalid, http.StatusBadRequest, "400 for AccessInvalid"},
		{ErrValueInvalid, http.StatusBadRequest, "400 for ValueInvalid"},

		{ErrIdentifierDuplicate, http.StatusConflict, "409 for IdentifierDuplicate"},
		{ErrVersionConflict, http.StatusConflict, "409 for VersionConflict"},

		{ErrImmutable, http.StatusPreconditionFailed, "412 for Immutable"},
		{ErrBadTransition, http.StatusPreconditionFailed, "412 for BadTransition"},

		{ErrTenantForbidden, http.StatusForbidden, "403 for TenantForbidden"},
		{ErrBadAccess, http.StatusForbidden, "403 for BadAccess"},

		{ErrRelatedNotFound, http.StatusUnprocessableEntity, "422 for RelatedNotFound"},
		{ErrProductHasDevices, http.StatusUnprocessableEntity, "422 for ProductHasDevices"},
		{ErrProductTemplateMismatch, http.StatusUnprocessableEntity, "422 for ProductTemplateMismatch"},
		{ErrHasReferences, http.StatusUnprocessableEntity, "422 for HasReferences"},

		{nil, http.StatusInternalServerError, "500 for nil error"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			status := HTTPStatusCode(tt.code)
			if status != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, status)
			}
		})
	}
}

func TestErrorCodeConstants(t *testing.T) {
	// Verify we have at least 25 error codes
	if len(AllErrorCodes) < 25 {
		t.Errorf("Expected at least 25 error codes, got %d", len(AllErrorCodes))
	}

	// Verify all error codes are unique
	seen := make(map[string]struct{})
	for _, code := range AllErrorCodes {
		codeStr := code.Error()
		if _, ok := seen[codeStr]; ok {
			t.Errorf("Duplicate error code: %s", codeStr)
		}
		seen[codeStr] = struct{}{}
	}
}

func TestDetailedErrorNilCode(t *testing.T) {
	err := &DetailedError{Code: nil, Details: map[string]interface{}{"key": "value"}}
	if err.Error() != "" {
		t.Errorf("Expected empty string for nil code, got: %s", err.Error())
	}
	if err.Unwrap() != nil {
		t.Errorf("Expected nil from Unwrap(), got: %v", err.Unwrap())
	}
}

func TestDetailedErrorWithNilDetails(t *testing.T) {
	err := New(ErrNotFound, nil)
	if err.Code != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got: %v", err.Code)
	}
	if err.Details != nil {
		t.Errorf("Expected nil details, got: %v", err.Details)
	}
}
