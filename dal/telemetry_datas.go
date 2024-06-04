package dal

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	global "project/global"
	model "project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
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
		//时间戳转time.Time
		dc.T = time.Unix(0, d.T*int64(time.Millisecond)).UTC()
		dc.TenantID = d.TenantID
		info, err := query.TelemetryCurrentData.
			Where(query.TelemetryCurrentData.DeviceID.Eq(d.DeviceID)).
			Where(query.TelemetryCurrentData.Key.Eq(d.Key)).
			Updates(dc)
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
	queryString := fmt.Sprintf(
		`WITH TimeIntervals AS (
				SELECT 
					ts - (ts %% ?) AS x, 
					%s(number_v) AS y 
				FROM 
					telemetry_datas 
				WHERE 
					ts BETWEEN ? AND ? AND key = ? AND device_id = ? 
				GROUP BY 
					x
			)
			SELECT 
				x, 
				x + ? AS x2, 
				y 
			FROM 
				TimeIntervals 
			WHERE 
				y IS NOT NULL 
			ORDER BY 
				x asc;`,
		aggregateFunc,
	)
	resultData := global.DB.Raw(queryString, aggregateWindow, sTime, eTime, key, deviceId, aggregateWindow).Scan(&data)
	if resultData.Error != nil {
		return nil, resultData.Error
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

// 支持的统计方法
var StatisticAggregateFunction = map[string]string{
	"max":  "MAX",
	"avg":  "AVG",
	"test": "",
}
