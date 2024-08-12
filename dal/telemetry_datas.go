package dal

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"time"

	global "project/global"
	model "project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	tptodb "project/grpc/tptodb_client"
	pb "project/grpc/tptodb_client/grpc_tptodb"
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
		data := []model.TelemetryData{}
		//获取当前设备的第一条数据
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

	// if p.Page != 0 && p.PageSize != 0 {
	// 	queryBuilder = queryBuilder.Limit(p.PageSize)
	// 	queryBuilder = queryBuilder.Offset((p.Page - 1) * p.PageSize)
	// }

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
		//时间戳转time.Time
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
	return err
}

// 非聚合查询(req.DeviceID, req.Key, req.StartTime, req.EndTime)
func GetTelemetrStatisticData(deviceID, key string, startTime, endTime int64) (interface{}, error) {
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

	//pg数据库进行聚合查询
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
