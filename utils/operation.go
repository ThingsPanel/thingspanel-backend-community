package utils

import (
	"errors"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/spf13/cast"
)

// 运算
func Check(value1 interface{}, symbol string, value2 interface{}) (bool, error) {
	logs.Error(value1, symbol, value2)
	var valueB string
	if v, ok := value2.(string); ok {
		valueB = v
	} else {
		logs.Error("比较的值格式不是string")
		return false, errors.New("比较的值格式不是string")
	}
	if valueA, ok := value1.(string); ok {
		//string
		switch symbol {
		case "==":
			if valueA == valueB {
				return true, nil
			}
		case ">":
			if valueA > valueB {
				return true, nil
			}
		case "<":
			if valueA < valueB {
				return true, nil
			}
		case ">=":
			if valueA >= valueB {
				return true, nil
			}
		case "<=":
			if valueA <= valueB {
				return true, nil
			}
		case "in":
			return In(valueA, strings.Split(valueB, ",")), nil
		case "between":
			sList := strings.Split(valueB, ",")
			if valueA > sList[0] && valueA < sList[1] {
				return true, nil
			}
		default:
			logs.Error("运算符错误")
			return false, errors.New("运算符错误")
		}
		return false, nil

	} else if valueA, ok := value1.(float64); ok {
		//float64
		switch symbol {
		case "==":

			if valueA == cast.ToFloat64(valueB) {
				return true, nil
			}
		case ">":
			if valueA > cast.ToFloat64(valueB) {
				return true, nil
			}
		case "<":
			if valueA < cast.ToFloat64(valueB) {
				return true, nil
			}
		case ">=":
			if valueA >= cast.ToFloat64(valueB) {
				return true, nil
			}
		case "<=":
			if valueA <= cast.ToFloat64(valueB) {
				return true, nil
			}
		case "in":
			sList := strings.Split(valueB, ",")
			for _, v := range sList {
				if valueA == cast.ToFloat64(v) {
					return true, nil
				}
			}
		case "between":
			sList := strings.Split(valueB, ",")
			if valueA > cast.ToFloat64(sList[0]) && valueA < cast.ToFloat64(sList[1]) {
				return true, nil
			}
		default:
			logs.Error("运算符错误")
			return false, errors.New("运算符错误")
		}
		return false, nil
	} else {
		//不是字符串也不是float64
		logs.Error("设备上报的值非string,float64")
		return false, errors.New("设备上报的值非string,float64")
	}

}

//时间范围
func CheckTime(startTime string, endTime string) (bool, error) {
	format := "2006-01-02 15:04:05"
	now, _ := time.Parse(format, time.Now().Format(format))
	// string转日期
	startTimeA, err := time.Parse(format, startTime)
	if err != nil {
		logs.Error(err.Error())
		return false, err
	}
	endTimeA, err := time.Parse(format, endTime)
	if err != nil {
		logs.Error(err.Error())
		return false, err
	}
	if startTimeA.Before(now) && now.Before(endTimeA) {
		return true, nil
	}
	return false, nil
}

func In(target string, str_array []string) bool {
	for _, element := range str_array {
		if target == element {
			return true
		}
	}
	return false
}
