package utils

// 运算
func Check(value1 interface{}, symbol string, value2 interface{}) bool {

	return true
}

//时间范围
func CheckTime(startTime string, endTime string) bool {
	return true
}

func In(target string, str_array []string) bool {
	for _, element := range str_array {
		if target == element {
			return true
		}
	}
	return false
}
