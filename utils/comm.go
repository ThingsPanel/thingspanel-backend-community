package utils

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cast"
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

// 提取url中的路径
func GetUrlPath(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		logs.Error("url parse error: %v", err)
		return ""
	}
	return parsedURL.Path
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

// GenerateAppKey 生成指定字节长度的随机字符串
func GenerateAppKey(length int) string {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// 计算定时任务下次执行时间
func GetNextTime(v1 string, v2 string, v3 string, v4 string) (string, error) {
	var nextTime string
	var err error
	var cronString string
	if v1 == "0" {
		//几分钟
		number := cast.ToInt(v3)
		if number > 0 {
			cronString = "0/" + v3 + " * * * *"
		} else {
			logs.Error("cron按分钟不能为空或0")
			return nextTime, errors.New("cron按分钟不能为空或0")
		}
	} else if v1 == "1" {
		// 每小时的几分
		number := cast.ToInt(v3)
		cronString = cast.ToString(number) + " 0/1 * * *"
	} else if v1 == "2" {
		// 每天的几点几分
		timeList := strings.Split(v3, ":")
		cronString = timeList[1] + " " + timeList[0] + " * * *"
	} else if v1 == "3" {
		// 星期几的几点几分
		timeList := strings.Split(v3, ":")
		if len(timeList) >= 2 {
			cronString = timeList[1] + " " + timeList[0] + " * * " + v4
		} else {
			return nextTime, errors.New("配置错误")
		}

	} else if v1 == "4" {
		// 每月的哪一天的几点几分
		timeList := strings.Split(v3, ":")
		cronString = timeList[2] + " " + timeList[1] + " " + timeList[0] + " * *"
	} else if v1 == "5" {
		cronString = v3
	}
	// 打印
	logs.Info("cron表达式：", cronString)
	// 解析 cron 表达式
	specParser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := specParser.Parse(cronString)
	if err != nil {
		logs.Error("cron表达式解析错误")
		return nextTime, err
	}
	// 获取下次执行时间
	nextTime = sched.Next(time.Now()).Format("2006-01-02 15:04:05")
	// 打印下次执行时间
	logs.Info("下次执行时间：", nextTime)
	return nextTime, err
}
