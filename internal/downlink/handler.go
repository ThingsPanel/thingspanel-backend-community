package downlink

import (
	"context"
	"fmt"
	"time"

	"project/internal/dal"
	"project/internal/processor"

	"github.com/sirupsen/logrus"
)

// Handler 下行处理器
type Handler struct {
	publisher MessagePublisher // ✨ 改为抽象接口
	processor processor.DataProcessor
	logger    *logrus.Logger
}

// NewHandler 创建处理器
func NewHandler(publisher MessagePublisher, processor processor.DataProcessor, logger *logrus.Logger) *Handler {
	return &Handler{
		publisher: publisher,
		processor: processor,
		logger:    logger,
	}
}

// HandleCommand 处理命令下发
func (h *Handler) HandleCommand(ctx context.Context, msg *Message) {
	h.handle(ctx, msg, processor.DataTypeCommand)
}

// HandleAttributeSet 处理属性设置
func (h *Handler) HandleAttributeSet(ctx context.Context, msg *Message) {
	h.handle(ctx, msg, processor.DataTypeAttributeSet)
}

// HandleAttributeGet 处理属性获取
func (h *Handler) HandleAttributeGet(ctx context.Context, msg *Message) {
	// 属性获取也使用属性下发的脚本处理（ScriptTypeAttributeDownlink）
	h.handle(ctx, msg, processor.DataTypeAttributeSet)
}

// HandleTelemetry 处理遥测数据下发
func (h *Handler) HandleTelemetry(ctx context.Context, msg *Message) {
	h.handle(ctx, msg, processor.DataTypeTelemetryControl)
}

// handle 通用处理逻辑
func (h *Handler) handle(ctx context.Context, msg *Message, dataType processor.DataType) {
	start := time.Now()

	// 1. 参数验证（Topic 由 Adapter 构造，不在此验证）
	if msg == nil || msg.DeviceNumber == "" || len(msg.Data) == 0 {
		h.logger.WithFields(logrus.Fields{
			"module": "downlink",
			"error":  ErrInvalidMessage,
		}).Error("invalid message")

		// ✨ 修复：添加 msg.Type 参数
		if msg != nil && msg.MessageID != "" {
			h.updateLogStatus(msg.MessageID, msg.DeviceID, "2", "invalid message parameters", msg.Type) // ✨ 传递 device_id
		}
		return
	}

	// 2. 脚本编码
	var encodedData []byte
	if msg.DeviceConfigID != "" {
		// 有配置，执行脚本编码
		encodeInput := &processor.EncodeInput{
			DeviceConfigID: msg.DeviceConfigID,
			Type:           dataType,
			Data:           msg.Data,
			Timestamp:      time.Now().UnixMilli(),
		}

		encodeOutput, err := h.processor.Encode(ctx, encodeInput)
		if err != nil {
			h.logger.WithFields(logrus.Fields{
				"module":           "downlink",
				"device_config_id": msg.DeviceConfigID,
				"type":             dataType,
				"error":            err,
			}).Error("encode failed")

			// ✨ 更新日志为失败
			h.updateLogStatus(msg.MessageID, msg.DeviceID, "2", fmt.Sprintf("encode failed: %v", err), msg.Type) // ✨ 传递 device_id
			return
		}

		if !encodeOutput.Success {
			h.logger.WithFields(logrus.Fields{
				"module":           "downlink",
				"device_config_id": msg.DeviceConfigID,
				"type":             dataType,
				"error":            encodeOutput.Error,
			}).Error("encode execution failed")

			// ✨ 更新日志为失败
			h.updateLogStatus(msg.MessageID, msg.DeviceID, "2", fmt.Sprintf("script execution failed: %v", encodeOutput.Error), msg.Type) // ✨ 传递 device_id
			return
		}

		encodedData = encodeOutput.EncodedData
	} else {
		// 无配置，直接使用原始数据
		encodedData = msg.Data

		h.logger.WithFields(logrus.Fields{
			"module":    "downlink",
			"device_id": msg.DeviceID,
			"type":      dataType,
		}).Debug("no device config, using raw data")
	}

	// 3. 发布消息
	err := h.publishMessage(msg, encodedData)
	if err != nil {
		h.logger.WithFields(logrus.Fields{
			"module":        "downlink",
			"device_id":     msg.DeviceID,
			"device_number": msg.DeviceNumber,
			"device_type":   msg.DeviceType,
			"error":         err,
			"duration_ms":   time.Since(start).Milliseconds(),
		}).Error("message publish failed")

		// ✨ 更新日志为失败
		h.updateLogStatus(msg.MessageID, msg.DeviceID, "2", fmt.Sprintf("publish failed: %v", err), msg.Type) // ✨ 传递 device_id
		return
	}

	// 4. 发送成功
	h.updateLogStatus(msg.MessageID, msg.DeviceID, "1", "", msg.Type) // ✨ 传递 device_id

	// 5. 成功日志
	h.logger.WithFields(logrus.Fields{
		"module":      "downlink",
		"device_id":   msg.DeviceID,
		"type":        dataType,
		"topic":       msg.Topic,
		"duration_ms": time.Since(start).Milliseconds(),
	}).Info("message published successfully")
}

// publishMessage 发布消息（协议无关命名）
func (h *Handler) publishMessage(msg *Message, payload []byte) error {
	if h.publisher == nil {
		return fmt.Errorf("message publisher not initialized")
	}

	// 调用PublishMessage接口，传递 messageID 参数
	if err := h.publisher.PublishMessage(msg.DeviceNumber, msg.Type, msg.DeviceType, msg.TopicPrefix, msg.MessageID, 1, payload); err != nil {
		h.logger.WithFields(logrus.Fields{
			"device_number": msg.DeviceNumber,
			"device_type":   msg.DeviceType,
			"msg_type":      msg.Type,
			"message_id":    msg.MessageID,
			"topic_prefix":  msg.TopicPrefix,
			"payload":       string(payload),
			"error":         err.Error(),
		}).Error("message publish failed, may not be delivered to device")
		return err
	}

	return nil
}

// updateLogStatus 更新日志状态（添加 device_id 参数）
// status: 0=pending, 1=sent, 2=failed
func (h *Handler) updateLogStatus(messageID, deviceID, status, errorMsg string, msgType MessageType) {
	if messageID == "" || deviceID == "" {
		return
	}

	// ✨ 根据消息类型查询不同的日志表

	switch msgType {
	case MessageTypeCommand:
		// 查询命令日志表
		log, err := dal.GetCommandSetLogByMessageID(messageID, deviceID)
		if err != nil {
			h.logger.WithError(err).WithFields(logrus.Fields{
				"message_id": messageID,
				"device_id":  deviceID,
			}).Warn("Failed to find command log")
			return
		}

		// 更新状态
		log.Status = &status
		if errorMsg != "" {
			log.ErrorMessage = &errorMsg
		}

		// 保存更新
		if err := dal.UpdateCommandSetLog(log); err != nil {
			h.logger.WithError(err).WithField("message_id", messageID).Error("Failed to update command log status")
		} else {
			h.logger.WithFields(logrus.Fields{
				"message_id": messageID,
				"device_id":  deviceID,
				"status":     status,
				"type":       "command",
			}).Debug("Command log status updated")
		}

	case MessageTypeAttributeSet:
		// 查询属性设置日志表
		log, err := dal.GetAttributeSetLogByMessageID(messageID, deviceID)
		if err != nil {
			h.logger.WithError(err).WithFields(logrus.Fields{
				"message_id": messageID,
				"device_id":  deviceID,
			}).Warn("Failed to find attribute log")
			return
		}

		// 更新状态
		log.Status = &status
		if errorMsg != "" {
			log.ErrorMessage = &errorMsg
		}

		// 保存更新
		if err := dal.UpdateAttributeSetLog(log); err != nil {
			h.logger.WithError(err).WithField("message_id", messageID).Error("Failed to update attribute log status")
		} else {
			h.logger.WithFields(logrus.Fields{
				"message_id": messageID,
				"device_id":  deviceID,
				"status":     status,
				"type":       "attribute_set",
			}).Debug("Attribute log status updated")
		}

	case MessageTypeTelemetry:
		// 查询遥测下发日志表（使用日志ID作为MessageID）
		log, err := dal.GetTelemetrySetLogByID(messageID)
		if err != nil {
			h.logger.WithError(err).WithField("log_id", messageID).Warn("Failed to find telemetry log")
			return
		}

		// 更新状态
		log.Status = &status
		if errorMsg != "" {
			log.ErrorMessage = &errorMsg
		}

		// 保存更新
		if err := dal.UpdateTelemetrySetLog(log); err != nil {
			h.logger.WithError(err).WithField("log_id", messageID).Error("Failed to update telemetry log status")
		} else {
			h.logger.WithFields(logrus.Fields{
				"log_id": messageID,
				"status": status,
				"type":   "telemetry",
			}).Debug("Telemetry log status updated")
		}

	default:
		h.logger.WithFields(logrus.Fields{
			"message_id": messageID,
			"msg_type":   msgType,
		}).Warn("Unknown message type for log update")
	}
}
