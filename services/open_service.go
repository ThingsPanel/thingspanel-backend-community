package services

import (
	"encoding/base64"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"
)

type OpenService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
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
