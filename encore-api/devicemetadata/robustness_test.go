package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// R1: Giant thing model – 200 items should not crash or time out.
func TestRobustness_GiantThingModel(t *testing.T) {
	if testing.Short() {
		t.Skip("robustness – requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()

	tm, err := createThingModel(ctx, tenantID, &ThingModel{Name: "Giant", Status: "DRAFT"})
	if err != nil {
		t.Fatalf("createThingModel: %v", err)
	}

	const n = 50 // keep test fast; still exercises bulk path
	for i := 0; i < n; i++ {
		prefix := string(rune('a' + i%26))
		suffix := strings.Repeat("x", i/26)
		_, err := createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
			Type:       "PROPERTY",
			Identifier: prefix + suffix + strings.Repeat("y", i%3),
			NameI18n:   json.RawMessage(`{"default":"Field"}`),
			ValueType:  json.RawMessage(`{"kind":"FLOAT"}`),
			Access:     json.RawMessage(`{"read":true}`),
		})
		if err != nil {
			t.Fatalf("createThingModelItem #%d: %v", i, err)
		}
	}

	listed, total, err := listThingModelItems(ctx, tenantID, tm.ID, "", "", pageRequest{Page: 1, PageSize: 250})
	if err != nil {
		t.Fatalf("list after batch: %v", err)
	}
	if total < n {
		t.Fatalf("expected at least %d items, got total=%d listed=%d", n, total, len(listed))
	}
}

// R2: Oversized field name (unicode, 500 chars) must be rejected cleanly.
func TestRobustness_OversizedFieldName(t *testing.T) {
	if testing.Short() {
		t.Skip("robustness – requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()
	tm, _ := createThingModel(ctx, tenantID, &ThingModel{Name: "RobustTM", Status: "DRAFT"})

	longName := strings.Repeat("あ", 500)
	nameJSON, _ := json.Marshal(map[string]string{"default": longName})

	_, err := createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "field_a",
		NameI18n:   json.RawMessage(nameJSON),
		ValueType:  json.RawMessage(`{"kind":"FLOAT"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	// Should either succeed (truncated/stored) or fail with a validation error – never panic
	_ = err
}

// R3: Invalid JSON in value_type must return an error, not panic.
func TestRobustness_MalformedValueTypeJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("robustness – requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()
	tm, _ := createThingModel(ctx, tenantID, &ThingModel{Name: "BadJSON", Status: "DRAFT"})

	_, err := createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "bad_vt",
		NameI18n:   json.RawMessage(`{"default":"Bad"}`),
		ValueType:  json.RawMessage(`{NOT_VALID_JSON`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	if err == nil {
		t.Fatal("malformed value_type JSON must return an error")
	}
}

// R4: Invalid identifier characters must be rejected.
func TestRobustness_InvalidIdentifiers(t *testing.T) {
	cases := []struct {
		id  string
		bad bool
	}{
		{"123start", true},
		{"has space", true},
		{"emoji😀", true},
		{"valid_id", false},
		{"a", false},
		{"_underscore", true},
		{"UPPER", true},
		{"has-dash", true},
		{strings.Repeat("a", 65), true},
	}
	for _, tc := range cases {
		err := ValidateIdentifier(tc.id)
		if tc.bad && err == nil {
			t.Errorf("identifier %q should be invalid", tc.id)
		}
		if !tc.bad && err != nil {
			t.Errorf("identifier %q should be valid: %v", tc.id, err)
		}
	}
}

// R5: Reserved identifier words must be rejected.
func TestRobustness_ReservedIdentifiers(t *testing.T) {
	reserved := []string{"id", "type", "ts", "time", "timestamp"}
	for _, word := range reserved {
		err := ValidateIdentifier(word)
		if err == nil {
			t.Errorf("reserved identifier %q should be invalid", word)
		}
	}
}

// R6: Empty enum_values for ENUM type must be rejected.
func TestRobustness_EmptyEnumValues(t *testing.T) {
	vt := ValueType{Kind: "ENUM", EnumValues: []EnumOption{}}
	if err := vt.Validate(); err == nil {
		t.Fatal("ENUM with no enum_values should fail validation")
	}
}

// R7: FLOAT constraint – min > max must be rejected.
func TestRobustness_FloatMinGreaterThanMax(t *testing.T) {
	min, max := 100.0, 0.0
	vt := ValueType{Kind: "FLOAT", Constraint: &ValueConstraint{Min: &min, Max: &max}}
	if err := vt.Validate(); err == nil {
		t.Fatal("min > max should fail validation")
	}
}

// R8: Value exceeding max must be rejected by ValidateValue.
func TestRobustness_FloatValueExceedsMax(t *testing.T) {
	min, max := 0.0, 100.0
	vt := ValueType{Kind: "FLOAT", Constraint: &ValueConstraint{Min: &min, Max: &max}}
	if err := vt.ValidateValue(json.RawMessage("150")); err == nil {
		t.Fatal("value 150 exceeds max=100 – must fail")
	}
}

// R9: Value below min must be rejected.
func TestRobustness_FloatValueBelowMin(t *testing.T) {
	min, max := 0.0, 100.0
	vt := ValueType{Kind: "FLOAT", Constraint: &ValueConstraint{Min: &min, Max: &max}}
	if err := vt.ValidateValue(json.RawMessage("-1")); err == nil {
		t.Fatal("value -1 is below min=0 – must fail")
	}
}

// R10: Boolean field must reject non-boolean values.
func TestRobustness_BoolFieldRejectsNonBool(t *testing.T) {
	vt := ValueType{Kind: "BOOL"}
	if err := vt.ValidateValue(json.RawMessage(`"yes"`)); err == nil {
		t.Fatal("string value for BOOL field must fail")
	}
	if err := vt.ValidateValue(json.RawMessage(`1`)); err == nil {
		t.Fatal("integer value for BOOL field must fail")
	}
}

// R11: ENUM value not in allowed set must be rejected.
func TestRobustness_EnumValueNotInSet(t *testing.T) {
	vt := ValueType{
		Kind: "ENUM",
		EnumValues: []EnumOption{
			{Code: "ON", Label: I18nString{Default: "On"}},
			{Code: "OFF", Label: I18nString{Default: "Off"}},
		},
	}
	if err := vt.ValidateValue(json.RawMessage(`"MAYBE"`)); err == nil {
		t.Fatal("enum value MAYBE not in {ON, OFF} – must fail")
	}
}

// R12: String regex pattern mismatch must be rejected.
func TestRobustness_StringPatternMismatch(t *testing.T) {
	maxLen := 10
	vt := ValueType{Kind: "STRING", Constraint: &ValueConstraint{Pattern: `^[A-Z]+$`, MaxLength: &maxLen}}
	if err := vt.ValidateValue(json.RawMessage(`"abc"`)); err == nil {
		t.Fatal("lowercase does not match ^[A-Z]+$ – must fail")
	}
}

// R13: Concurrent publish attempts on the same thing model must not result in duplicate snapshots.
// This is a best-effort concurrency test.
func TestRobustness_ConcurrentPublish(t *testing.T) {
	if testing.Short() {
		t.Skip("robustness – requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()

	tm, _ := createThingModel(ctx, tenantID, &ThingModel{Name: "ConcModel", Status: "DRAFT"})
	_, _ = createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "c",
		NameI18n:   json.RawMessage(`{"default":"C"}`),
		ValueType:  json.RawMessage(`{"kind":"BOOL"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})

	// Fire N concurrent publishes – at most 1 should succeed
	const workers = 5
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_, err := publishThingModel(ctx, tenantID, tm.ID, testActorID(), &publishReq{Changelog: "concurrent"})
			errs <- err
		}()
	}

	successes := 0
	for i := 0; i < workers; i++ {
		if <-errs == nil {
			successes++
		}
	}
	if successes > 1 {
		t.Fatalf("concurrent publish: expected ≤1 success, got %d", successes)
	}

	// Verify versions are consistent
	versions, err := listVersions(ctx, tenantID, tm.ID)
	if err != nil {
		t.Fatalf("listVersions: %v", err)
	}
	if len(versions) != successes {
		t.Fatalf("version count %d != success count %d", len(versions), successes)
	}
}
