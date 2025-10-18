package flow

import (
	"context"
	"encoding/json"
	"time"

	"project/internal/query"

	"github.com/sirupsen/logrus"
)

// ResponseFlow 响应流处理器
// 负责处理命令和属性设置的响应消息，更新日志表
type ResponseFlow struct {
	logger *logrus.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// ResponseFlowConfig ResponseFlow 配置
type ResponseFlowConfig struct {
	Logger *logrus.Logger
}

// NewResponseFlow 创建响应流处理器
func NewResponseFlow(config ResponseFlowConfig) *ResponseFlow {
	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = logrus.StandardLogger()
	}

	return &ResponseFlow{
		logger: config.Logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 启动响应流处理
func (f *ResponseFlow) Start(input <-chan *DeviceMessage) error {
	f.logger.Info("ResponseFlow starting...")

	go func() {
		f.logger.Info("ResponseFlow message loop started")
		for {
			select {
			case <-f.ctx.Done():
				f.logger.Info("ResponseFlow stopped")
				return
			case msg := <-input:
				if msg == nil {
					f.logger.Warn("Received nil response message, skipping")
					continue
				}
				f.logger.WithFields(logrus.Fields{
					"device_id":  msg.DeviceID,
					"type":       msg.Type,
					"message_id": msg.Metadata["message_id"],
				}).Info("ResponseFlow received message")
				f.processMessage(msg)
			}
		}
	}()

	f.logger.Info("ResponseFlow started successfully")
	return nil
}

// Stop 停止响应流处理
func (f *ResponseFlow) Stop() error {
	f.cancel()
	return nil
}

// processMessage 处理响应消息
func (f *ResponseFlow) processMessage(msg *DeviceMessage) {
	// 1. 提取 message_id
	messageID, ok := msg.Metadata["message_id"].(string)
	if !ok || messageID == "" {
		f.logger.WithField("metadata", msg.Metadata).Error("Missing message_id in metadata")
		return
	}

	// 2. 解析响应数据
	responseData, success := f.parseResponse(msg.Payload)

	// 3. 根据消息类型更新对应日志表
	switch msg.Type {
	case MessageTypeCommandResponse, MessageTypeGatewayCommandResponse:
		f.updateCommandLog(messageID, success, responseData)

	case MessageTypeAttributeSetResponse, MessageTypeGatewayAttributeSetResponse:
		f.updateAttributeLog(messageID, success, responseData)

	default:
		f.logger.WithField("type", msg.Type).Warn("Unknown response type")
	}
}

// parseResponse 解析响应数据
// 返回: (错误信息, 是否成功)
func (f *ResponseFlow) parseResponse(payload []byte) (string, bool) {
	// 尝试解析响应格式
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Success bool   `json:"success"`
	}

	if err := json.Unmarshal(payload, &response); err != nil {
		// 解析失败，认为是原始响应数据
		f.logger.WithError(err).Debug("Failed to parse response as JSON, treating as raw data")
		return string(payload), true // 假设成功
	}

	// 判断成功/失败
	success := response.Code == 200 || response.Success

	errorMsg := ""
	if !success {
		errorMsg = response.Message
		if errorMsg == "" {
			errorMsg = string(payload)
		}
	}

	return errorMsg, success
}

// updateCommandLog 更新命令日志
func (f *ResponseFlow) updateCommandLog(messageID string, success bool, errorMsg string) {
	// 状态: 0=pending, 1=success, 2=failed
	status := "1" // 成功
	var errorMsgPtr *string

	if !success {
		status = "2" // 失败
		errorMsgPtr = &errorMsg
	}

	// 更新日志
	result, err := query.CommandSetLog.
		Where(query.CommandSetLog.MessageID.Eq(messageID)).
		Updates(map[string]interface{}{
			"status":        &status,
			"error_message": errorMsgPtr,
			"updated_at":    time.Now(),
		})

	if err != nil {
		f.logger.WithError(err).WithField("message_id", messageID).Error("Failed to update command log")
		return
	}

	if result.RowsAffected == 0 {
		f.logger.WithField("message_id", messageID).Warn("Command log not found")
		return
	}

	f.logger.WithFields(logrus.Fields{
		"message_id": messageID,
		"status":     status,
		"success":    success,
	}).Info("Command log updated")
}

// updateAttributeLog 更新属性设置日志
func (f *ResponseFlow) updateAttributeLog(messageID string, success bool, errorMsg string) {
	// 状态: 0=pending, 1=success, 2=failed
	status := "1" // 成功
	var errorMsgPtr *string

	if !success {
		status = "2" // 失败
		errorMsgPtr = &errorMsg
	}

	// 更新日志
	result, err := query.AttributeSetLog.
		Where(query.AttributeSetLog.MessageID.Eq(messageID)).
		Updates(map[string]interface{}{
			"status":        &status,
			"error_message": errorMsgPtr,
			"updated_at":    time.Now(),
		})

	if err != nil {
		f.logger.WithError(err).WithField("message_id", messageID).Error("Failed to update attribute log")
		return
	}

	if result.RowsAffected == 0 {
		f.logger.WithField("message_id", messageID).Warn("Attribute log not found")
		return
	}

	f.logger.WithFields(logrus.Fields{
		"message_id": messageID,
		"status":     status,
		"success":    success,
	}).Info("Attribute log updated")
}
