package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"project/internal/diagnostics"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// telemetryWriter 遥测数据批量写入器
type telemetryWriter struct {
	db      *gorm.DB
	logger  Logger
	config  Config
	metrics *metricsCollector

	buffer   []*telemetryBatchItem // 批次缓冲区
	bufferMu sync.Mutex            // 缓冲区锁

	flushTicker *time.Ticker  // 定时刷新定时器
	stopCh      chan struct{} // 停止信号
	doneCh      chan struct{} // 完成信号
}

// telemetryBatchItem 批次项
type telemetryBatchItem struct {
	deviceID  string               // 设备ID
	tenantID  string               // 租户ID
	timestamp int64                // 时间戳（毫秒）
	points    []TelemetryDataPoint // 遥测数据点列表
}

// newTelemetryWriter 创建遥测数据写入器
func newTelemetryWriter(db *gorm.DB, logger Logger, config Config, metrics *metricsCollector) *telemetryWriter {
	return &telemetryWriter{
		db:      db,
		logger:  logger,
		config:  config,
		metrics: metrics,
		buffer:  make([]*telemetryBatchItem, 0, config.TelemetryBatchSize),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// start 启动写入器
func (w *telemetryWriter) start(ctx context.Context) {
	flushDuration := w.config.GetFlushDuration()
	// 如果配置了定时flush，启动定时器和后台协程
	if flushDuration > 0 {
		w.flushTicker = time.NewTicker(flushDuration)
		go w.run(ctx)
	}
}

// run 运行后台flush任务
func (w *telemetryWriter) run(ctx context.Context) {
	defer close(w.doneCh)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("telemetry writer context cancelled")
			return
		case <-w.stopCh:
			w.logger.Info("telemetry writer stopped")
			w.flushRemaining() // 停止前刷新剩余数据
			return
		case <-w.flushTicker.C:
			w.flush() // 定时刷新
		}
	}
}

// stop 停止写入器
func (w *telemetryWriter) stop(timeout time.Duration) {
	close(w.stopCh)

	if w.flushTicker != nil {
		w.flushTicker.Stop()
	}

	// 等待完成或超时
	select {
	case <-w.doneCh:
		w.logger.Info("telemetry writer stopped gracefully")
	case <-time.After(timeout):
		w.logger.Warn("telemetry writer stop timeout")
	}
}

// write 写入遥测消息
func (w *telemetryWriter) write(msg *Message) error {
	// 尝试转换为遥测数据点列表
	points, ok := msg.Data.([]TelemetryDataPoint)
	if !ok {
		// 如果不是直接的切片，尝试从interface{}切片转换
		if dataSlice, ok := msg.Data.([]interface{}); ok {
			points = make([]TelemetryDataPoint, 0, len(dataSlice))
			for _, item := range dataSlice {
				if point, ok := item.(TelemetryDataPoint); ok {
					points = append(points, point)
				}
			}
		}
		if len(points) == 0 {
			return fmt.Errorf("invalid telemetry data format")
		}
	}

	// 创建批次项
	item := &telemetryBatchItem{
		deviceID:  msg.DeviceID,
		tenantID:  msg.TenantID,
		timestamp: msg.Timestamp,
		points:    points,
	}

	// 加入缓冲区，检查是否需要刷新
	w.bufferMu.Lock()
	w.buffer = append(w.buffer, item)
	shouldFlush := len(w.buffer) >= w.config.TelemetryBatchSize
	w.bufferMu.Unlock()

	// 如果缓冲区满了，立即刷新
	if shouldFlush {
		w.flush()
	}

	return nil
}

// flush 刷新缓冲区
func (w *telemetryWriter) flush() {
	w.bufferMu.Lock()
	if len(w.buffer) == 0 {
		w.bufferMu.Unlock()
		return
	}

	// 取出当前批次，创建新缓冲区
	batch := w.buffer
	w.buffer = make([]*telemetryBatchItem, 0, w.config.TelemetryBatchSize)
	w.bufferMu.Unlock()

	w.doFlush(batch)
}

// flushRemaining 刷新剩余数据（停止时调用）
func (w *telemetryWriter) flushRemaining() {
	w.bufferMu.Lock()
	batch := w.buffer
	w.buffer = nil
	w.bufferMu.Unlock()

	if len(batch) > 0 {
		w.logger.Infof("flushing remaining %d telemetry items", len(batch))
		w.doFlush(batch)
	}
}

// doFlush 执行实际的刷新操作
func (w *telemetryWriter) doFlush(batch []*telemetryBatchItem) {
	// 1. 批次内去重并转换为数据库模型
	historyData, currentData, duplicates := w.deduplicateAndConvert(batch)

	// 记录批次内重复数
	if duplicates > 0 {
		w.metrics.addTelemetryDuplicates(int64(duplicates))
	}

	if len(historyData) == 0 {
		return
	}

	// 2. 批量写入数据库
	written, failed := w.batchInsert(historyData, currentData)

	// 3. 记录监控指标
	w.metrics.addTelemetryWritten(int64(written))
	w.metrics.addTelemetryFailed(int64(failed))
	w.metrics.recordTelemetryBatch(len(historyData))

	w.logger.Debugf("【设备诊断】flushed batch: total=%d, written=%d, failed=%d, duplicates=%d",
		len(historyData), written, failed, duplicates)
}

// deduplicateAndConvert 批次内去重并转换为数据库模型
func (w *telemetryWriter) deduplicateAndConvert(batch []*telemetryBatchItem) (
	[]TelemetryData, []TelemetryCurrentData, int) {

	seen := make(map[string]struct{})
	historyData := make([]TelemetryData, 0, len(batch)*2)
	currentMap := make(map[string]*TelemetryCurrentData)
	duplicates := 0

	for _, item := range batch {
		for _, point := range item.points {
			// 生成唯一键：device_id|key|timestamp
			key := fmt.Sprintf("%s|%s|%d", item.deviceID, point.Key, item.timestamp)

			// 检查是否重复
			if _, exists := seen[key]; exists {
				duplicates++
				continue
			}
			seen[key] = struct{}{}

			// 根据值类型转换为对应字段
			boolV, numberV, stringV := convertValue(point.Value)

			// 构建历史数据（时间戳为int64毫秒）
			historyData = append(historyData, TelemetryData{
				DeviceID: item.deviceID,
				Key:      point.Key,
				TS:       item.timestamp,
				BoolV:    boolV,
				NumberV:  numberV,
				StringV:  stringV,
				TenantID: item.tenantID,
			})

			// 构建最新值数据（时间戳为time.Time）
			currentKey := fmt.Sprintf("%s|%s", item.deviceID, point.Key)
			ts := time.UnixMilli(item.timestamp)

			if existing, ok := currentMap[currentKey]; !ok || ts.After(existing.TS) {
				currentMap[currentKey] = &TelemetryCurrentData{
					DeviceID: item.deviceID,
					Key:      point.Key,
					TS:       ts,
					BoolV:    boolV,
					NumberV:  numberV,
					StringV:  stringV,
					TenantID: item.tenantID,
				}
			}
		}
	}

	currentData := make([]TelemetryCurrentData, 0, len(currentMap))
	for _, data := range currentMap {
		currentData = append(currentData, *data)
	}

	return historyData, currentData, duplicates
}

// batchInsert 批量插入数据库
func (w *telemetryWriter) batchInsert(historyData []TelemetryData, currentData []TelemetryCurrentData) (written, failed int) {
	// 使用事务同时写入历史表和最新值表
	err := w.db.Transaction(func(tx *gorm.DB) error {
		// 插入历史表 - 遇到重复键则忽略（DO NOTHING）
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "device_id"}, {Name: "key"}, {Name: "ts"}},
			DoNothing: true,
		}).Create(&historyData).Error; err != nil {
			return fmt.Errorf("insert history data failed: %w", err)
		}

		// 插入最新值表 - 遇到重复键则更新（DO UPDATE）
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"ts", "bool_v", "number_v", "string_v", "tenant_id",
			}),
		}).Create(&currentData).Error; err != nil {
			return fmt.Errorf("insert current data failed: %w", err)
		}

		return nil
	})

	if err != nil {
		// 批量插入失败，降级为逐条插入
		w.logger.Errorf("batch insert failed: %v, fallback to single insert", err)
		return w.fallbackInsert(historyData, currentData)
	}

	return len(historyData), 0
}

// fallbackInsert 逐条插入兜底（批量失败时使用）
func (w *telemetryWriter) fallbackInsert(historyData []TelemetryData, currentData []TelemetryCurrentData) (written, failed int) {
	for i := range historyData {
		// 逐条使用事务插入
		err := w.db.Transaction(func(tx *gorm.DB) error {
			// 插入历史表
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "device_id"}, {Name: "key"}, {Name: "ts"}},
				DoNothing: true,
			}).Create(&historyData[i]).Error; err != nil {
				return err
			}

			// 插入最新值表
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"ts", "bool_v", "number_v", "string_v", "tenant_id",
				}),
			}).Create(&currentData[i]).Error; err != nil {
				return err
			}

			return nil
		})

		if err != nil {
			w.logger.Errorf("single insert failed: %v", err)
			// 记录诊断：存储失败（每条失败记录到对应设备）
			diagnostics.GetInstance().RecordStorageFailed(historyData[i].DeviceID, fmt.Sprintf("存储失败：%v", err))
			failed++
		} else {
			written++
		}
	}

	return written, failed
}

// convertValue 转换值类型到对应的数据库字段
// 返回: bool值指针, number值指针, string值指针
func convertValue(value interface{}) (*bool, *float64, *string) {
	switch v := value.(type) {
	case bool:
		return &v, nil, nil
	case int:
		f := float64(v)
		return nil, &f, nil
	case int32:
		f := float64(v)
		return nil, &f, nil
	case int64:
		f := float64(v)
		return nil, &f, nil
	case float32:
		f := float64(v)
		return nil, &f, nil
	case float64:
		return nil, &v, nil
	case string:
		return nil, nil, &v
	default:
		// 对于 map、slice 等复杂类型，序列化为 JSON 字符串
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			// 序列化失败时降级为 fmt.Sprintf
			s := fmt.Sprintf("%v", v)
			return nil, nil, &s
		}
		s := string(jsonBytes)
		return nil, nil, &s
	}
}
