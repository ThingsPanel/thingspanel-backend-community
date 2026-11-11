package dal

import (
	"fmt"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

// StatisticLeaf is the data access that the multi-device statistics need. The window
// arithmetic in StatisticsByDeviceIds is storage independent and mirrors
// GetTelemetryStatisticDataByDeviceIds, which keeps its own PostgreSQL code untouched; a
// differential test keeps the two in step.
type StatisticLeaf interface {
	// Count returns the number of rows with start <= ts <= end.
	Count(deviceID, key string, start, end int64) (int64, error)
	// Aggregate applies avg, sum, max or min to number_v over start <= ts <= end and returns
	// nil when no row has a number.
	Aggregate(deviceID, key, method string, start, end int64) (*float64, error)
	// Edges returns the newest and the oldest row (columns number_v and string_v) in the range.
	Edges(deviceID, key string, start, end int64) (latest, oldest map[string]interface{}, found bool, err error)
}

type statisticWindow struct{ start, end int64 }

// StatisticsByDeviceIds is the storage-independent form of GetTelemetryStatisticDataByDeviceIds.
func StatisticsByDeviceIds(leaf StatisticLeaf, deviceIds []string, keys []string, timeType string, limit *int, aggregateMethod string) ([]map[string]interface{}, error) {
	if len(deviceIds) != len(keys) {
		return nil, fmt.Errorf("设备ID数量与key数量不匹配")
	}
	var results []map[string]interface{}
	endTime := time.Now().UnixNano() / 1e6
	var startTime int64
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
	for i, deviceId := range deviceIds {
		key := keys[i]
		switch aggregateMethod {
		case "count":
			count, err := leaf.Count(deviceId, key, startTime, endTime)
			if err != nil {
				logrus.Error("查询数据计数失败:", err)
				continue
			}
			results = append(results, map[string]interface{}{"device_id": deviceId, "key": key, "count": count})
		case "diff":
			diffData, err := statisticDiffData(leaf, deviceId, key, startTime, endTime, timeType)
			if err != nil {
				logrus.Error("查询差值数据失败:", err)
				continue
			}
			results = append(results, map[string]interface{}{"device_id": deviceId, "key": key, "data": diffData})
		default:
			aggregated, err := statisticAggregatedWithTime(leaf, deviceId, key, startTime, endTime, aggregateMethod, limit, timeType)
			if err != nil {
				logrus.Error("查询聚合数据失败:", err)
				continue
			}
			results = append(results, map[string]interface{}{"device_id": deviceId, "key": key, "data": aggregated})
		}
	}
	return results, nil
}

func statisticAggregatedWithTime(leaf StatisticLeaf, deviceId, key string, startTime, endTime int64, method string, limit *int, timeType string) ([]map[string]interface{}, error) {
	actualLimit := 1
	if limit != nil && *limit > 0 {
		actualLimit = *limit
	}
	var results []map[string]interface{}
	var windows []statisticWindow
	endTimeUnix := time.Unix(0, endTime*int64(time.Millisecond))
	switch timeType {
	case "hour":
		nextHour := time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day(), endTimeUnix.Hour()+1, 0, 0, 0, endTimeUnix.Location())
		for i := 0; i < actualLimit; i++ {
			windowEnd := nextHour.Add(time.Duration(-i) * time.Hour)
			windows = append(windows, statisticWindow{windowEnd.Add(-time.Hour).UnixNano() / 1e6, windowEnd.UnixNano() / 1e6})
		}
	case "day":
		nextDay := time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day()+1, 0, 0, 0, 0, endTimeUnix.Location())
		for i := 0; i < actualLimit; i++ {
			windowEnd := nextDay.Add(time.Duration(-i) * 24 * time.Hour)
			windows = append(windows, statisticWindow{windowEnd.Add(-24*time.Hour).UnixNano() / 1e6, windowEnd.UnixNano() / 1e6})
		}
	case "week":
		nextWeek := endTimeUnix.AddDate(0, 0, 7-int(endTimeUnix.Weekday()))
		nextWeek = time.Date(nextWeek.Year(), nextWeek.Month(), nextWeek.Day(), 0, 0, 0, 0, nextWeek.Location())
		for i := 0; i < actualLimit; i++ {
			windowEnd := nextWeek.Add(time.Duration(-i) * 7 * 24 * time.Hour)
			windows = append(windows, statisticWindow{windowEnd.Add(-7*24*time.Hour).UnixNano() / 1e6, windowEnd.UnixNano() / 1e6})
		}
	case "month":
		year, month, _ := endTimeUnix.Date()
		nextMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, endTimeUnix.Location())
		for i := 0; i < actualLimit; i++ {
			windowEnd := nextMonth.AddDate(0, -i, 0)
			windows = append(windows, statisticWindow{windowEnd.AddDate(0, -1, 0).UnixNano() / 1e6, windowEnd.UnixNano() / 1e6})
		}
	case "year":
		nextYear := time.Date(endTimeUnix.Year()+1, 1, 1, 0, 0, 0, 0, endTimeUnix.Location())
		for i := 0; i < actualLimit; i++ {
			windowEnd := nextYear.AddDate(-i, 0, 0)
			windows = append(windows, statisticWindow{windowEnd.AddDate(-1, 0, 0).UnixNano() / 1e6, windowEnd.UnixNano() / 1e6})
		}
	default:
		windowSizeMs := (endTime - startTime) / int64(actualLimit)
		for i := 0; i < actualLimit; i++ {
			start := startTime + int64(i)*windowSizeMs
			windows = append(windows, statisticWindow{start, start + windowSizeMs})
		}
	}
	switch method {
	case "avg", "sum", "max", "min":
	default:
		return nil, fmt.Errorf("不支持的聚合方式: %s", method)
	}
	for _, window := range windows {
		value, err := leaf.Aggregate(deviceId, key, method, window.start, window.end)
		if err != nil {
			return nil, err
		}
		if value != nil {
			results = append(results, map[string]interface{}{"value": *value, "timestamp": window.start})
		}
	}
	return results, nil
}

func statisticDiffData(leaf StatisticLeaf, deviceId, key string, startTime, endTime int64, timeType string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	loc := time.Local
	endTimeUnix := time.Unix(0, endTime*int64(time.Millisecond)).In(loc)
	var alignedEndTime time.Time
	switch timeType {
	case "hour":
		alignedEndTime = time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day(), endTimeUnix.Hour()+1, 0, 0, 0, loc)
	case "day":
		alignedEndTime = time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day()+1, 0, 0, 0, 0, loc)
	case "week":
		alignedEndTime = time.Date(endTimeUnix.Year(), endTimeUnix.Month(), endTimeUnix.Day()+1, 0, 0, 0, 0, loc)
	case "month":
		alignedEndTime = time.Date(endTimeUnix.Year(), endTimeUnix.Month()+1, 1, 0, 0, 0, 0, loc)
	case "year":
		alignedEndTime = time.Date(endTimeUnix.Year()+1, 1, 1, 0, 0, 0, 0, loc)
	default:
		alignedEndTime = endTimeUnix
	}
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
	if windowCount > 100 {
		windowCount = 100
	}
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
		if windowEndMs <= startTime || windowStartMs >= endTime {
			continue
		}
		actualStart, actualEnd := windowStartMs, windowEndMs
		if actualStart < startTime {
			actualStart = startTime
		}
		if actualEnd > endTime {
			actualEnd = endTime
		}
		diffValue, err := statisticDiffInWindow(leaf, deviceId, key, actualStart, actualEnd)
		if err != nil {
			logrus.Error("查询时间窗口差值失败:", err)
			continue
		}
		if diffValue != nil {
			results = append(results, map[string]interface{}{
				"timestamp": windowStartMs,
				"time":      windowStart.Format("2006-01-02T15:04:05.000+08:00"),
				"value":     *diffValue,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		ti, _ := results[i]["timestamp"].(int64)
		tj, _ := results[j]["timestamp"].(int64)
		return ti < tj
	})
	return results, nil
}

func statisticDiffInWindow(leaf StatisticLeaf, deviceId, key string, start, end int64) (*float64, error) {
	latest, oldest, found, err := leaf.Edges(deviceId, key, start, end)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	latestValue, err := extractNumericValue(latest)
	if err != nil {
		logrus.Error("提取最新数值失败:", err)
		return nil, nil
	}
	oldestValue, err := extractNumericValue(oldest)
	if err != nil {
		logrus.Error("提取最旧数值失败:", err)
		return nil, nil
	}
	diff := latestValue - oldestValue
	return &diff, nil
}
