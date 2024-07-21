package common

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron"
)

func GetToday() time.Time {
	now := time.Now()
	year, month, day := now.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

func GetYearStart() time.Time {
	now := time.Now()
	year, _, _ := now.Date()
	return time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)
}

func GetMonthStart() time.Time {
	now := time.Now()
	year, month, _ := now.Date()
	return time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
}

func GetYesterdayBegin() time.Time {
	return GetToday().Add(-1)
}

func DateTimeToString(date time.Time, Layout string) string {
	if Layout == "" {
		Layout = "2006-01-02 15:04:05"
	}
	return date.Format(Layout)
}

func GetWeekDay(date time.Time) int {
	currentWeekday := date.Weekday()
	var weekday int

	switch currentWeekday {
	case time.Sunday:
		weekday = 7
	case time.Monday:
		weekday = 1
	case time.Tuesday:
		weekday = 2
	case time.Wednesday:
		weekday = 3
	case time.Thursday:
		weekday = 4
	case time.Friday:
		weekday = 5
	case time.Saturday:
		weekday = 6
	}
	return weekday
}

func GetSceneExecuteTime(taskType, condition string) (time.Time, error) {
	var (
		result time.Time
		now    = time.Now()
		err    error
	)
	switch taskType {
	case "HOUR":
		minstr := condition[:2]
		var min int
		min, err = strconv.Atoi(minstr)
		if err != nil || min > 59 || min < 0 {
			return result, errors.New("时间格式错误")
		}
		if min > now.Minute() {
			result = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), min, 0, 0, now.Location())
		} else {
			result = time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, min, 0, 0, now.Location())
		}

	case "DAY":
		daytime, err := time.Parse("15:04:05-07:00", condition)
		if err != nil {
			return result, errors.New("时间格式错误")
		}
		result = time.Date(now.Year(), now.Month(), now.Day(), daytime.Hour(), daytime.Minute(), daytime.Second(), 0, now.Location())
		if result.Before(now) {
			result = result.Add(time.Hour * 24)
		}
	case "WEEK":
		parts := strings.Split(condition, "|")
		if len(parts) != 2 {
			return result, errors.New("时间格式错误")
		}
		// 解析星期
		weekdaysStr := parts[0]
		weekdays := make([]time.Weekday, 0)
		for _, char := range weekdaysStr {
			if char >= '1' && char <= '7' {
				day, _ := strconv.Atoi(string(char))
				weekdays = append(weekdays, time.Weekday(day))
			}
		}
		// 解析时间
		timeStr := parts[1]
		targetTime, err := time.Parse("15:04:05-07:00", timeStr)
		if err != nil {
			return result, errors.New("时间格式错误")
		}
		result = getNextTime(now, weekdays, targetTime)
	case "MONTH":
		// 解析时间字符串
		targetTime, err := time.Parse("2T15:04:05-07:00", condition)
		if err != nil {
			return result, errors.New("时间解析错误：")
		}
		result = getMonthNextTime(now, targetTime)
	case "CRON":
		specParser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.DowOptional | cron.Descriptor)
		schedule, err := specParser.Parse(condition)
		if err != nil {
			return result, errors.New("cron格式错误")
		}
		result = schedule.Next(now)
	default:
		return result, errors.New("未支持的时间格式")
	}

	return result, err

}

func getNextTime(now time.Time, weekdays []time.Weekday, targetTime time.Time) time.Time {
	// 获取当前时间的年、月、日和星期几
	year, month, day := now.Date()

	// 从当前时间开始往后找
	for i := 0; i < 7; i++ {
		// 计算下一个满足条件的日期
		nextDay := now.AddDate(0, 0, i)
		nextWeekday := nextDay.Weekday()
		for _, wd := range weekdays {
			if (wd - 1) == nextWeekday {
				// 设置下一个满足条件的时间
				nextTime := time.Date(year, month, day, targetTime.Hour(), targetTime.Minute(), targetTime.Second(), 0, time.Local)
				// 如果时间在当前时间之后，则返回这个时间
				if nextTime.After(now) {
					return nextTime
				}
			}
		}
	}

	// 如果没有找到满足条件的时间，则返回下周的第一个满足条件的时间
	nextDay := now.AddDate(0, 0, 6-GetWeekDay(now))
	nextWeekday := nextDay.Weekday()
	for _, wd := range weekdays {
		if (wd - 1) == nextWeekday {
			// 设置下一个满足条件的时间
			nextTime := time.Date(year, month, day+6-GetWeekDay(now), targetTime.Hour(), targetTime.Minute(), targetTime.Second(), 0, time.Local)
			return nextTime
		}
	}
	return time.Time{}
}

// 获取下一个满足条件的时间
func getMonthNextTime(now time.Time, targetTime time.Time) time.Time {
	// 获取当前时间的年、月、日和时、分、秒
	year, month, _ := now.Date()
	//targetMonth := targetTime.Month()
	targetDay := targetTime.Day()
	// 计算下一个满足条件的时间
	var nextTime time.Time
	if now.Day() <= targetDay {
		// 如果当前日期小于目标日期或者当前月份小于目标月份，则下一个时间点是本月的目标日期
		nextTime = time.Date(year, month, targetDay, targetTime.Hour(), targetTime.Minute(), targetTime.Second(), 0, time.Local)
	} else {
		// 否则下一个时间点是下个月的目标日期
		nextTime = time.Date(year, month+1, targetDay, targetTime.Hour(), targetTime.Minute(), targetTime.Second(), 0, time.Local)
	}

	// 如果时间在当前时间之前，则加一个月
	if nextTime.Before(now) {
		nextTime = nextTime.AddDate(0, 1, 0)
	}

	return nextTime
}
