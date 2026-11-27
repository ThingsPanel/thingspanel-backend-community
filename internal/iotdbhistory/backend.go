// Package iotdbhistory reads and deletes telemetry history in IoTDB through
// dal.HistoryBackend. Writes arrive separately through storage.IoTDBHistorySink.
package iotdbhistory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/internal/storage"
)

// Backend implements dal.HistoryBackend on one IoTDB Table-model database.
type Backend struct {
	Sink *storage.IoTDBHistorySink
}

var _ dal.HistoryBackend = (*Backend)(nil)

const rowColumns = "time, number_v, bool_v, string_v, tenant_id, device_id, k"
const queryPageSize = 10000

func (b *Backend) scope(deviceID, key string, start, end int64) string {
	return fmt.Sprintf("device_id=%s AND k=%s AND time>=%d AND time<=%d", storage.IoTDBQuote(deviceID), storage.IoTDBQuote(key), start, end)
}

func number(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func timestamp(value interface{}) (int64, bool) {
	if n, ok := number(value); ok {
		return int64(n), true
	}
	if text, ok := value.(string); ok {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return n, true
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999"} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed.UnixMilli(), true
			}
		}
	}
	return 0, false
}

func (b *Backend) decodeRows(data [][]interface{}) ([]*model.TelemetryData, error) {
	out := make([]*model.TelemetryData, 0, len(data))
	for _, row := range data {
		if len(row) != 7 {
			return nil, fmt.Errorf("unexpected iotdb row width %d", len(row))
		}
		ts, ok := timestamp(row[0])
		if !ok {
			return nil, errors.New("iotdb returned a non-numeric timestamp")
		}
		item := &model.TelemetryData{T: ts}
		if v, ok := number(row[1]); ok {
			item.NumberV = &v
		}
		if v, ok := row[2].(bool); ok {
			item.BoolV = &v
		}
		if v, ok := row[3].(string); ok {
			item.StringV = &v
		}
		if v, ok := row[4].(string); ok {
			item.TenantID = &v
		}
		item.DeviceID, _ = row[5].(string)
		item.Key, _ = row[6].(string)
		out = append(out, item)
	}
	return out, nil
}

func (b *Backend) rows(ctx context.Context, where, tail string, rowLimit int) ([]*model.TelemetryData, error) {
	data, err := b.Sink.QueryLimit(ctx, fmt.Sprintf("SELECT %s FROM telemetry WHERE %s %s", rowColumns, where, tail), rowLimit)
	if err != nil {
		return nil, err
	}
	return b.decodeRows(data)
}

func (b *Backend) allRows(ctx context.Context, selectColumns, where, order string) ([][]interface{}, error) {
	var out [][]interface{}
	for offset := 0; ; offset += queryPageSize {
		statement := fmt.Sprintf("SELECT %s FROM telemetry WHERE %s ORDER BY %s LIMIT %d OFFSET %d", selectColumns, where, order, queryPageSize, offset)
		data, err := b.Sink.QueryLimit(ctx, statement, queryPageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, data...)
		if len(data) < queryPageSize {
			return out, nil
		}
	}
}

func (b *Backend) GetHistoryTelemetrData(deviceID, key string, start, end int64) ([]*model.TelemetryData, error) {
	data, err := b.allRows(context.Background(), rowColumns, b.scope(deviceID, key, start, end), "time")
	if err != nil {
		return nil, err
	}
	return b.decodeRows(data)
}

func (b *Backend) count(ctx context.Context, where string) (int64, error) {
	data, err := b.Sink.QueryLimit(ctx, fmt.Sprintf("SELECT COUNT(*) FROM telemetry WHERE %s", where), 1)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 || len(data[0]) == 0 || data[0][0] == nil {
		return 0, nil
	}
	n, ok := number(data[0][0])
	if !ok {
		return 0, errors.New("iotdb returned a non-numeric count")
	}
	return int64(n), nil
}

func (b *Backend) GetHistoryTelemetrDataByPage(p *model.GetTelemetryHistoryDataByPageReq) (int64, []*model.TelemetryData, error) {
	if p == nil {
		return 0, nil, errors.New("history page request is required")
	}
	if (p.Page == nil) != (p.PageSize == nil) {
		return 0, nil, errors.New("page and page size must be provided together")
	}
	if p.Page != nil && (*p.Page < 1 || *p.PageSize < 1) {
		return 0, nil, errors.New("page and page size must be positive")
	}
	if p.Page != nil && *p.Page-1 > int(^uint(0)>>1) / *p.PageSize {
		return 0, nil, errors.New("page offset is too large")
	}
	where := b.scope(p.DeviceID, p.Key, p.StartTime, p.EndTime)
	total, err := b.count(context.Background(), where)
	if err != nil {
		return 0, nil, err
	}
	tail := "ORDER BY time DESC"
	if p.Page != nil && p.PageSize != nil {
		tail += " LIMIT " + strconv.Itoa(*p.PageSize) + " OFFSET " + strconv.Itoa((*p.Page-1)**p.PageSize)
		list, err := b.rows(context.Background(), where, tail, *p.PageSize)
		return total, list, err
	}
	data, err := b.allRows(context.Background(), rowColumns, where, "time DESC")
	if err != nil {
		return total, nil, err
	}
	list, err := b.decodeRows(data)
	return total, list, err
}

func (b *Backend) GetHistoryTelemetrDataByExport(p *model.GetTelemetryHistoryDataByPageReq, offset, batchSize int) ([]*model.TelemetryData, error) {
	if p == nil {
		return nil, errors.New("history export request is required")
	}
	if offset < 0 || batchSize < 1 {
		return nil, errors.New("export offset must not be negative and batch size must be positive")
	}
	return b.rows(context.Background(), b.scope(p.DeviceID, p.Key, p.StartTime, p.EndTime),
		"ORDER BY time DESC LIMIT "+strconv.Itoa(batchSize)+" OFFSET "+strconv.Itoa(offset), batchSize)
}

func (b *Backend) GetTelemetrStatisticData(deviceID, key string, start, end int64) ([]map[string]interface{}, error) {
	data, err := b.allRows(context.Background(), "time, number_v", b.scope(deviceID, key, start, end), "time")
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(data))
	for _, row := range data {
		if len(row) != 2 {
			return nil, errors.New("iotdb returned an unexpected statistic row")
		}
		x, ok := timestamp(row[0])
		if !ok {
			return nil, errors.New("iotdb returned a non-numeric timestamp")
		}
		out = append(out, map[string]interface{}{"x": x, "y": row[1]})
	}
	return out, nil
}

func (b *Backend) GetTelemetryDataCountByTenantId(tenantID string) (int64, error) {
	return b.count(context.Background(), "tenant_id="+storage.IoTDBQuote(tenantID))
}

func (b *Backend) DeleteTelemetrData(deviceID, key string) error {
	return b.Sink.DeleteKey(context.Background(), deviceID, key)
}

func (b *Backend) DeleteTelemetrDataByTime(t int64) error {
	return b.Sink.DeleteBefore(context.Background(), t)
}

func (b *Backend) DeleteTelemetrDataByDeviceId(deviceID string, tx *query.QueryTx) error {
	if tx == nil {
		return errors.New("device history deletion requires the device-deletion transaction")
	}
	return storage.EnqueueHistoryDeviceDeletion(tx.TelemetryData.UnderlyingDB(), deviceID)
}

func (b *Backend) GetTelemetrStatisticaAgregationData(deviceID, key string, start, end, windowMillis int64, function string) ([]map[string]interface{}, error) {
	switch function {
	case "avg", "max", "min", "sum", "diff":
	default:
		return nil, fmt.Errorf("不支持的聚合函数: %s", function)
	}
	window := windowMillis / 1000
	if window < 1 {
		window = 1
	}
	where := b.scope(deviceID, key, start, end) + " AND number_v IS NOT NULL AND number_v < 1e15 AND number_v > -1e15"
	data, err := b.allRows(context.Background(), "time, number_v", where, "time")
	if err != nil {
		return nil, err
	}
	type bucket struct {
		start         int64
		sum, min, max float64
		count         int
	}
	var buckets []*bucket
	for _, row := range data {
		if len(row) != 2 {
			return nil, errors.New("iotdb returned an unexpected aggregation row")
		}
		ts, ok1 := timestamp(row[0])
		v, ok2 := number(row[1])
		if !ok1 || !ok2 {
			return nil, errors.New("iotdb returned an unexpected aggregation row")
		}
		sec := ts / 1000
		x := sec - sec%window
		if len(buckets) == 0 || buckets[len(buckets)-1].start != x {
			buckets = append(buckets, &bucket{start: x, min: v, max: v})
		}
		cur := buckets[len(buckets)-1]
		cur.sum += v
		cur.count++
		if v < cur.min {
			cur.min = v
		}
		if v > cur.max {
			cur.max = v
		}
	}
	out := make([]map[string]interface{}, 0, len(buckets))
	for _, cur := range buckets {
		var y float64
		switch function {
		case "avg":
			y = cur.sum / float64(cur.count)
		case "sum":
			y = cur.sum
		case "max":
			y = cur.max
		case "min":
			y = cur.min
		case "diff":
			y = cur.max - cur.min
		}
		out = append(out, map[string]interface{}{"x": cur.start * 1000, "x2": (cur.start + window) * 1000, "y": y})
	}
	return out, nil
}

func (b *Backend) GetTelemetryStatisticDataByDeviceIds(deviceIDs []string, keys []string, timeType string, limit *int, aggregateMethod string) ([]map[string]interface{}, error) {
	return dal.StatisticsByDeviceIds(statisticLeaf{b}, deviceIDs, keys, timeType, limit, aggregateMethod)
}

type statisticLeaf struct{ b *Backend }

func (l statisticLeaf) Count(deviceID, key string, start, end int64) (int64, error) {
	return l.b.count(context.Background(), l.b.scope(deviceID, key, start, end))
}

func (l statisticLeaf) Aggregate(deviceID, key, method string, start, end int64) (*float64, error) {
	var function string
	switch method {
	case "avg":
		function = "AVG"
	case "sum":
		function = "SUM"
	case "max":
		function = "MAX"
	case "min":
		function = "MIN"
	default:
		return nil, fmt.Errorf("不支持的聚合方式: %s", method)
	}
	data, err := l.b.Sink.QueryLimit(context.Background(), fmt.Sprintf("SELECT %s(number_v) FROM telemetry WHERE %s", function, l.b.scope(deviceID, key, start, end)), 1)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data[0]) == 0 || data[0][0] == nil {
		return nil, nil
	}
	v, ok := number(data[0][0])
	if !ok {
		return nil, errors.New("iotdb returned a non-numeric aggregate")
	}
	return &v, nil
}

func (l statisticLeaf) Edges(deviceID, key string, start, end int64) (latest, oldest map[string]interface{}, found bool, err error) {
	edge := func(order string) (map[string]interface{}, error) {
		data, err := l.b.Sink.QueryLimit(context.Background(), fmt.Sprintf("SELECT number_v, string_v FROM telemetry WHERE %s ORDER BY time %s LIMIT 1", l.b.scope(deviceID, key, start, end), order), 1)
		if err != nil || len(data) == 0 {
			return nil, err
		}
		if len(data[0]) != 2 {
			return nil, errors.New("iotdb returned an unexpected edge row")
		}
		return map[string]interface{}{"number_v": data[0][0], "string_v": data[0][1]}, nil
	}
	if latest, err = edge("DESC"); err != nil || latest == nil {
		return nil, nil, false, err
	}
	if oldest, err = edge("ASC"); err != nil || oldest == nil {
		return nil, nil, false, err
	}
	return latest, oldest, true, nil
}
