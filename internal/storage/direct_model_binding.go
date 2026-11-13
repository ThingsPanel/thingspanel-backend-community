package storage

import (
	"context"
	"gorm.io/gorm"
	"project/internal/diagnostics"
	"time"
)

func (w *directWriter) writeFenced(msg *Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	innerMessage := *msg
	innerMessage.Bindings = nil
	innerMessage.AfterCommit = nil
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := checkTelemetryModelBindings(ctx, tx, &telemetryBatchItem{deviceID: msg.DeviceID, tenantID: msg.TenantID, bindings: msg.Bindings}); e != nil {
			return e
		}
		inner := newDirectWriter(tx, w.logger, newMetricsCollector())
		if msg.DataType == DataTypeAttribute {
			return inner.writeAttribute(&innerMessage)
		}
		return inner.writeEvent(&innerMessage)
	})
	if err != nil {
		msg.Observation.Record(diagnostics.LogicalStorageFailed, "数据写入未提交或绑定已变化")
		if msg.DataType == DataTypeAttribute {
			w.metrics.incAttributeFailed()
		} else {
			w.metrics.incEventFailed()
		}
		return err
	}
	if msg.DataType == DataTypeAttribute {
		w.metrics.incAttributeWritten()
	} else {
		w.metrics.incEventWritten()
	}
	msg.Observation.Record(diagnostics.LogicalCommitted, "")
	if msg.AfterCommit != nil {
		msg.AfterCommit()
	}
	return nil
}
