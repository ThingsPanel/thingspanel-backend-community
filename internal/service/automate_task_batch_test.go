package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"project/internal/model"
	"project/pkg/errcode"
)

func TestAutomationBatchesContinueAndRedact(t *testing.T) {
	const sensitive = "fixture-sensitive-sql-device-secret"
	now := time.Now()
	var executed []string
	execute := func(id string) error {
		executed = append(executed, id)
		if id == "first" {
			return errors.New(sensitive)
		}
		return nil
	}
	var expired []string
	err := executeOnceBatch([]*model.OneTimeTask{
		{SceneAutomationID: "first"},
		{ID: "expired", ExpirationTime: 1, ExecutionTime: now.Add(-2 * time.Minute)},
		{SceneAutomationID: "second"},
	}, execute, func(ids []string) error { expired = append(expired, ids...); return nil })
	if !errors.Is(err, errAutomationBatch) || strings.Contains(err.Error(), sensitive) || !reflect.DeepEqual(executed, []string{"first", "second"}) || !reflect.DeepEqual(expired, []string{"expired"}) {
		t.Fatalf("once batch executed=%v expired=%v error=%v", executed, expired, err)
	}
	executed = nil
	err = executePeriodicBatch([]*model.PeriodicTask{{SceneAutomationID: "first"}, {SceneAutomationID: "second"}}, execute)
	if !errors.Is(err, errAutomationBatch) || strings.Contains(err.Error(), sensitive) || !reflect.DeepEqual(executed, []string{"first", "second"}) {
		t.Fatalf("periodic batch executed=%v error=%v", executed, err)
	}
	err = executeOnceBatch(nil, execute, func([]string) error { return errors.New(sensitive) })
	if !errors.Is(err, errAutomationBatch) || strings.Contains(err.Error(), sensitive) {
		t.Fatalf("expiration failure error=%v", err)
	}
	// Aggregates must not retain the mode-gate code that cron intentionally suppresses.
	err = executePeriodicBatch([]*model.PeriodicTask{{SceneAutomationID: "gate"}}, func(string) error { return errcode.New(100001) })
	var code *errcode.Error
	if !errors.Is(err, errAutomationBatch) || errors.As(err, &code) {
		t.Fatalf("batch was mistaken for a quiet mode gate: %v", err)
	}
}
