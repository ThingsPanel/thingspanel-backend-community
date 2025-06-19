package dal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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
	return query.TelemetryData.CreateInBatches(data, len(data))
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

	switch timeType {
	case "hour":
		startTime = endTime - int64(time.Hour.Milliseconds())
	case "day":
		startTime = endTime - int64(24*time.Hour.Milliseconds())
	case "week":
		startTime = endTime - int64(7*24*time.Hour.Milliseconds())
	case "month":
		startTime = endTime - int64(30*24*time.Hour.Milliseconds())
	case "year":
		startTime = endTime - int64(365*24*time.Hour.Milliseconds())
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
			aggregatedData, err := getAggregatedDataWithTime(deviceId, key, startTime, endTime, aggregateMethod, limit)
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

// getAggregatedDataWithTime 获取聚合数据
func getAggregatedDataWithTime(deviceId, key string, startTime, endTime int64, aggregateMethod string, limit *int) ([]map[string]interface{}, error) {
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

	return result, nil
}

// getDiffData 获取差值数据 - 按时间窗口分组计算每个窗口内最新值减去最旧值
func getDiffData(deviceId, key string, startTime, endTime int64, timeType string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}

	// 根据时间类型确定分组间隔
	var groupInterval int64
	var dateFormat string

	switch timeType {
	case "hour":
		groupInterval = int64(time.Hour.Milliseconds())
		dateFormat = "2006-01-02 15:00:00"
	case "day":
		groupInterval = int64(24 * time.Hour.Milliseconds())
		dateFormat = "2006-01-02"
	case "week":
		groupInterval = int64(7 * 24 * time.Hour.Milliseconds())
		dateFormat = "2006-01-02" // 显示周的开始日期
	case "month":
		groupInterval = int64(30 * 24 * time.Hour.Milliseconds())
		dateFormat = "2006-01"
	case "year":
		groupInterval = int64(365 * 24 * time.Hour.Milliseconds())
		dateFormat = "2006"
	default:
		return nil, fmt.Errorf("不支持的时间类型: %s", timeType)
	}

	// 按时间窗口遍历
	currentTime := startTime
	for currentTime < endTime {
		windowEndTime := currentTime + groupInterval
		if windowEndTime > endTime {
			windowEndTime = endTime
		}

		// 查询当前时间窗口内的最新和最旧值
		diffValue, err := getDiffValueInTimeWindow(deviceId, key, currentTime, windowEndTime)
		if err != nil {
			logrus.Error("查询时间窗口差值失败:", err)
			currentTime = windowEndTime
			continue
		}

		// 如果有数据，添加到结果中
		if diffValue != nil {
			// 格式化时间
			timeStr := time.Unix(0, currentTime*int64(time.Millisecond)).Format(dateFormat)

			results = append(results, map[string]interface{}{
				"timestamp": currentTime,
				"time":      timeStr,
				"value":     *diffValue,
			})
		}

		currentTime = windowEndTime
	}

	return results, nil
}

// getDiffValueInTimeWindow 获取指定时间窗口内的差值
func getDiffValueInTimeWindow(deviceId, key string, startTime, endTime int64) (*float64, error) {
	q := query.TelemetryData
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(deviceId))
	queryBuilder = queryBuilder.Where(q.Key.Eq(key))
	queryBuilder = queryBuilder.Where(q.T.Between(startTime, endTime))

	// 查询最新的值 - 同时获取number_v和string_v字段
	var latestData []map[string]interface{}
	err := queryBuilder.Select(q.NumberV.As("number_v"), q.StringV.As("string_v")).Order(q.T.Desc()).Limit(1).Scan(&latestData)
	if err != nil {
		return nil, err
	}

	// 查询最旧的值 - 同时获取number_v和string_v字段
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
