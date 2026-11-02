package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
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
	deviceID    string               // 设备ID
	tenantID    string               // 租户ID
	timestamp   int64                // 时间戳（毫秒）
	points      []TelemetryDataPoint // 遥测数据点列表
	route       *TrustedTelemetryRoute
	afterCommit func()
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
		deviceID:    msg.DeviceID,
		tenantID:    msg.TenantID,
		timestamp:   msg.Timestamp,
		points:      points,
		route:       msg.BrokerRoute,
		afterCommit: msg.AfterCommit,
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
	var legacy, trusted []*telemetryBatchItem
	for _, item := range batch {
		if item.route == nil {
			legacy = append(legacy, item)
		} else {
			trusted = append(trusted, item)
		}
	}
	if len(legacy) > 0 {
		w.doFlushLegacy(legacy)
	}
	if len(trusted) > 0 {
		w.doFlushTrusted(trusted)
	}
}

func (w *telemetryWriter) doFlushLegacy(batch []*telemetryBatchItem) {
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
	written, failed, unknown := w.batchInsert(historyData, currentData)

	// 3. 记录监控指标
	w.metrics.addTelemetryWritten(int64(written))
	w.metrics.addTelemetryFailed(int64(failed))
	w.metrics.addTelemetryUnknown(int64(unknown))
	w.metrics.recordTelemetryBatch(len(historyData))

	w.logger.Debugf("【设备诊断】flushed batch: total=%d, written=%d, failed=%d, unknown=%d, duplicates=%d",
		len(historyData), written, failed, unknown, duplicates)
}

// deduplicateAndConvert 批次内去重并转换为数据库模型
func (w *telemetryWriter) deduplicateAndConvert(batch []*telemetryBatchItem) (
	[]TelemetryData, []TelemetryCurrentData, int) {

	type historyKey struct {
		device, key string
		ts          int64
	}
	type currentKeyType struct{ device, key string }
	seen := make(map[historyKey]struct{})
	historyData := make([]TelemetryData, 0, len(batch)*2)
	currentMap := make(map[currentKeyType]*TelemetryCurrentData)
	duplicates := 0

	for _, item := range batch {
		for _, point := range item.points {
			// 生成唯一键：device_id|key|timestamp
			key := historyKey{item.deviceID, point.Key, item.timestamp}

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
			currentKey := currentKeyType{item.deviceID, point.Key}
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

// writeTelemetryWinners uses the caller-owned transaction; it does not commit.
func writeTelemetryWinners(tx *gorm.DB, points []TelemetryData) (int, error) {
	points = append([]TelemetryData(nil), points...)
	sort.Slice(points, func(i, j int) bool {
		if points[i].DeviceID != points[j].DeviceID {
			return points[i].DeviceID < points[j].DeviceID
		}
		if points[i].Key != points[j].Key {
			return points[i].Key < points[j].Key
		}
		return points[i].TS < points[j].TS
	})
	result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}, {Name: "ts"}}, DoNothing: true}).CreateInBatches(&points, 1000)
	err := result.Error
	if err == nil {
		type identity struct {
			device, key string
			ts          int64
		}
		expected := make(map[identity]bool, len(points))
		type currentIdentity struct{ device, key string }
		winners := make(map[currentIdentity]TelemetryData)
		for _, p := range points {
			expected[identity{p.DeviceID, p.Key, p.TS}] = false
		}
		for offset := 0; offset < len(points) && err == nil; offset += 1000 {
			end := offset + 1000
			if end > len(points) {
				end = len(points)
			}
			keys := make([][]interface{}, 0, end-offset)
			for _, p := range points[offset:end] {
				keys = append(keys, []interface{}{p.DeviceID, p.Key, p.TS})
			}
			var stored []TelemetryData
			err = tx.Where("(device_id, key, ts) IN ?", keys).Find(&stored).Error
			for _, p := range stored {
				id := identity{p.DeviceID, p.Key, p.TS}
				if _, ok := expected[id]; !ok {
					err = fmt.Errorf("unexpected telemetry history key")
					break
				}
				expected[id] = true
				key := currentIdentity{p.DeviceID, p.Key}
				if prior, ok := winners[key]; !ok || p.TS > prior.TS {
					winners[key] = p
				}
			}
		}
		for _, found := range expected {
			if !found && err == nil {
				err = fmt.Errorf("telemetry history winner missing")
			}
		}
		if err == nil {
			rows := make([]TelemetryCurrentData, 0, len(winners))
			for _, p := range winners {
				rows = append(rows, TelemetryCurrentData{DeviceID: p.DeviceID, Key: p.Key, TS: time.UnixMilli(p.TS), BoolV: p.BoolV, NumberV: p.NumberV, StringV: p.StringV, TenantID: p.TenantID})
			}
			sort.Slice(rows, func(i, j int) bool {
				if rows[i].DeviceID != rows[j].DeviceID {
					return rows[i].DeviceID < rows[j].DeviceID
				}
				return rows[i].Key < rows[j].Key
			})
			err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"ts", "bool_v", "number_v", "string_v", "tenant_id"}), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "telemetry_current_datas.ts <= excluded.ts"}}}}).CreateInBatches(&rows, 1000).Error
		}
	}
	return int(result.RowsAffected), err
}

// legacyWrite writes history first and reads the committed winner for every key.
// Statement failure is retryable only after a confirmed rollback; commit failure
// has an unknown outcome and must never trigger automatic fallback.
func (w *telemetryWriter) legacyWrite(points []TelemetryData) (int, bool, error) {
	tx := w.db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if tx.Error != nil {
		return 0, false, tx.Error
	}
	defer tx.Rollback()
	_, err := writeTelemetryWinners(tx, points)
	if err != nil {
		if rollbackErr := tx.Rollback().Error; rollbackErr != nil {
			return 0, true, fmt.Errorf("write: %v; rollback outcome unknown: %w", err, rollbackErr)
		}
		return 0, false, err
	}
	if err = tx.Commit().Error; err != nil {
		return 0, true, err
	}
	return len(points), false, nil
}

func (w *telemetryWriter) batchInsert(historyData []TelemetryData, _ []TelemetryCurrentData) (written, failed, unknown int) {
	written, uncertain, err := w.legacyWrite(historyData)
	if err == nil {
		return written, 0, 0
	}
	if uncertain {
		for _, p := range historyData {
			w.logger.Errorf("telemetry commit outcome unknown device=%s key=%s ts=%d err=%v", p.DeviceID, p.Key, p.TS, err)
		}
		return 0, 0, len(historyData)
	}
	w.logger.Errorf("telemetry batch rolled back; fallback to individual writes: count=%d err=%v", len(historyData), err)
	return w.fallbackInsert(historyData, nil)
}

func (w *telemetryWriter) fallbackInsert(historyData []TelemetryData, _ []TelemetryCurrentData) (written, failed, unknown int) {
	for _, point := range historyData {
		n, uncertain, err := w.legacyWrite([]TelemetryData{point})
		if err == nil {
			written += n
			continue
		}
		if uncertain {
			unknown++
			w.logger.Errorf("telemetry point commit outcome unknown device=%s key=%s ts=%d err=%v", point.DeviceID, point.Key, point.TS, err)
			continue
		}
		failed++
		diagnostics.GetInstance().RecordStorageFailed(point.DeviceID, fmt.Sprintf("存储失败：%v", err))
		w.logger.Errorf("telemetry point rolled back device=%s key=%s ts=%d err=%v", point.DeviceID, point.Key, point.TS, err)
	}
	return
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
