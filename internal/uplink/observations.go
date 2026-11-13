package uplink

import (
	"context"
	"project/internal/brokerledger"
	"project/internal/diagnostics"
)

func batchObservations(batch *preparedUplinkBatch, tenant, root, id, family string) []*diagnostics.LogicalMessage {
	var result []*diagnostics.LogicalMessage
	if batch != nil {
		entries := append(append([]preparedLogicalUplink(nil), batch.entries...), batch.rejected...)
		for _, entry := range entries {
			result = append(result, &diagnostics.LogicalMessage{TenantID: tenant, DeviceID: entry.device.ID, ID: id, Family: family})
		}
	}
	if batch == nil {
		result = append(result, &diagnostics.LogicalMessage{TenantID: tenant, DeviceID: root, ID: id, Family: family})
	}
	return result
}

func observeMessages(ctx context.Context, observations []*diagnostics.LogicalMessage, state diagnostics.LogicalState, reason string) {
	updates := make([]diagnostics.LogicalUpdate, 0, len(observations))
	for _, o := range observations {
		updates = append(updates, diagnostics.LogicalUpdate{Message: o, State: state, Reason: reason})
	}
	diagnostics.GetInstance().RecordLogicalBatch(ctx, updates)
}
func observePending(observations []*diagnostics.LogicalMessage) {
	observeMessages(context.Background(), observations, diagnostics.LogicalPending, "")
}
func receiptObserver(observations []*diagnostics.LogicalMessage, preparationFailure string) func(context.Context, brokerledger.Message, brokerledger.ReceiptOutcome) {
	return func(ctx context.Context, _ brokerledger.Message, outcome brokerledger.ReceiptOutcome) {
		state, reason := diagnostics.LogicalCommitted, ""
		switch outcome {
		case brokerledger.OutcomeRejected:
			if preparationFailure != "" {
				state, reason = diagnostics.LogicalRejected, preparationFailure
			} else {
				state, reason = diagnostics.LogicalStorageFailed, "数据写入未提交"
			}
		case brokerledger.OutcomeFenced:
			state, reason = diagnostics.LogicalRejected, "设备接入状态已变化"
		}
		observeMessages(ctx, observations, state, reason)
	}
}
