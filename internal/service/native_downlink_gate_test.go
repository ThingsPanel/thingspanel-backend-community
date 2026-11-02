package service

import (
	"testing"

	"project/internal/model"
)

func TestNativeLegacyControlGateRejectsAnyNativeReference(t *testing.T) {
	modelID, snapshotID, revision := "model", "snapshot", 1
	for _, config := range []*model.DeviceConfig{
		{NativeThingModelID: &modelID},
		{NativeThingModelSnapshotID: &snapshotID},
		{NativeThingModelRevision: &revision},
		nativeCommandDeviceConfig(modelID, snapshotID, revision),
	} {
		if nativeLegacyControlError(config) == nil {
			t.Fatal("native or partial native binding allowed a legacy control endpoint")
		}
	}
	for _, config := range []*model.DeviceConfig{nil, {}, {NativeBindingRevision: 5}} {
		if err := nativeLegacyControlError(config); err != nil {
			t.Fatalf("legacy or intentionally unbound config rejected: %v", err)
		}
	}
}
