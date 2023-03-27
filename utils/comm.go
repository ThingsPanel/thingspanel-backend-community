package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func TsKvFilterToSql(filters map[string]interface{}) (string, []interface{}) {
	SQL := " WHERE 1=1 "
	params := []interface{}{}
	for key, value := range filters {
		switch key {
		case "start_date":
			SQL = fmt.Sprintf("%s and ts_kv.ts >= ?", SQL)
			params = append(params, value)
		case "end_date":
			SQL = fmt.Sprintf("%s and ts_kv.ts < ?", SQL)
			params = append(params, value)
		case "business_id":
			SQL = fmt.Sprintf("%s and business.id = ?", SQL)
			params = append(params, value)
		case "asset_id":
			SQL = fmt.Sprintf("%s and asset.id = ?", SQL)
			params = append(params, value)
		case "token":
			SQL = fmt.Sprintf("%s and device.token = ?", SQL)
			params = append(params, value)
		}
	}
	return SQL, params
}

func WidgetsToSql(filters map[string]interface{}) (string, []interface{}) {
	SQL := "1=1"
	params := []interface{}{}
	for key, value := range filters {
		switch key {
		case "dashboard_id":
			SQL = fmt.Sprintf("%s and dashboard_id = ?", SQL)
			params = append(params, value)
		case "asset_id":
			SQL = fmt.Sprintf("%s and asset_id = ?", SQL)
			params = append(params, value)
		case "device_id":
			SQL = fmt.Sprintf("%s and device_id = ?", SQL)
			params = append(params, value)
		}
	}
	return SQL, params
}

//用户输入组合路径安全校验
func CheckPath(param string) error {
	if count := strings.Count(param, "."); count > 0 {
		return errors.New("路径中不能包含非法字符“.”")
	}
	if count := strings.Count(param, "/"); count > 0 {
		return errors.New("路径中不能包含非法字符“/”")
	}
	if count := strings.Count(param, "\\"); count > 0 {
		return errors.New("路径中不能包含非法字符“\\”")
	}
	return nil
}

//用户输入文件名安全校验
func CheckFilename(param string) error {
	if count := strings.Count(param, "."); count > 1 {
		return errors.New("文件名中不能超过一个“.”")
	}
	if count := strings.Count(param, "/"); count > 0 {
		return errors.New("文件名中不能包含非法字符“/”")
	}
	if count := strings.Count(param, "\\"); count > 0 {
		return errors.New("文件名中不能包含非法字符“\\”")
	}
	return nil
}

//用户文件全路径安全校验
func CheckPathFilename(param string) error {
	if count := strings.Count(param, "."); count > 2 {
		return errors.New("文件全路径中不能超过两个“.”")
	}
	if count := strings.Count(param, "/"); count > 5 {
		return errors.New("文件全路径中不能包含非法字符“/”")
	}
	if count := strings.Count(param, "\\"); count > 0 {
		return errors.New("文件全路径中不能包含非法字符“\\”")
	}
	return nil
}

//字符串替换非法字符
func ReplaceUserInput(s string) string {
	newStringInput := strings.NewReplacer("\n", " ", "\r", " ")
	return newStringInput.Replace(s)
}

//字符包含非法字符
func ContainsIllegal(target string) bool {
	var str_array [3]string = [3]string{"/", "./", "\\"}
	for _, element := range str_array {
		if strings.Contains(target, element) {
			return true
		}
	}
	return false
}

//文件md5计算
func FileSign(filePath string, sign string) (string, error) {
	check_err := CheckPathFilename(filePath)
	if check_err != nil {
		return "", check_err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	if sign == "MD5" {
		hash := md5.New()
		_, _ = io.Copy(hash, file)
		return hex.EncodeToString(hash.Sum(nil)), nil
	} else {
		hash := sha256.New()
		_, _ = io.Copy(hash, file)
		return hex.EncodeToString(hash.Sum(nil)), nil
	}

}

func GetFileSize(filePath string) (int64, error) {
	check_err := CheckPathFilename(filePath)
	if check_err != nil {
		return 0, check_err
	}
	fi, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// 字节的单位转换 保留两位小数
func FormatFileSize(fileSize int64) (size string) {
	if fileSize < 1024 {
		//return strconv.FormatInt(fileSize, 10) + "B"
		return fmt.Sprintf("%.2fB", float64(fileSize)/float64(1))
	} else if fileSize < (1024 * 1024) {
		return fmt.Sprintf("%.2fKB", float64(fileSize)/float64(1024))
	} else if fileSize < (1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fMB", float64(fileSize)/float64(1024*1024))
	} else if fileSize < (1024 * 1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fGB", float64(fileSize)/float64(1024*1024*1024))
	} else if fileSize < (1024 * 1024 * 1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fTB", float64(fileSize)/float64(1024*1024*1024*1024))
	} else { //if fileSize < (1024 * 1024 * 1024 * 1024 * 1024 * 1024)
		return fmt.Sprintf("%.2fEB", float64(fileSize)/float64(1024*1024*1024*1024*1024))
	}
}
