package utils

import (
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	beego "github.com/beego/beego/v2/adapter"
	"golang.org/x/crypto/bcrypt"
)

// KeyInMap 模仿php的array_key_exists,判断是否存在map中
func KeyInMap(key string, m map[string]interface{}) bool {
	_, ok := m[key]
	if ok {
		return true
	}
	return false
}

// InArrayForString 模仿php的in_array,判断是否存在string数组中
func InArrayForString(items []string, item string) bool {
	for _, eachItem := range items {
		if eachItem == item {
			return true
		}
	}
	return false
}

// InArrayForInt 模仿php的in_array,判断是否存在int数组中
func InArrayForInt(items []int, item int) bool {
	for _, eachItem := range items {
		if eachItem == item {
			return true
		}
	}
	return false
}

// PasswordHash php的函数password_hash
func PasswordHash(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// PasswordVerify php的函数password_verify
func PasswordVerify(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// IntArrToStringArr int数组转string数组
func IntArrToStringArr(arr []int) []string {
	var stringArr []string
	for _, v := range arr {
		stringArr = append(stringArr, strconv.Itoa(v))
	}
	return stringArr
}

// GetMd5String 对字符串进行MD5哈希
func GetMd5String(str string) string {
	t := md5.New()
	io.WriteString(t, str)
	return fmt.Sprintf("%x", t.Sum(nil))
}

// GetSha1String 对字符串进行SHA1哈希
func GetSha1String(str string) string {
	t := sha1.New()
	io.WriteString(t, str)
	return fmt.Sprintf("%x", t.Sum(nil))
}

// ParseName 字符串命名风格转换
func ParseName(name string, ptype int, ucfirst bool) string {
	if ptype > 0 {
		//解释正则表达式
		reg := regexp.MustCompile(`_([a-zA-Z])`)
		if reg == nil {
			beego.Error("MustCompile err")
			return ""
		}
		//提取关键信息
		result := reg.FindAllStringSubmatch(name, -1)
		for _, v := range result {
			name = strings.ReplaceAll(name, v[0], strings.ToUpper(v[1]))
		}

		if ucfirst {
			return Ucfirst(name)
		}
		return Lcfirst(name)
	}
	//解释正则表达式
	reg := regexp.MustCompile(`[A-Z]`)
	if reg == nil {
		beego.Error("MustCompile err")
		return ""
	}
	//提取关键信息
	result := reg.FindAllStringSubmatch(name, -1)

	for _, v := range result {
		name = strings.ReplaceAll(name, v[0], "_"+v[0])
	}
	return strings.ToLower(name)
}

// Ucfirst 首字母大写
func Ucfirst(str string) string {
	for i, v := range str {
		return string(unicode.ToUpper(v)) + str[i+1:]
	}
	return ""
}

// Lcfirst 首字母小写
func Lcfirst(str string) string {
	for i, v := range str {
		return string(unicode.ToLower(v)) + str[i+1:]
	}
	return ""
}
