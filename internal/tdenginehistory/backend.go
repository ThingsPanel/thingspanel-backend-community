// Package tdenginehistory reads and deletes telemetry history in TDengine through
// dal.HistoryBackend. Writes arrive separately through storage.TDengineHistorySink.
package tdenginehistory

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/internal/storage"
)

// ErrUnsupported marks operations this backend does not implement yet. They fail
// loudly instead of silently reading another store.
var ErrUnsupported = errors.New("operation is not implemented by the tdengine history backend")

// Backend implements dal.HistoryBackend on top of one TDengine database.
type Backend struct {
	Sink *storage.TDengineHistorySink
}

var _ dal.HistoryBackend = (*Backend)(nil)

const columns = "CAST(ts AS BIGINT), number_v, bool_v, string_v, tenant_id"

func (b *Backend) rows(ctx context.Context, where, tail string) ([]*model.TelemetryData, error) {
	data, err := b.Sink.Query(ctx, fmt.Sprintf("SELECT %s, device_id, k FROM %s.telemetry WHERE %s %s", columns, b.Sink.Database, where, tail))
	if err != nil {
		return nil, err
	}
	out := make([]*model.TelemetryData, 0, len(data))
	for _, row := range data {
		if len(row) != 7 {
			return nil, fmt.Errorf("unexpected tdengine row width %d", len(row))
		}
		ts, ok := row[0].(float64)
		if !ok {
			return nil, errors.New("tdengine returned a non-numeric timestamp")
		}
		item := &model.TelemetryData{T: int64(ts)}
		if v, ok := row[1].(float64); ok {
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

func (b *Backend) scope(deviceID, key string, start, end int64) string {
	return fmt.Sprintf("device_id=%s AND k=%s AND ts>=%d AND ts<=%d", b.Sink.Quote(deviceID), b.Sink.Quote(key), start, end)
}

func (b *Backend) GetHistoryTelemetrData(deviceID, key string, start, end int64) ([]*model.TelemetryData, error) {
	return b.rows(context.Background(), b.scope(deviceID, key, start, end), "ORDER BY ts")
}

func (b *Backend) count(ctx context.Context, where string) (int64, error) {
	data, err := b.Sink.Query(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s.telemetry WHERE %s", b.Sink.Database, where))
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil // TDengine returns no row when the table does not exist yet
	}
	n, ok := data[0][0].(float64)
	if !ok {
		return 0, errors.New("tdengine returned a non-numeric count")
	}
	return int64(n), nil
}

func (b *Backend) GetHistoryTelemetrDataByPage(p *model.GetTelemetryHistoryDataByPageReq) (int64, []*model.TelemetryData, error) {
	where := b.scope(p.DeviceID, p.Key, p.StartTime, p.EndTime)
	total, err := b.count(context.Background(), where)
	if err != nil {
		return 0, nil, err
	}
	tail := "ORDER BY ts DESC"
	if p.Page != nil && p.PageSize != nil {
		if *p.Page < 1 || *p.PageSize < 1 {
			return total, nil, errors.New("page and page size must be positive")
		}
		tail += " LIMIT " + strconv.Itoa(*p.PageSize) + " OFFSET " + strconv.Itoa((*p.Page-1)**p.PageSize)
	}
	list, err := b.rows(context.Background(), where, tail)
	return total, list, err
}

func (b *Backend) GetHistoryTelemetrDataByExport(p *model.GetTelemetryHistoryDataByPageReq, offset, batchSize int) ([]*model.TelemetryData, error) {
	if offset < 0 || batchSize < 1 {
		return nil, errors.New("export offset must not be negative and batch size must be positive")
	}
	return b.rows(context.Background(), b.scope(p.DeviceID, p.Key, p.StartTime, p.EndTime),
		"ORDER BY ts DESC LIMIT "+strconv.Itoa(batchSize)+" OFFSET "+strconv.Itoa(offset))
}

// GetTelemetrStatisticData returns the same {x: ts, y: number} pairs as the PostgreSQL code.
func (b *Backend) GetTelemetrStatisticData(deviceID, key string, start, end int64) ([]map[string]interface{}, error) {
	data, err := b.Sink.Query(context.Background(), fmt.Sprintf("SELECT CAST(ts AS BIGINT), number_v FROM %s.telemetry WHERE %s ORDER BY ts", b.Sink.Database, b.scope(deviceID, key, start, end)))
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(data))
	for _, row := range data {
		x, _ := row[0].(float64)
		out = append(out, map[string]interface{}{"x": int64(x), "y": row[1]})
	}
	return out, nil
}

func (b *Backend) GetTelemetryDataCountByTenantId(tenantID string) (int64, error) {
	return b.count(context.Background(), "tenant_id="+b.Sink.Quote(tenantID))
}

// DeleteTelemetrData drops the child tables of one device key. It is idempotent.
func (b *Backend) DeleteTelemetrData(deviceID, key string) error {
	return b.dropTables(context.Background(), "device_id="+b.Sink.Quote(deviceID)+" AND k="+b.Sink.Quote(key))
}

func (b *Backend) dropTables(ctx context.Context, where string) error {
	data, err := b.Sink.Query(ctx, fmt.Sprintf("SELECT DISTINCT tbname FROM %s.telemetry WHERE %s", b.Sink.Database, where))
	if err != nil {
		return err
	}
	for _, row := range data {
		name, ok := row[0].(string)
		if !ok || !strings.HasPrefix(name, "t_") {
			return errors.New("tdengine returned an unexpected table name")
		}
		if _, err := b.Sink.Query(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", b.Sink.Database, name)); err != nil {
			return err
		}
	}
	return nil
}

// DeleteTelemetrDataByTime applies the retention policy; t is the cut-off in milliseconds.
func (b *Backend) DeleteTelemetrDataByTime(t int64) error {
	_, err := b.Sink.Query(context.Background(), fmt.Sprintf("DELETE FROM %s.telemetry WHERE ts<=%d", b.Sink.Database, t))
	return err
}

// DeleteTelemetrDataByDeviceId runs inside the device-deletion transaction, which a
// time-series store cannot join: dropping history first would lose it if that
// transaction rolled back. It stays unsupported until deletion is made durable and
// replayable through a generation.
func (b *Backend) DeleteTelemetrDataByDeviceId(string, *query.QueryTx) error {
	return fmt.Errorf("DeleteTelemetrDataByDeviceId: %w", ErrUnsupported)
}

// GetTelemetrStatisticaAgregationData reproduces the PostgreSQL bucketing exactly: windows are whole
// seconds aligned to the Unix epoch, numeric values with |v| >= 1e15 are ignored, and each
// bucket reports x (start, ms), x2 (end, ms) and y. Buckets are computed here, not by
// TDengine's INTERVAL, whose window alignment depends on the server time zone.
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
	data, err := b.Sink.Query(context.Background(), fmt.Sprintf("SELECT CAST(ts AS BIGINT), number_v FROM %s.telemetry WHERE %s AND number_v IS NOT NULL AND number_v < 1e15 AND number_v > -1e15 ORDER BY ts",
		b.Sink.Database, b.scope(deviceID, key, start, end)))
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
		ts, ok1 := row[0].(float64)
		v, ok2 := row[1].(float64)
		if !ok1 || !ok2 {
			return nil, errors.New("tdengine returned an unexpected aggregation row")
		}
		sec := int64(ts) / 1000
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

// GetTelemetryStatisticDataByDeviceIds shares the window arithmetic with the PostgreSQL
// implementation through dal.StatisticsByDeviceIds and supplies only the storage queries.
func (b *Backend) GetTelemetryStatisticDataByDeviceIds(deviceIds []string, keys []string, timeType string, limit *int, aggregateMethod string) ([]map[string]interface{}, error) {
	return dal.StatisticsByDeviceIds(statisticLeaf{b}, deviceIds, keys, timeType, limit, aggregateMethod)
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
	data, err := l.b.Sink.Query(context.Background(), fmt.Sprintf("SELECT %s(number_v) FROM %s.telemetry WHERE %s", function, l.b.Sink.Database, l.b.scope(deviceID, key, start, end)))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || data[0][0] == nil {
		return nil, nil
	}
	v, ok := data[0][0].(float64)
	if !ok {
		return nil, errors.New("tdengine returned a non-numeric aggregate")
	}
	return &v, nil
}

func (l statisticLeaf) Edges(deviceID, key string, start, end int64) (latest, oldest map[string]interface{}, found bool, err error) {
	edge := func(order string) (map[string]interface{}, error) {
		data, err := l.b.Sink.Query(context.Background(), fmt.Sprintf("SELECT number_v, string_v FROM %s.telemetry WHERE %s ORDER BY ts %s LIMIT 1", l.b.Sink.Database, l.b.scope(deviceID, key, start, end), order))
		if err != nil || len(data) == 0 {
			return nil, err
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
