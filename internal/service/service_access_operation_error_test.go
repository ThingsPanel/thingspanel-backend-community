package service

import (
	"errors"
	"testing"

	"project/internal/dal"
	"project/pkg/errcode"
)

func isServiceAccessOperationConflict(err error) bool {
	var failure *errcode.Error
	if !errors.As(err, &failure) || failure.Code != errcode.CodeOpDenied {
		return false
	}
	data, ok := failure.Data.(map[string]interface{})
	return ok && data["reason"] == "SERVICE_ACCESS_OPERATION_CONFLICT"
}

func TestServiceAccessOperationErrorContract(t *testing.T) {
	for _, test := range []struct {
		err    error
		code   int
		reason string
	}{
		{dal.ErrServiceAccessOperationConflict, errcode.CodeOpDenied, "SERVICE_ACCESS_OPERATION_CONFLICT"},
		{dal.ErrServiceAccessResourceChanged, errcode.CodeOpDenied, "SERVICE_ACCESS_RESOURCE_CHANGED"},
		{dal.ErrServiceAccessOperationUnknown, errcode.CodeDBError, "SERVICE_ACCESS_OUTCOME_UNKNOWN"},
	} {
		var failure *errcode.Error
		if err := serviceAccessOperationError(test.err); !errors.As(err, &failure) || failure.Code != test.code {
			t.Fatalf("wrong existing error code: %v", err)
		}
		data, ok := failure.Data.(map[string]interface{})
		if !ok || data["reason"] != test.reason {
			t.Fatalf("missing stable machine reason: %#v", failure.Data)
		}
	}
}
