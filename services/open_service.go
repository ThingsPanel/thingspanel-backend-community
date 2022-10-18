package services

import (
	valid "ThingsPanel-Go/validate"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
)

type OpenService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 保存三方数据
func (*OpenService) SaveData(OpenValidate valid.OpenValidate) (bool, error) {
	if len(OpenValidate.Token) == 0 {
		return false, errors.New("data token missing")
	}
	if len(OpenValidate.Values) == 0 {
		return false, errors.New("data values missing")
	}
	valuesMap := OpenValidate.Values
	// 是否存在file，
	if _, ok := valuesMap["file"]; ok {
		// filename是否存在
		if v, ok := valuesMap["filename"]; ok {
			if _, ok := v.(string); !ok {
				return false, errors.New("data values.filename type missing")
			}
			// 保存BASE64文件
			filename, err := SaveBase64File(valuesMap["filename"].(string), valuesMap["file"].(string))
			if err != nil {
				return false, err
			}
			valuesMap["filename"] = filename
			delete(valuesMap, "file")
		} else {
			return false, errors.New("data values.filename inexistence")
		}
	}
	logs.Info("=======================")
	logs.Info(OpenValidate)
	// 解析并存储数据
	OpenValidateByte, _ := json.Marshal(OpenValidate)
	var TSKV TSKVService
	isSucess := TSKV.MsgProc(OpenValidateByte, "")
	if isSucess {
		return isSucess, nil
	} else {
		return isSucess, errors.New("save faild")
	}
}

// 存储base64文件并返回文件名
func SaveBase64File(filename string, fileBase string) (string, error) {
	fileByte, deErr := base64.StdEncoding.DecodeString(fileBase[strings.IndexByte(fileBase, ',')+1:]) //成图片文件并把文件写入到buffer
	if deErr != nil {
		log.Println(deErr)
		return "", deErr
	}
	timeStr := time.Now().Format("2006-01-02")
	os.MkdirAll("./files/outer/"+timeStr, os.ModePerm)
	filePath := "./files/outer/" + timeStr + "/" + filename
	err := ioutil.WriteFile(filePath, fileByte, 0667)
	if err == nil {
		log.Println(err)
		return filePath[1:], err
	} else {
		return "", err
	}

}
