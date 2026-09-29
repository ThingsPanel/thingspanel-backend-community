package downlink

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"project/internal/model"
	"project/internal/query"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type commandStatusPublisher struct {
	beforeReturn func(messageID string) error
	returnErr    error
}

func (p commandStatusPublisher) PublishMessage(_ string, _ MessageType, _, _, messageID string, _ byte, _ []byte) error {
	if p.beforeReturn != nil {
		if err := p.beforeReturn(messageID); err != nil {
			return err
		}
	}
	return p.returnErr
}

func TestHandleCommandPreservesEarlyAcknowledgementStatus(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		ackMessage string
		result     int
		wantStatus string
	}{
		{name: "success ACK", status: "3", ackMessage: "HA service call succeeded", result: 0, wantStatus: "3"},
		{name: "failure ACK", status: "4", ackMessage: "HA service call failed", result: 1, wantStatus: "4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := useCommandStatusTestDB(t)
			createCommandStatusLog(t, db, "message-early", "0")
			rspDataBytes, err := json.Marshal(map[string]any{"result": tt.result, "message": tt.ackMessage, "ts": 123})
			if err != nil {
				t.Fatalf("marshal ACK response: %v", err)
			}
			rspData := string(rspDataBytes)
			h := newCommandStatusTestHandler(commandStatusPublisher{
				beforeReturn: func(messageID string) error {
					return db.Model(&model.CommandSetLog{}).
						Where("message_id = ? AND device_id = ?", messageID, "device-1").
						Updates(map[string]any{
							"status":        tt.status,
							"rsp_data":      rspData,
							"error_message": tt.ackMessage,
						}).Error
				},
			})

			h.HandleCommand(context.Background(), testCommandMessage("message-early"))

			got := loadCommandStatusLog(t, db, "message-early")
			if got.Status == nil || *got.Status != tt.wantStatus {
				t.Fatalf("status = %v, want %q", got.Status, tt.wantStatus)
			}
			if got.RspDatum == nil || *got.RspDatum != rspData {
				t.Errorf("rsp_data = %v, want %q", got.RspDatum, rspData)
			}
			if got.ErrorMessage == nil || *got.ErrorMessage != tt.ackMessage {
				t.Errorf("error_message = %v, want %q", got.ErrorMessage, tt.ackMessage)
			}
		})
	}
}

func TestHandleCommandUpdatesPendingPublishStatus(t *testing.T) {
	tests := []struct {
		name       string
		publishErr error
		wantStatus string
		wantError  string
	}{
		{name: "published", wantStatus: "1"},
		{name: "publish failed", publishErr: errors.New("broker unavailable"), wantStatus: "2", wantError: "publish failed: broker unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := useCommandStatusTestDB(t)
			createCommandStatusLog(t, db, "message-pending", "0")
			h := newCommandStatusTestHandler(commandStatusPublisher{returnErr: tt.publishErr})

			h.HandleCommand(context.Background(), testCommandMessage("message-pending"))

			got := loadCommandStatusLog(t, db, "message-pending")
			if got.Status == nil || *got.Status != tt.wantStatus {
				t.Fatalf("status = %v, want %q", got.Status, tt.wantStatus)
			}
			if got.ErrorMessage == nil || *got.ErrorMessage != tt.wantError {
				t.Errorf("error_message = %v, want %q", got.ErrorMessage, tt.wantError)
			}
		})
	}
}

func useCommandStatusTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:downlink-command-status?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&model.CommandSetLog{}); err != nil {
		t.Fatalf("migrate command logs: %v", err)
	}
	oldCommandSetLog := query.CommandSetLog
	query.CommandSetLog = &query.Use(db).CommandSetLog
	t.Cleanup(func() {
		query.CommandSetLog = oldCommandSetLog
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func createCommandStatusLog(t *testing.T, db *gorm.DB, messageID, status string) {
	t.Helper()
	if err := db.Create(&model.CommandSetLog{
		ID:        "log-" + messageID,
		DeviceID:  "device-1",
		MessageID: stringPointer(messageID),
		Status:    stringPointer(status),
	}).Error; err != nil {
		t.Fatalf("create command log: %v", err)
	}
}

func loadCommandStatusLog(t *testing.T, db *gorm.DB, messageID string) *model.CommandSetLog {
	t.Helper()
	var got model.CommandSetLog
	if err := db.Where("message_id = ?", messageID).First(&got).Error; err != nil {
		t.Fatalf("load command log: %v", err)
	}
	return &got
}

func newCommandStatusTestHandler(publisher MessagePublisher) *Handler {
	logger := logrus.New()
	logger.SetOutput(&discardWriter{})
	return NewHandler(publisher, nil, logger)
}

func testCommandMessage(messageID string) *Message {
	return &Message{
		DeviceID:     "device-1",
		DeviceNumber: "number-1",
		DeviceType:   "1",
		Type:         MessageTypeCommand,
		Data:         json.RawMessage(`{"switch":"on"}`),
		MessageID:    messageID,
	}
}

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func stringPointer(value string) *string { return &value }
