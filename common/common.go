package common

import (
	"encoding/json"
	"math/rand"
	constant "project/constant"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

func CheckEmpty(str string) bool {
	if str == constant.EMPTY {
		return true
	}
	return false
}

func GetMessageID() string {
	// 获取当前Unix时间戳
	timestamp := time.Now().Unix()
	// 将时间戳转换为字符串
	timestampStr := strconv.FormatInt(timestamp, 10)
	// 截取后七位
	messageID := timestampStr[len(timestampStr)-7:]

	return messageID
}

// JsonToString
// @AUTH:zxq
// @DATE:2024-03-11 11:00:00
// @DESCRIPTION: 正常 json 转 string，注意，这里json 是没有问题的
func JsonToString(any any) (string, error) {
	data, err := json.Marshal(any)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func GetErrors(err error, message string) error {
	return errors.WithMessage(err, message)
}

// 主题响应返回内容
func GetResponsePayload(method string, err error) []byte {
	//成功示例：{"result":0,"message":"success","ts":1609143039}
	//失败示例：{"result":1,"errcode":"xxx","message":"xxxxxx","ts":1609143039}
	// 返回[]byte
	if err != nil {
		data := map[string]interface{}{
			"result":  1,
			"errcode": "000",
			"message": err.Error(),
			"ts":      time.Now().Unix(),
		}
		res, _ := json.Marshal(data)
		return res
	}
	data := map[string]interface{}{
		"result":  0,
		"message": "success",
		"ts":      time.Now().Unix(),
	}
	if method != "" {
		data["method"] = method
	}
	res, _ := json.Marshal(data)
	return res
}

func StringSpt(str string) *string {
	return &str
}

func IsStringEmpty(str *string) bool {
	return str == nil || *str == ""
}

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateRandomString(length int) string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}
