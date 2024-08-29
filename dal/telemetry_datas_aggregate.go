package dal

import (
	"context"
	"fmt"
	global "project/global"
)

type TelemetryDatasAggregate struct {
	AggregateWindow   int64  `json:"aggregate_window"`   // 聚合间隔
	AggregateFunction string `json:"aggregate_function"` // 聚合函数
	STime             int64  `json:"s_time"`
	ETime             int64  `json:"e_time"`
	Count             int64  `json:"count"`
	DeviceID          string `json:"device_id"`
	Key               string `json:"key"`
}

// 聚合查询
func GetTelemetryDatasAggregate(ctx context.Context, telemetryDatasAggregate TelemetryDatasAggregate) ([]map[string]interface{}, error) {
	var data []map[string]interface{}
	var queryString string

	// 根据聚合方法获取不同的查询sql
	switch telemetryDatasAggregate.AggregateFunction {
	case "avg", "max", "min", "sum":
		queryString = GetQueryString1(telemetryDatasAggregate.AggregateFunction)
	case "diff":
		queryString = GetQueryString2(telemetryDatasAggregate.AggregateFunction)

	default:
		return nil, fmt.Errorf("不支持的聚合函数: %s", telemetryDatasAggregate.AggregateFunction)
	}

	resultData := global.DB.Raw(queryString, telemetryDatasAggregate.AggregateWindow, telemetryDatasAggregate.STime, telemetryDatasAggregate.ETime, telemetryDatasAggregate.Key, telemetryDatasAggregate.DeviceID, telemetryDatasAggregate.AggregateWindow).Scan(&data)
	if resultData.Error != nil {
		return nil, resultData.Error
	}

	return data, nil

}

// 获取queryString，支持平均值，最大值，最小值，合
func GetQueryString1(aggregateFunction string) string {
	queryString := fmt.Sprintf(
		`WITH TimeIntervals AS (
				SELECT 
					ts - (ts %% ?) AS x, 
					ROUND(%s(number_v), 4) AS y 
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
		aggregateFunction,
	)
	return queryString
}

// 获取queryString，支持差值计算
func GetQueryString2(aggregateFunction string) string {

	queryString := fmt.Sprintf(
		`WITH TimeIntervals AS (
				SELECT 
					ts - (ts %% ?) AS x, 
					MAX(number_v) - MIN(number_v) AS y 
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
				x ASC;`,
	)

	return queryString
}
