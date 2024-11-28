package common

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	constant "project/pkg/constant"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

func CheckEmpty(str string) bool {
	return str == constant.EMPTY
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

func GenerateRandomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	for i := range b {
		b[i] = charset[b[i]%byte(len(charset))]
	}
	return string(b), nil
}

var ErrNoRows = errors.New("record not found")

func GetRandomNineDigits() (string, error) {
	// 生成 [100000000, 999999999] 范围内的随机数
	min := big.NewInt(100000000)
	max := big.NewInt(999999999)

	// 计算范围大小
	diff := new(big.Int).Sub(max, min)
	diff = diff.Add(diff, big.NewInt(1))

	// 生成随机数
	n, err := rand.Int(rand.Reader, diff)
	if err != nil {
		return "", fmt.Errorf("生成随机数失败: %v", err)
	}

	// 加上最小值以确保在正确范围内
	n = n.Add(n, min)

	// 转换为字符串
	return n.String(), nil
}

func GenerateNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("长度必须大于0")
	}

	// 构建验证码
	code := make([]byte, length)

	for i := 0; i < length; i++ {
		// 生成 0-9 的随机数
		num, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", fmt.Errorf("生成随机数字失败: %v", err)
		}

		// 转换为字符并添加到验证码中
		code[i] = byte(num.Int64() + '0')
	}

	return string(code), nil
}
