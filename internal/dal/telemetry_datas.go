package dal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	tptodb "project/third_party/grpc/tptodb_client"
	pb "project/third_party/grpc/tptodb_client/grpc_tptodb"
)

func CreateTelemetrData(data *model.TelemetryData) error {
	return query.TelemetryData.Create(data)
}

func GetCurrentTelemetrData(deviceId string) ([]model.TelemetryData, error) {
	var re []model.TelemetryData
	sql := `
	SELECT *
	FROM (
		SELECT
			*,
			ROW_NUMBER() OVER (PARTITION BY key ORDER BY ts DESC) as rn
		FROM telemetry_datas
		WHERE device_id = ?
	) subquery
	WHERE rn = 1
	`
	r := global.DB.Raw(sql, deviceId).Scan(&re)
	if r.Error != nil {
		return nil, r.Error
	}

	return re, nil
}

// 根据设备ID，按ts倒序查找一条数据
func GetCurrentTelemetrDetailData(deviceId string) (*model.TelemetryData, error) {
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB" {
		var data []model.TelemetryData
		// 获取当前设备的第一条数据
		request := &pb.GetDeviceAttributesCurrentsRequest{
			DeviceId: deviceId,
		}
		request.Attribute = append(request.Attribute, "")
		r, err := tptodb.TptodbClient.GetDeviceAttributesCurrents(context.Background(), request)
		if err != nil {
			logrus.Printf("err: %+v", err)
			return nil, err
		}
		logrus.Printf("data: %+v", r.Data)
		err = json.Unmarshal([]byte(r.Data), &data)
		if err != nil {
			logrus.Printf("err: %+v", err)
			return nil, err
		}
		if len(data) > 0 {
			return &data[0], err
		}
		return &model.TelemetryData{}, err
	}

	re, err := query.TelemetryData.
		Where(query.TelemetryData.DeviceID.Eq(deviceId)).
		Order(query.TelemetryData.T.Desc()).
		First()
	if err != nil {
		logrus.Error(err)
		return re, err
	}
	return re, nil
}

func GetHistoryTelemetrData(deviceId, key string, startTime, endTime int64) ([]*model.TelemetryData, error) {
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB" {
		data := make([]*model.TelemetryData, 0)
		request := &pb.GetDeviceHistoryRequest{
			DeviceId:  deviceId,
			StartTime: startTime,
			EndTime:   endTime,
			Key:       key,
		}
		r, err := tptodb.TptodbClient.GetDeviceHistory(context.Background(), request)
		if err != nil {
			logrus.Printf("err: %+v", err)
			return nil, err
		}
		logrus.Printf("data: %+v", r.Data)
		err = json.Unmarshal([]byte(r.Data), &data)
		if err != nil {
			logrus.Printf("Unmarshal err:%v", err)
			return nil, err
		}

		return data, nil
	}

	data, err := query.TelemetryData.
		Where(query.TelemetryData.DeviceID.Eq(deviceId)).
		Where(query.TelemetryData.Key.Eq(key)).
		Where(query.TelemetryData.T.Between(startTime, endTime)).Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetHistoryTelemetrDataByPage(p *model.GetTelemetryHistoryDataByPageReq) (int64, []*model.TelemetryData, error) {
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB" {
		data := make([]*model.TelemetryData, 0)
		request := &pb.GetDeviceHistoryWithPageAndPageRequest{
			DeviceId:  p.DeviceID,
			StartTime: p.StartTime,
			EndTime:   p.EndTime,
		}
		if len(p.Key) > 0 {
			request.Key = p.Key
		}
		r, err := tptodb.TptodbClient.GetDeviceHistoryWithPageAndPage(context.Background(), request)
		if err != nil {
			logrus.Printf("err: %+v", err)
			return 0, nil, err
		}

		logrus.Printf("data: %+v", r.Data)
		err = json.Unmarshal([]byte(r.Data), &data)
		if err != nil {
			logrus.Printf("err: %+v", err)
			return 0, nil, err
		}
		return int64(len(data)), data, nil
	}

	var count int64
	q := query.TelemetryData
	queryBuilder := q.WithContext(context.Background())

	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(p.DeviceID))
	queryBuilder = queryBuilder.Where(q.Key.Eq(p.Key))

	// st := time.Unix(p.StartTime, 0)
	// et := time.Unix(p.EndTime, 0)

	queryBuilder = queryBuilder.Where(q.T.Between(p.StartTime, p.EndTime))

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	if p.Page != nil && p.PageSize != nil {
		queryBuilder = queryBuilder.Limit(*p.PageSize)
		queryBuilder = queryBuilder.Offset((*p.Page - 1) * *p.PageSize)
	}

	list, err := queryBuilder.Select().Order(q.T.Desc()).Find()
	if err != nil {
		logrus.Error(err)
		return count, list, err
	}

	return count, list, nil
}

func GetHistoryTelemetrDataByExport(p *model.GetTelemetryHistoryDataByPageReq, offset, batchSize int) ([]*model.TelemetryData, error) {
	q := query.TelemetryData
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(p.DeviceID))
	queryBuilder = queryBuilder.Where(q.Key.Eq(p.Key))
	queryBuilder = queryBuilder.Where(q.T.Between(p.StartTime, p.EndTime))
	list, err := queryBuilder.Select().Offset(offset).Limit(batchSize).Order(q.T.Desc()).Find()
	if err != nil {
		logrus.Error(err)
		return list, err
	}

	return list, nil
}

// 批量插入
func CreateTelemetrDataBatch(data []*model.TelemetryData) error {
	// 先尝试直接批量插入
	err := query.TelemetryData.CreateInBatches(data, len(data))
	if err == nil {
		return nil // 成功插入，直接返回
	}
	// 检查是否为唯一约束冲突
	if !isUniqueConstraintError(err) {
		return err // 其他错误直接返回
	}
	// 发生冲突时，使用带冲突处理的SQL
	logrus.Debugf("发生(device_id,key,timestamp)唯一约束冲突，使用冲突处理SQL: data: %+v, err: %+v", data, err)
	sql := `INSERT INTO telemetry_datas (device_id, key, ts, number_v, string_v, bool_v, tenant_id) VALUES `

	values := make([]interface{}, 0, len(data)*7)
	placeholders := make([]string, 0, len(data))

	for i, d := range data {
		placeholders = append(placeholders, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			i*7+1, i*7+2, i*7+3, i*7+4, i*7+5, i*7+6, i*7+7))

		values = append(values, d.DeviceID, d.Key, d.T, d.NumberV, d.StringV, d.BoolV, d.TenantID)
	}

	sql += strings.Join(placeholders, ", ")
	// 由于约束是(device_id, key, ts)，冲突时ts必然相等，直接更新
	sql += ` ON CONFLICT (device_id, key, ts) DO UPDATE SET
		number_v = EXCLUDED.number_v,
		string_v = EXCLUDED.string_v,
		bool_v = EXCLUDED.bool_v`

	return global.DB.Exec(sql, values...).Error
}

// 检查是否为唯一约束冲突错误
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// PostgreSQL唯一约束错误标识
	return strings.Contains(errStr, "SQLSTATE 23505") ||
		strings.Contains(errStr, "duplicate key value violates unique constraint")
}

// 批量更新，如果没有则新增
func UpdateTelemetrDataBatch(data []*model.TelemetryData) error {
	// 条件字段，device_id, key
	for _, d := range data {
		var dc model.TelemetryCurrentData
		dc.DeviceID = d.DeviceID
		dc.Key = d.Key
		dc.NumberV = d.NumberV
		dc.StringV = d.StringV
		dc.BoolV = d.BoolV
		// 时间戳转time.Time
		dc.T = time.Unix(0, d.T*int64(time.Millisecond)).UTC()
		dc.TenantID = d.TenantID
		info, err := query.TelemetryCurrentData.
			Where(query.TelemetryCurrentData.DeviceID.Eq(d.DeviceID)).
			Where(query.TelemetryCurrentData.Key.Eq(d.Key)).
			Updates(map[string]interface{}{"number_v": d.NumberV, "string_v": d.StringV, "bool_v": d.BoolV, "ts": dc.T})
		if err != nil {
			return err
		} else if info.RowsAffected == 0 {
			err := query.TelemetryCurrentData.Create(&dc)
			if err != nil {
				logrus.Error(err)
				return err
			}
		}
	}
	return nil
}

// 删除数据
func DeleteTelemetrData(deviceId, key string) error {
	_, err := query.TelemetryData.
		Where(query.TelemetryData.DeviceID.Eq(deviceId)).
		Where(query.TelemetryData.Key.Eq(key)).
		Delete()
	return err
}

// 根据时间批量删除遥测数据
func DeleteTelemetrDataByTime(t int64) error {
	_, err := query.TelemetryData.Where(query.TelemetryData.T.Lte(t)).Delete()
	if err != nil {
		logrus.Error(err)
		return err
	} else {
		if err := global.DB.Exec("VACUUM FULL telemetry_datas").Error; err != nil {
			logrus.Warnf("Error during VACUUM FULL: %v", err)
		}
		return err
	}
}

// 非聚合查询(req.DeviceID, req.Key, req.StartTime, req.EndTime)
func GetTelemetrStatisticData(deviceID, key string, startTime, endTime int64) ([]map[string]interface{}, error) {
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB" {
		var fields []map[string]interface{}
		request := &pb.GetDeviceKVDataWithNoAggregateRequest{
			DeviceId:  deviceID,
			Key:       key,
			StartTime: startTime,
			EndTime:   endTime,
		}
		r, err := tptodb.TptodbClient.GetDeviceKVDataWithNoAggregate(context.Background(), request)
		if err != nil {
			logrus.Printf("err: %+v\n", err)
			return fields, err
		}
		logrus.Printf("data: %+v", r.Data)
		err = json.Unmarshal([]byte(r.Data), &fields)
		if err != nil {
			logrus.Printf("err: %+v\n", err)
			return nil, err
		}
		return fields, nil
	}

	q := query.TelemetryData
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(deviceID))
	queryBuilder = queryBuilder.Where(q.Key.Eq(key))
	queryBuilder = queryBuilder.Where(q.T.Between(startTime, endTime))
	var data []map[string]interface{}
	err := queryBuilder.Select(q.T.As("x"), q.NumberV.As("y")).Scan(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetTelemetrStatisticaAgregationData(deviceId, key string, sTime, eTime, aggregateWindow int64, aggregateFunc string) ([]map[string]interface{}, error) {
	var data []map[string]interface{}
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB" {
		request := &pb.GetDeviceKVDataWithAggregateRequest{
			DeviceId:        deviceId,
			Key:             key,
			StartTime:       sTime,
			EndTime:         eTime,
			AggregateWindow: aggregateWindow,
			AggregateFunc:   aggregateFunc,
		}
		r, err := tptodb.TptodbClient.GetDeviceKVDataWithAggregate(context.Background(), request)
		if err != nil {
			logrus.Printf("err: %+v\n", err)
			return nil, err
		}
		logrus.Printf("data: %+v", r.Data)
		err = json.Unmarshal([]byte(r.Data), &data)
		if err != nil {
			logrus.Printf("err: %+v\n", err)
			return nil, err
		}
		return data, nil
	}

	// pg数据库进行聚合查询
	telemetryDatasAggregate := TelemetryDatasAggregate{
		DeviceID:          deviceId,
		Key:               key,
		STime:             sTime,
		ETime:             eTime,
		AggregateWindow:   aggregateWindow,
		AggregateFunction: aggregateFunc,
	}

	data, err := GetTelemetryDatasAggregate(context.Background(), telemetryDatasAggregate)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetTelemetryDataCountByTenantId(tenantId string) (int64, error) {
	var count int64
	var explainOutput string

	sql := `
		EXPLAIN select * from telemetry_datas where tenant_id = ?;
		`
	err := global.DB.Raw(sql, tenantId).Row().Scan(&explainOutput)
	if err != nil {
		return count, err
	}
	re := regexp.MustCompile(`rows=(\d+)`)
	match := re.FindStringSubmatch(explainOutput)
	if len(match) > 1 {
		count, err = strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

// 支持的间隔之间
var StatisticAggregateWindowMicrosecond = map[string]int64{
	"30s": int64(time.Second * 30 / time.Microsecond),
	"1m":  int64(time.Minute / time.Microsecond),
	"2m":  int64(time.Minute * 2 / time.Microsecond),
	"5m":  int64(time.Minute * 5 / time.Microsecond),
	"10m": int64(time.Minute * 10 / time.Microsecond),
	"30m": int64(time.Minute * 30 / time.Microsecond),
	"1h":  int64(time.Hour / time.Microsecond),
	"3h":  int64(time.Hour * 3 / time.Microsecond),
	"6h":  int64(time.Hour * 6 / time.Microsecond),
	"1d":  int64(time.Hour * 24 / time.Microsecond),
	"7d":  int64(time.Hour * 24 * 7 / time.Microsecond),
	"1mo": int64(time.Hour * 24 * 30 / time.Microsecond),
}

var StatisticAggregateWindowMillisecond = map[string]int64{
	"30s": int64(time.Second * 30 / time.Millisecond),
	"1m":  int64(time.Minute / time.Millisecond),
	"2m":  int64(time.Minute * 2 / time.Millisecond),
	"5m":  int64(time.Minute * 5 / time.Millisecond),
	"10m": int64(time.Minute * 10 / time.Millisecond),
	"30m": int64(time.Minute * 30 / time.Millisecond),
	"1h":  int64(time.Hour / time.Millisecond),
	"3h":  int64(time.Hour * 3 / time.Millisecond),
	"6h":  int64(time.Hour * 6 / time.Millisecond),
	"1d":  int64(time.Hour * 24 / time.Millisecond),
	"7d":  int64(time.Hour * 24 * 7 / time.Millisecond),
	"1mo": int64(time.Hour * 24 * 30 / time.Millisecond),
}

// 根据设备id删除所有数据
func DeleteTelemetrDataByDeviceId(deviceId string, tx *query.QueryTx) error {
	_, err := tx.TelemetryData.Where(query.TelemetryData.DeviceID.Eq(deviceId)).Delete()
	return err
}

// GetTelemetryStatisticDataByDeviceIds 根据多个设备ID和key查询遥测统计数据
func GetTelemetryStatisticDataByDeviceIds(deviceIds []string, keys []string, timeType string, limit *int, aggregateMethod string) ([]map[string]interface{}, error) {
	// 验证设备ID和key数量一致
	if len(deviceIds) != len(keys) {
		return nil, fmt.Errorf("设备ID数量与key数量不匹配")
	}

	var results []map[string]interface{}

	// 计算时间范围
	endTime := time.Now().UnixNano() / 1e6
	var startTime int64

	// 默认limit为1
	actualLimit := 1
	if limit != nil && *limit > 0 {
		actualLimit = *limit
	}

	switch timeType {
	case "hour":
		startTime = endTime - int64(actualLimit*int(time.Hour.Milliseconds()))
	case "day":
		startTime = endTime - int64(actualLimit*int(24*time.Hour.Milliseconds()))
	case "week":
		startTime = endTime - int64(actualLimit*int(7*24*time.Hour.Milliseconds()))
	case "month":
		startTime = endTime - int64(actualLimit*int(30*24*time.Hour.Milliseconds()))
	case "year":
		startTime = endTime - int64(actualLimit*int(365*24*time.Hour.Milliseconds()))
	default:
		return nil, fmt.Errorf("不支持的时间类型: %s", timeType)
	}

	// 遍历设备ID和key的配对
	for i, deviceId := range deviceIds {
		key := keys[i]

		// 根据聚合方式选择查询
		if aggregateMethod == "count" {
			// 计数查询
			count, err := getDataCount(deviceId, key, startTime, endTime)
			if err != nil {
				logrus.Error("查询数据计数失败:", err)
				continue
			}
			results = append(results, map[string]interface{}{
				"device_id": deviceId,
				"key":       key,
				"count":     count,
			})
		} else if aggregateMethod == "diff" {
			// 差值查询 - 最新值减去最旧值
			diffData, err := getDiffData(deviceId, key, startTime, endTime, timeType)
			if err != nil {
				logrus.Error("查询差值数据失败:", err)
				continue
			}
			results = append(results, map[string]interface{}{
				"device_id": deviceId,
				"key":       key,
				"data":      diffData,
			})
		} else {
			// 聚合查询 (avg, sum, max, min) - 返回时间序列数据
			aggregatedData, err := getAggregatedDataWithTime(deviceId, key, startTime, endTime, aggregateMethod, limit, timeType)
			if err != nil {
				logrus.Error("查询聚合数据失败:", err)
				continue
			}
			results = append(results, map[string]interface{}{
				"device_id": deviceId,
				"key":       key,
				"data":      aggregatedData,
			})
		}
	}

	return results, nil
}

// getDataCount 获取数据计数
func getDataCount(deviceId, key string, startTime, endTime int64) (int64, error) {
	q := query.TelemetryData
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(deviceId))
	queryBuilder = queryBuilder.Where(q.Key.Eq(key))
	queryBuilder = queryBuilder.Where(q.T.Between(startTime, endTime))

	count, err := queryBuilder.Count()
	if err != nil {
		return 0, err
	}
	return count, nil
}

// getDataRange 获取数据范围
func getDataRange(deviceId, key string, startTime, endTime int64, limit *int) ([]map[string]interface{}, error) {
	q := query.TelemetryData
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(deviceId))
	queryBuilder = queryBuilder.Where(q.Key.Eq(key))
	queryBuilder = queryBuilder.Where(q.T.Between(startTime, endTime))
	queryBuilder = queryBuilder.Order(q.T.Desc())

	if limit != nil {
		queryBuilder = queryBuilder.Limit(*limit)
	}

	var data []map[string]interface{}
	err := queryBuilder.Select(q.T.As("timestamp"), q.NumberV.As("value")).Scan(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// getAggregatedData 获取聚合数据
func getAggregatedData(deviceId, key string, startTime, endTime int64, aggregateMethod string, limit *int) (interface{}, error) {
	q := query.TelemetryData
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(deviceId))
	queryBuilder = queryBuilder.Where(q.Key.Eq(key))
	queryBuilder = queryBuilder.Where(q.T.Between(startTime, endTime))

	var result []map[string]interface{}
	var err error

	switch aggregateMethod {
	case "avg":
		err = queryBuilder.Select(q.NumberV.Avg().As("value")).Scan(&result)
	case "sum":
		err = queryBuilder.Select(q.NumberV.Sum().As("value")).Scan(&result)
	case "max":
		err = queryBuilder.Select(q.NumberV.Max().As("value")).Scan(&result)
	case "min":
		err = queryBuilder.Select(q.NumberV.Min().As("value")).Scan(&result)
	default:
		return nil, fmt.Errorf("不支持的聚合方式: %s", aggregateMethod)
	}

	if err != nil {
		return nil, err
	}

	// 返回聚合结果的第一条记录的value值
	if len(result) > 0 && result[0]["value"] != nil {
		return result[0]["value"], nil
	}

	return 0, nil
}

// getAggregatedDataWithTime 获取按时间窗口分组的聚合数据
func getAggregatedDataWithTime(deviceId, key string, startTime, endTime int64, aggregateMethod string, limit *int, timeType string) ([]map[string]interface{}, error) {
	// 计算实际的limit值
	actualLimit := 1
	if limit != nil && *limit > 0 {
		actualLimit = *limit
	}

	var results []map[string]interface{}

	// 从endTime开始向后回溯，生成时间窗口
	var timeWindows []struct {
		start int64
		end   int64
	}

	endTimeUnix := time.Unix(0, endTime*int64(time.Millisecond))

	switch timeType {
	case "hour":
		// 小时级别 - 对齐到整点小时
		nextHour := time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day(), endTimeUnix.Hour()+1, 0, 0, 0, endTimeUnix.Location())

		for i := 0; i < actualLimit; i++ {
			windowEnd := nextHour.Add(time.Duration(-i) * time.Hour)
			windowStart := windowEnd.Add(-time.Hour)

			timeWindows = append(timeWindows, struct {
				start int64
				end   int64
			}{
				start: windowStart.UnixNano() / 1e6,
				end:   windowEnd.UnixNano() / 1e6,
			})
		}
	case "day":
		// 天级别 - 对齐到天边界
		nextDay := time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day()+1, 0, 0, 0, 0, endTimeUnix.Location())

		for i := 0; i < actualLimit; i++ {
			windowEnd := nextDay.Add(time.Duration(-i) * 24 * time.Hour)
			windowStart := windowEnd.Add(-24 * time.Hour)

			timeWindows = append(timeWindows, struct {
				start int64
				end   int64
			}{
				start: windowStart.UnixNano() / 1e6,
				end:   windowEnd.UnixNano() / 1e6,
			})
		}
	case "week":
		// 周级别 - 对齐到周边界（周日为一周开始）
		nextWeek := endTimeUnix.AddDate(0, 0, 7-int(endTimeUnix.Weekday()))
		nextWeek = time.Date(nextWeek.Year(), nextWeek.Month(), nextWeek.Day(), 0, 0, 0, 0, nextWeek.Location())

		for i := 0; i < actualLimit; i++ {
			windowEnd := nextWeek.Add(time.Duration(-i) * 7 * 24 * time.Hour)
			windowStart := windowEnd.Add(-7 * 24 * time.Hour)

			timeWindows = append(timeWindows, struct {
				start int64
				end   int64
			}{
				start: windowStart.UnixNano() / 1e6,
				end:   windowEnd.UnixNano() / 1e6,
			})
		}
	case "month":
		// 月级别 - 对齐到月边界
		year, month, _ := endTimeUnix.Date()
		nextMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, endTimeUnix.Location())

		for i := 0; i < actualLimit; i++ {
			windowEnd := nextMonth.AddDate(0, -i, 0)
			windowStart := windowEnd.AddDate(0, -1, 0)

			timeWindows = append(timeWindows, struct {
				start int64
				end   int64
			}{
				start: windowStart.UnixNano() / 1e6,
				end:   windowEnd.UnixNano() / 1e6,
			})
		}
	case "year":
		// 年级别 - 对齐到年边界
		nextYear := time.Date(endTimeUnix.Year()+1, 1, 1, 0, 0, 0, 0, endTimeUnix.Location())

		for i := 0; i < actualLimit; i++ {
			windowEnd := nextYear.AddDate(-i, 0, 0)
			windowStart := windowEnd.AddDate(-1, 0, 0)

			timeWindows = append(timeWindows, struct {
				start int64
				end   int64
			}{
				start: windowStart.UnixNano() / 1e6,
				end:   windowEnd.UnixNano() / 1e6,
			})
		}
	default:
		// 其他情况 - 简单平均分割
		totalDuration := endTime - startTime
		windowSizeMs := totalDuration / int64(actualLimit)

		for i := 0; i < actualLimit; i++ {
			windowStart := startTime + int64(i)*windowSizeMs
			windowEnd := windowStart + windowSizeMs

			timeWindows = append(timeWindows, struct {
				start int64
				end   int64
			}{
				start: windowStart,
				end:   windowEnd,
			})
		}
	}

	// 为每个时间窗口执行查询
	for _, window := range timeWindows {
		// 构建聚合函数
		var aggregateFunc string
		switch aggregateMethod {
		case "avg":
			aggregateFunc = "AVG(number_v)"
		case "sum":
			aggregateFunc = "SUM(number_v)"
		case "max":
			aggregateFunc = "MAX(number_v)"
		case "min":
			aggregateFunc = "MIN(number_v)"
		default:
			return nil, fmt.Errorf("不支持的聚合方式: %s", aggregateMethod)
		}

		// 执行单个窗口的查询
		sql := fmt.Sprintf(`
			SELECT 
				%s as value,
				%d as timestamp
			FROM telemetry_datas 
			WHERE device_id = ? AND key = ? AND ts BETWEEN ? AND ?
		`, aggregateFunc, window.start)

		var result []map[string]interface{}
		err := global.DB.Raw(sql, deviceId, key, window.start, window.end).Scan(&result)
		if err.Error != nil {
			return nil, err.Error
		}

		// 添加结果 - 只有真正有数据时才添加
		if len(result) > 0 && result[0]["value"] != nil {
			results = append(results, result[0])
		}
	}

	return results, nil
}

// getDiffData 获取差值数据 - 按时间窗口分组计算每个窗口内最新值减去最旧值
func getDiffData(deviceId, key string, startTime, endTime int64, timeType string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}

	// 从endTime开始向前对齐到时间边界，然后向前生成时间窗口
	// 使用当地时区而不是UTC，以确保时间边界对齐正确
	loc := time.Local
	endTimeUnix := time.Unix(0, endTime*int64(time.Millisecond)).In(loc)

	// 对齐到时间边界
	var alignedEndTime time.Time
	switch timeType {
	case "hour":
		// 对齐到当前小时的结束时间（下一个整点）
		alignedEndTime = time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day(), endTimeUnix.Hour()+1, 0, 0, 0, loc)
	case "day":
		// 对齐到当前天的结束时间（下一天的00:00:00）
		alignedEndTime = time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day()+1, 0, 0, 0, 0, loc)
	case "week":
		// 对齐到下周的开始日期
		alignedEndTime = time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day()+1, 0, 0, 0, 0, loc)
	case "month":
		// 对齐到下个月的第一天
		alignedEndTime = time.Date(endTimeUnix.Year(), endTimeUnix.Month()+1, 1, 0, 0, 0, 0, loc)
	case "year":
		// 对齐到下一年的第一天
		alignedEndTime = time.Date(endTimeUnix.Year()+1, 1, 1, 0, 0, 0, 0, loc)
	default:
		alignedEndTime = endTimeUnix
	}

	// 计算需要的窗口数量，从数据范围推算
	startTimeUnix := time.Unix(0, startTime*int64(time.Millisecond)).In(loc)
	duration := alignedEndTime.Sub(startTimeUnix)

	var windowCount int
	switch timeType {
	case "hour":
		windowCount = int(duration.Hours()) + 1
	case "day":
		windowCount = int(duration.Hours()/24) + 1
	case "week":
		windowCount = int(duration.Hours()/(7*24)) + 1
	case "month":
		windowCount = int(duration.Hours()/(30*24)) + 1
	case "year":
		windowCount = int(duration.Hours()/(365*24)) + 1
	default:
		return nil, fmt.Errorf("不支持的时间类型: %s", timeType)
	}

	// 限制窗口数量，避免过多的计算
	if windowCount > 100 {
		windowCount = 100
	}

	// 生成时间窗口（从最新开始向前推）
	for i := 0; i < windowCount; i++ {
		var windowStart, windowEnd time.Time

		switch timeType {
		case "hour":
			windowEnd = alignedEndTime.Add(time.Duration(-i) * time.Hour)
			windowStart = windowEnd.Add(-time.Hour)
		case "day":
			windowEnd = alignedEndTime.Add(time.Duration(-i) * 24 * time.Hour)
			windowStart = windowEnd.Add(-24 * time.Hour)
		case "week":
			windowEnd = alignedEndTime.Add(time.Duration(-i) * 7 * 24 * time.Hour)
			windowStart = windowEnd.Add(-7 * 24 * time.Hour)
		case "month":
			windowEnd = alignedEndTime.AddDate(0, -i, 0)
			windowStart = windowEnd.AddDate(0, -1, 0)
		case "year":
			windowEnd = alignedEndTime.AddDate(-i, 0, 0)
			windowStart = windowEnd.AddDate(-1, 0, 0)
		}

		windowStartMs := windowStart.UnixNano() / 1e6
		windowEndMs := windowEnd.UnixNano() / 1e6

		// 只处理与实际数据范围有交集的窗口
		if windowEndMs <= startTime || windowStartMs >= endTime {
			continue
		}

		// 调整窗口边界以确保在数据范围内
		actualStart := windowStartMs
		actualEnd := windowEndMs
		if actualStart < startTime {
			actualStart = startTime
		}
		if actualEnd > endTime {
			actualEnd = endTime
		}

		// 查询当前时间窗口内的差值
		diffValue, err := getDiffValueInTimeWindow(deviceId, key, actualStart, actualEnd)
		if err != nil {
			logrus.Error("查询时间窗口差值失败:", err)
			continue
		}

		// 如果有数据，添加到结果中
		if diffValue != nil {
			// 格式化时间 - 使用窗口开始时间作为该时间段的代表时间
			// 使用+08:00时区格式以符合用户期望
			var timeStr string

			switch timeType {
			case "hour":
				// 小时级：使用窗口开始时间的整点小时
				timeStr = windowStart.Format("2006-01-02T15:04:05.000+08:00")
			case "day":
				// 天级：使用窗口开始时间的日期
				timeStr = windowStart.Format("2006-01-02T15:04:05.000+08:00")
			case "week":
				// 周级：使用窗口开始时间
				timeStr = windowStart.Format("2006-01-02T15:04:05.000+08:00")
			case "month":
				// 月级：使用窗口开始时间
				timeStr = windowStart.Format("2006-01-02T15:04:05.000+08:00")
			case "year":
				// 年级：使用窗口开始时间
				timeStr = windowStart.Format("2006-01-02T15:04:05.000+08:00")
			default:
				timeStr = windowStart.Format("2006-01-02T15:04:05.000+08:00")
			}

			results = append(results, map[string]interface{}{
				"timestamp": windowStartMs,
				"time":      timeStr,
				"value":     *diffValue,
			})
		}
	}

	// 按时间顺序排序（最早的在前）
	sort.Slice(results, func(i, j int) bool {
		ti, _ := results[i]["timestamp"].(int64)
		tj, _ := results[j]["timestamp"].(int64)
		return ti < tj
	})

	return results, nil
}

// getDiffValueInTimeWindow 获取指定时间窗口内的差值
func getDiffValueInTimeWindow(deviceId, key string, startTime, endTime int64) (*float64, error) {
	q := query.TelemetryData
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(deviceId))
	queryBuilder = queryBuilder.Where(q.Key.Eq(key))
	queryBuilder = queryBuilder.Where(q.T.Between(startTime, endTime))

	// 查询最新的值 - 获取number_v和string_v字段
	var latestData []map[string]interface{}
	err := queryBuilder.Select(q.NumberV.As("number_v"), q.StringV.As("string_v")).Order(q.T.Desc()).Limit(1).Scan(&latestData)
	if err != nil {
		return nil, err
	}

	// 查询最旧的值 - 获取number_v和string_v字段
	var oldestData []map[string]interface{}
	err = queryBuilder.Select(q.NumberV.As("number_v"), q.StringV.As("string_v")).Order(q.T.Asc()).Limit(1).Scan(&oldestData)
	if err != nil {
		return nil, err
	}

	// 如果没有数据
	if len(latestData) == 0 || len(oldestData) == 0 {
		return nil, nil
	}

	// 获取最新值
	latestValue, err := extractNumericValue(latestData[0])
	if err != nil {
		logrus.Error("提取最新数值失败:", err)
		return nil, nil
	}

	// 获取最旧值
	oldestValue, err := extractNumericValue(oldestData[0])
	if err != nil {
		logrus.Error("提取最旧数值失败:", err)
		return nil, nil
	}

	diff := latestValue - oldestValue
	return &diff, nil
}

// extractNumericValue 从数据记录中提取数值
func extractNumericValue(data map[string]interface{}) (float64, error) {
	// 优先使用number_v字段
	if numberV, exists := data["number_v"]; exists && numberV != nil {
		if val, ok := numberV.(float64); ok {
			return val, nil
		}
	}

	// 如果number_v不存在或为空，尝试从string_v转换
	if stringV, exists := data["string_v"]; exists && stringV != nil {
		if strVal, ok := stringV.(string); ok && strVal != "" {
			// 尝试转换字符串为数字
			if floatVal, err := strconv.ParseFloat(strVal, 64); err == nil {
				return floatVal, nil
			} else {
				return 0, fmt.Errorf("无法将字符串 '%s' 转换为数字: %v", strVal, err)
			}
		}
	}

	return 0, fmt.Errorf("未找到有效的数值数据")
}
