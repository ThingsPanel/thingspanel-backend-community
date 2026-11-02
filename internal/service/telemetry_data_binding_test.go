package service

import (
	"testing"

	"project/internal/model"
)

func TestShouldLoadLegacyTelemetryMetadataRespectsNativeBindingReferences(t *testing.T) {
	templateID := "legacy-template"
	modelID := "native-model"
	snapshotID := "native-snapshot"
	revision := 3
	tests := []struct {
		name   string
		config *model.DeviceConfig
		want   bool
	}{
		{name: "legacy template", config: &model.DeviceConfig{DeviceTemplateID: &templateID}, want: true},
		{name: "no template", config: &model.DeviceConfig{}},
		{name: "partial native model reference", config: &model.DeviceConfig{DeviceTemplateID: &templateID, NativeThingModelID: &modelID}},
		{name: "partial native snapshot reference", config: &model.DeviceConfig{DeviceTemplateID: &templateID, NativeThingModelSnapshotID: &snapshotID}},
		{name: "partial native revision reference", config: &model.DeviceConfig{DeviceTemplateID: &templateID, NativeThingModelRevision: &revision}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldLoadLegacyTelemetryMetadata(test.config); got != test.want {
				t.Fatalf("shouldLoadLegacyTelemetryMetadata()=%t, want %t", got, test.want)
			}
		})
	}
}
