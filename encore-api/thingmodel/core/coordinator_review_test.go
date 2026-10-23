package core_test

import (
	"testing"

	"encore.app/thingmodel/core"
)

// The frozen request schema requires precision >= 0. This checks definition
// validity; it does not impose rounding or precision rejection on device values.
func TestConstraintDefinitionRejectsNegativePrecision(t *testing.T) {
	precision := -1
	valueType := core.ValueType{
		Kind:       core.ScalarNumber,
		Constraint: &core.ValueConstraint{Precision: &precision},
	}
	if err := core.ValidateScalar(valueType, []byte("1.25")); err == nil {
		t.Fatal("negative precision contradicts the frozen numeric constraint schema")
	}
	precision = 0
	if err := core.ValidateScalar(valueType, []byte("1.25")); err != nil {
		t.Fatalf("valid precision metadata must not silently add a rounding rule: %v", err)
	}
}
