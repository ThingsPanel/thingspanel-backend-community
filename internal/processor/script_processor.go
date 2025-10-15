package processor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sirupsen/logrus"
)

// ScriptProcessor 基于 Lua 脚本的数据处理器实现
type ScriptProcessor struct {
	cache    *ScriptCache  // 脚本缓存管理器
	executor *LuaExecutor  // Lua 执行引擎
}

// NewScriptProcessor 创建脚本处理器实例
func NewScriptProcessor() *ScriptProcessor {
	return &ScriptProcessor{
		cache:    NewScriptCache(),
		executor: NewLuaExecutor(),
	}
}

// Decode 上行数据解码：设备协议数据 -> 标准化数据
func (p *ScriptProcessor) Decode(ctx context.Context, input *DecodeInput) (*DecodeOutput, error) {
	startTime := time.Now()

	// 1. 验证输入参数
	if err := input.Validate(); err != nil {
		logrus.WithFields(logrus.Fields{
			"module": "processor",
			"method": "Decode",
			"error":  err.Error(),
		}).Error("invalid input")
		return &DecodeOutput{
			Success:   false,
			Error:     err,
			Timestamp: time.Now().UnixMilli(),
		}, err
	}

	// 2. 获取 scriptType
	scriptType, ok := GetScriptType(input.Type)
	if !ok {
		err := NewInvalidInputError("unsupported data type: " + string(input.Type))
		logrus.WithFields(logrus.Fields{
			"module":    "processor",
			"method":    "Decode",
			"data_type": input.Type,
			"error":     err.Error(),
		}).Error("invalid data type")
		return &DecodeOutput{
			Success:   false,
			Error:     err,
			Timestamp: time.Now().UnixMilli(),
		}, err
	}

	// 3. 从缓存加载脚本
	script, err := p.cache.GetScript(ctx, input.DeviceConfigID, scriptType)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"module":           "processor",
			"method":           "Decode",
			"device_config_id": input.DeviceConfigID,
			"data_type":        input.Type,
			"script_type":      scriptType,
			"error":            err.Error(),
		}).Warn("failed to get script")
		return &DecodeOutput{
			Success:   false,
			Error:     err,
			Timestamp: time.Now().UnixMilli(),
		}, err
	}

	// 4. 检查脚本是否启用
	if !script.IsEnabled() {
		err := NewScriptDisabledError(input.DeviceConfigID, scriptType)
		logrus.WithFields(logrus.Fields{
			"module":           "processor",
			"method":           "Decode",
			"device_config_id": input.DeviceConfigID,
			"script_type":      scriptType,
			"script_id":        script.ID,
		}).Warn("script is disabled")
		return &DecodeOutput{
			Success:   false,
			Error:     err,
			Timestamp: time.Now().UnixMilli(),
		}, err
	}

	// 5. 执行脚本解码
	resultStr, err := p.executor.ExecuteDecode(ctx, script.Content, input.RawData)
	if err != nil {
		duration := time.Since(startTime)
		logrus.WithFields(logrus.Fields{
			"module":           "processor",
			"method":           "Decode",
			"device_config_id": input.DeviceConfigID,
			"data_type":        input.Type,
			"script_type":      scriptType,
			"script_id":        script.ID,
			"duration_ms":      duration.Milliseconds(),
			"error":            err.Error(),
		}).Error("script execution failed")
		return &DecodeOutput{
			Success:   false,
			Error:     err,
			Timestamp: time.Now().UnixMilli(),
		}, err
	}

	// 6. 将结果转换为 json.RawMessage
	data := json.RawMessage(resultStr)

	// 7. 记录成功日志
	duration := time.Since(startTime)
	logrus.WithFields(logrus.Fields{
		"module":           "processor",
		"method":           "Decode",
		"device_config_id": input.DeviceConfigID,
		"data_type":        input.Type,
		"script_type":      scriptType,
		"script_id":        script.ID,
		"duration_ms":      duration.Milliseconds(),
		"success":          true,
	}).Info("decode completed")

	return &DecodeOutput{
		Success:   true,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
		Error:     nil,
	}, nil
}

// Encode 下行数据编码：标准化数据 -> 设备协议数据
func (p *ScriptProcessor) Encode(ctx context.Context, input *EncodeInput) (*EncodeOutput, error) {
	startTime := time.Now()

	// 1. 验证输入参数
	if err := input.Validate(); err != nil {
		logrus.WithFields(logrus.Fields{
			"module": "processor",
			"method": "Encode",
			"error":  err.Error(),
		}).Error("invalid input")
		return &EncodeOutput{
			Success: false,
			Error:   err,
		}, err
	}

	// 2. 获取 scriptType
	scriptType, ok := GetScriptType(input.Type)
	if !ok {
		err := NewInvalidInputError("unsupported data type: " + string(input.Type))
		logrus.WithFields(logrus.Fields{
			"module":    "processor",
			"method":    "Encode",
			"data_type": input.Type,
			"error":     err.Error(),
		}).Error("invalid data type")
		return &EncodeOutput{
			Success: false,
			Error:   err,
		}, err
	}

	// 3. 从缓存加载脚本
	script, err := p.cache.GetScript(ctx, input.DeviceConfigID, scriptType)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"module":           "processor",
			"method":           "Encode",
			"device_config_id": input.DeviceConfigID,
			"data_type":        input.Type,
			"script_type":      scriptType,
			"error":            err.Error(),
		}).Warn("failed to get script")
		return &EncodeOutput{
			Success: false,
			Error:   err,
		}, err
	}

	// 4. 检查脚本是否启用
	if !script.IsEnabled() {
		err := NewScriptDisabledError(input.DeviceConfigID, scriptType)
		logrus.WithFields(logrus.Fields{
			"module":           "processor",
			"method":           "Encode",
			"device_config_id": input.DeviceConfigID,
			"script_type":      scriptType,
			"script_id":        script.ID,
		}).Warn("script is disabled")
		return &EncodeOutput{
			Success: false,
			Error:   err,
		}, err
	}

	// 5. 执行脚本编码
	resultStr, err := p.executor.ExecuteEncode(ctx, script.Content, input.Data)
	if err != nil {
		duration := time.Since(startTime)
		logrus.WithFields(logrus.Fields{
			"module":           "processor",
			"method":           "Encode",
			"device_config_id": input.DeviceConfigID,
			"data_type":        input.Type,
			"script_type":      scriptType,
			"script_id":        script.ID,
			"duration_ms":      duration.Milliseconds(),
			"error":            err.Error(),
		}).Error("script execution failed")
		return &EncodeOutput{
			Success: false,
			Error:   err,
		}, err
	}

	// 6. 将结果转换为字节数组
	encodedData := []byte(resultStr)

	// 7. 记录成功日志
	duration := time.Since(startTime)
	logrus.WithFields(logrus.Fields{
		"module":           "processor",
		"method":           "Encode",
		"device_config_id": input.DeviceConfigID,
		"data_type":        input.Type,
		"script_type":      scriptType,
		"script_id":        script.ID,
		"duration_ms":      duration.Milliseconds(),
		"success":          true,
	}).Info("encode completed")

	return &EncodeOutput{
		Success:     true,
		EncodedData: encodedData,
		Error:       nil,
	}, nil
}

// InvalidateScriptCache 使指定脚本缓存失效（供外部调用，脚本更新时使用）
func (p *ScriptProcessor) InvalidateScriptCache(ctx context.Context, deviceConfigID, scriptType string) error {
	// 清除 Redis 缓存
	return p.cache.InvalidateCache(ctx, deviceConfigID, scriptType)
}

// PreloadScripts 预加载脚本（可选，用于启动时预热缓存）
func (p *ScriptProcessor) PreloadScripts(ctx context.Context, deviceConfigID string) error {
	return p.cache.PreloadScripts(ctx, deviceConfigID)
}
