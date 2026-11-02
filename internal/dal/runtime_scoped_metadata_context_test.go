package dal

import (
	"context"
	"errors"
	"testing"

	"project/internal/model"
)

// An already cancelled request must fail before touching Redis or issuing SQL.
// The scoped config path must also work with an uninitialized Redis client.
func TestRuntimeMetadataReadsRespectCancellation(t *testing.T) {
	_, cleanup := setupRawDeviceDataDB(t, true)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		read func() error
	}{
		{"device", func() error { _, err := GetDeviceByID("device", ctx); return err }},
		{"detail", func() error { _, err := GetDeviceDetail("device", ctx); return err }},
		{"config bypasses Redis", func() error { _, err := GetDeviceConfigByID("config", ctx); return err }},
		{"alarm", func() error { _, err := GetDeviceLatestAlarmStatus("device", ctx); return err }},
		{"attribute list", func() error { _, err := GetAttributeDataListWithDeviceName("device", ctx); return err }},
		{"attribute key", func() error { _, err := GetAttributeDataByKey(model.GetDataListByKeyReq{}, ctx); return err }},
		{"attribute logs", func() error {
			_, _, err := GetAttributeSetLogsDataListByPage(model.GetAttributeSetLogsListByPageReq{}, ctx)
			return err
		}},
		{"event", func() error {
			_, _, err := GetEventDatasListByPage(&model.GetEventDatasListByPageReq{}, ctx)
			return err
		}},
		{"status history", func() error {
			_, _, err := GetDeviceStatusHistoryByPage(&model.GetDeviceStatusHistoryReq{}, "tenant", ctx)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.read(); !errors.Is(err, context.Canceled) {
				t.Fatalf("want cancellation, got %v", err)
			}
		})
	}
}
