package uplink

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"project/internal/brokerledger"
	"project/internal/diagnostics"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/uplinkbinding"
	"strings"
)

func prepareBusMessage(ctx context.Context, db *gorm.DB, contracts uplinkbinding.ContractReader, decoder processor.DataProcessor, msg *DeviceMessage, logger *logrus.Logger) (*preparedUplinkBatch, error) {
	trusted, transport, _, _, _, err := trustedTelemetryMetadata(msg)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("uplink database unavailable")
	}
	deviceID, ok := msg.Metadata["device_id"].(string)
	if !ok || deviceID == "" || trusted && (deviceID != transport || msg.DeviceID != transport || msg.TenantID == "") {
		return nil, errors.New("uplink device identity mismatch")
	}
	var device model.Device
	q := db.WithContext(ctx).Where("id=? AND is_enabled='enabled'", deviceID)
	if trusted {
		q = q.Where("tenant_id=?", msg.TenantID)
	}
	if err = q.Take(&device).Error; err != nil {
		return nil, err
	}
	p := &ReliableProcessor{decoder: decoder, logger: logger, contracts: contracts}
	if msg.observationID == "" {
		msg.observationID = uuid.NewString()
		if id, ok := msg.Metadata["ingestion_id"].(string); ok && len(id) > 0 && len(id) <= 128 {
			msg.observationID = id
		}
	}
	batch, err := p.prepareMessages(ctx, db, brokerledger.Message{TenantID: device.TenantID, DeviceID: device.ID, Stream: msg.Type, Payload: msg.Payload, Metadata: map[string]any{"timestamp": msg.Timestamp}}, false)
	observations := batchObservations(batch, device.TenantID, device.ID, msg.observationID, strings.TrimPrefix(msg.Type, "gateway_"))
	observePending(observations)
	if err != nil {
		observeMessages(ctx, observations, diagnostics.LogicalRejected, preparationFailureReason(err))

	} else if len(batch.rejected) > 0 {
		updates := make([]diagnostics.LogicalUpdate, 0, len(batch.rejected))
		for _, entry := range batch.rejected {
			updates = append(updates, diagnostics.LogicalUpdate{Message: &diagnostics.LogicalMessage{TenantID: device.TenantID, DeviceID: entry.device.ID, ID: msg.observationID, Family: batch.family}, State: diagnostics.LogicalRejected, Reason: entry.rejection})
		}
		diagnostics.GetInstance().RecordLogicalBatch(ctx, updates)
	}

	return batch, err
}

func preparedBusMessage(original *DeviceMessage, batch *preparedUplinkBatch, deviceID string) *DeviceMessage {
	copy := *original
	copy.preparedBindings = batch.pathBindings(deviceID)
	return &copy
}
