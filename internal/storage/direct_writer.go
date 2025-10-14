package storage

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// directWriter 属性和事件直接写入器
type directWriter struct {
	db      *gorm.DB
	logger  Logger
	metrics *metricsCollector
}

func newDirectWriter(db *gorm.DB, logger Logger, metrics *metricsCollector) *directWriter {
	return &directWriter{
		db:      db,
		logger:  logger,
		metrics: metrics,
	}
}

func (w *directWriter) writeAttribute(msg *Message) error {
	points, ok := msg.Data.([]AttributeDataPoint)
	if !ok {
		if dataSlice, ok := msg.Data.([]interface{}); ok {
			points = make([]AttributeDataPoint, 0, len(dataSlice))
			for _, item := range dataSlice {
				if point, ok := item.(AttributeDataPoint); ok {
					points = append(points, point)
				}
			}
		}
		if len(points) == 0 {
			return fmt.Errorf("invalid attribute data format")
		}
	}

	for _, point := range points {
		if err := w.insertAttribute(msg, point); err != nil {
			w.logger.Errorf("insert attribute failed: %v", err)
			w.metrics.incAttributeFailed()
		} else {
			w.metrics.incAttributeWritten()
		}
	}

	return nil
}

func (w *directWriter) insertAttribute(msg *Message, point AttributeDataPoint) error {
	boolV, numberV, stringV := convertValue(point.Value)

	data := AttributeData{
		ID:       uuid.New().String(),
		DeviceID: msg.DeviceID,
		Key:      point.Key,
		TS:       time.UnixMilli(msg.Timestamp),
		BoolV:    boolV,
		NumberV:  numberV,
		StringV:  stringV,
		TenantID: msg.TenantID,
	}

	return w.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"ts", "bool_v", "number_v", "string_v", "tenant_id",
		}),
	}).Create(&data).Error
}

func (w *directWriter) writeEvent(msg *Message) error {
	eventData, ok := msg.Data.(EventData)
	if !ok {
		return fmt.Errorf("invalid event data format")
	}

	data := EventDataModel{
		ID:       uuid.New().String(),
		DeviceID: msg.DeviceID,
		Identify: eventData.Identify,
		TS:       time.UnixMilli(msg.Timestamp),
		Data:     eventData.Data,
		TenantID: msg.TenantID,
	}

	if err := w.db.Create(&data).Error; err != nil {
		w.logger.Errorf("insert event failed: %v", err)
		w.metrics.incEventFailed()
		return err
	}

	w.metrics.incEventWritten()
	return nil
}
