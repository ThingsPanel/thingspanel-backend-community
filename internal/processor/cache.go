package processor

import (
	"context"
	"encoding/json"
	"time"

	"project/internal/dal"
	"project/pkg/global"

	"github.com/sirupsen/logrus"
)

// ScriptCache 脚本缓存管理器
type ScriptCache struct {
	// 使用全局 Redis 客户端
}

// NewScriptCache 创建脚本缓存管理器
func NewScriptCache() *ScriptCache {
	return &ScriptCache{}
}

// GetScript 获取脚本（优先从缓存读取，缓存未命中则从数据库加载）
// deviceConfigID: 设备配置ID
// scriptType: 脚本类型（A/B/C/D/E/F）
func (c *ScriptCache) GetScript(ctx context.Context, deviceConfigID, scriptType string) (*CachedScript, error) {
	// 1. 生成缓存 key
	cacheKey := GetCacheKey(deviceConfigID, scriptType)

	// 2. 尝试从 Redis 缓存读取
	cached, err := c.getFromCache(ctx, cacheKey)
	if err == nil && cached != nil {
		logrus.WithFields(logrus.Fields{
			"module":           "processor.cache",
			"device_config_id": deviceConfigID,
			"script_type":      scriptType,
			"cache_key":        cacheKey,
		}).Debug("script cache hit")
		return cached, nil
	}

	// 3. 缓存未命中，从数据库加载
	logrus.WithFields(logrus.Fields{
		"module":           "processor.cache",
		"device_config_id": deviceConfigID,
		"script_type":      scriptType,
		"cache_key":        cacheKey,
	}).Debug("script cache miss, loading from database")

	script, err := c.loadFromDatabase(deviceConfigID, scriptType)
	if err != nil {
		return nil, err
	}

	// 4. 脚本不存在
	if script == nil {
		return nil, NewScriptNotFoundError(deviceConfigID, scriptType)
	}

	// 5. 将脚本写入缓存（永久有效）
	if err := c.setToCache(ctx, cacheKey, script); err != nil {
		// 缓存写入失败不影响主流程，只记录日志
		logrus.WithFields(logrus.Fields{
			"module":    "processor.cache",
			"cache_key": cacheKey,
			"error":     err.Error(),
		}).Warn("failed to cache script")
	}

	return script, nil
}

// InvalidateCache 使指定脚本缓存失效（脚本更新/删除/禁用时调用）
func (c *ScriptCache) InvalidateCache(ctx context.Context, deviceConfigID, scriptType string) error {
	cacheKey := GetCacheKey(deviceConfigID, scriptType)

	err := global.REDIS.Del(ctx, cacheKey).Err()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"module":           "processor.cache",
			"device_config_id": deviceConfigID,
			"script_type":      scriptType,
			"cache_key":        cacheKey,
			"error":            err.Error(),
		}).Error("failed to invalidate cache")
		return NewCacheError(err)
	}

	logrus.WithFields(logrus.Fields{
		"module":           "processor.cache",
		"device_config_id": deviceConfigID,
		"script_type":      scriptType,
		"cache_key":        cacheKey,
	}).Info("script cache invalidated")

	return nil
}

// getFromCache 从 Redis 缓存读取脚本
func (c *ScriptCache) getFromCache(ctx context.Context, cacheKey string) (*CachedScript, error) {
	result, err := global.REDIS.Get(ctx, cacheKey).Result()
	if err != nil {
		// 缓存不存在或其他错误
		return nil, err
	}

	var script CachedScript
	if err := json.Unmarshal([]byte(result), &script); err != nil {
		logrus.WithFields(logrus.Fields{
			"module":    "processor.cache",
			"cache_key": cacheKey,
			"error":     err.Error(),
		}).Error("failed to unmarshal cached script")
		return nil, err
	}

	return &script, nil
}

// setToCache 将脚本写入 Redis 缓存（永久有效）
func (c *ScriptCache) setToCache(ctx context.Context, cacheKey string, script *CachedScript) error {
	data, err := json.Marshal(script)
	if err != nil {
		return err
	}

	// 永久缓存（TTL = 0）
	err = global.REDIS.Set(ctx, cacheKey, data, 0).Err()
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"module":      "processor.cache",
		"cache_key":   cacheKey,
		"script_id":   script.ID,
		"script_type": script.ScriptType,
	}).Info("script cached successfully")

	return nil
}

// loadFromDatabase 从数据库加载脚本
func (c *ScriptCache) loadFromDatabase(deviceConfigID, scriptType string) (*CachedScript, error) {
	// 调用 DAL 层查询脚本
	script, err := dal.GetDataScriptByDeviceConfigIdAndScriptType(&deviceConfigID, scriptType)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"module":           "processor.cache",
			"device_config_id": deviceConfigID,
			"script_type":      scriptType,
			"error":            err.Error(),
		}).Error("failed to load script from database")
		return nil, NewDatabaseError(err)
	}

	// 脚本不存在
	if script == nil {
		return nil, nil
	}

	// 转换为 CachedScript 结构
	cached := &CachedScript{
		ID:         script.ID,
		Content:    *script.Content,
		EnableFlag: script.EnableFlag,
		ScriptType: script.ScriptType,
	}

	logrus.WithFields(logrus.Fields{
		"module":           "processor.cache",
		"device_config_id": deviceConfigID,
		"script_type":      scriptType,
		"script_id":        cached.ID,
		"enable_flag":      cached.EnableFlag,
	}).Info("script loaded from database")

	return cached, nil
}

// PreloadScripts 预加载指定设备配置的所有脚本（可选，用于启动时预热缓存）
func (c *ScriptCache) PreloadScripts(ctx context.Context, deviceConfigID string) error {
	scriptTypes := []string{
		ScriptTypeTelemetryUplink,
		ScriptTypeTelemetryDownlink,
		ScriptTypeAttributeUplink,
		ScriptTypeAttributeDownlink,
		ScriptTypeCommand,
		ScriptTypeEvent,
	}

	startTime := time.Now()
	successCount := 0
	failCount := 0

	for _, scriptType := range scriptTypes {
		_, err := c.GetScript(ctx, deviceConfigID, scriptType)
		if err != nil {
			// 脚本不存在是正常情况（不是所有类型都有脚本）
			if _, ok := err.(*ProcessorError); ok && err.(*ProcessorError).Code == ErrCodeScriptNotFound {
				continue
			}
			failCount++
			logrus.WithFields(logrus.Fields{
				"module":           "processor.cache",
				"device_config_id": deviceConfigID,
				"script_type":      scriptType,
				"error":            err.Error(),
			}).Warn("failed to preload script")
			continue
		}
		successCount++
	}

	logrus.WithFields(logrus.Fields{
		"module":           "processor.cache",
		"device_config_id": deviceConfigID,
		"success_count":    successCount,
		"fail_count":       failCount,
		"duration_ms":      time.Since(startTime).Milliseconds(),
	}).Info("scripts preloaded")

	return nil
}
