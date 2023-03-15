package controllers

import (
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	"crypto/md5"
	"fmt"
	"math/rand"
	"os"
	"path"
	"time"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type UploadController struct {
	beego.Controller
}

// func (uploadController *UploadController) UpForm() {
// 	uploadController.TplName = "upload.tpl"
// }

func (uploadController *UploadController) UpFile() {
	fileType := uploadController.GetString("type")
	if fileType == "" {
		response.SuccessWithMessage(1000, "类型为空", (*context2.Context)(uploadController.Ctx))
	} else {
		err := utils.CheckPath(fileType)
		if err != nil {
			response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(uploadController.Ctx))
		}
	}
	f, h, _ := uploadController.GetFile("file") //获取上传的文件
	ext := path.Ext(h.Filename)
	//验证后缀名是否符合要求
	var AllowExtMap map[string]bool = map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".svg":  true,
		".ico":  true,
		".gif":  true,
		".bin":  true,
		".tar":  true,
		".gz":   true,
		".zip":  true,
		".gzip": true,
		".apk":  true,
		".dav":  true,
		".pack": true,
	}
	if _, ok := AllowExtMap[ext]; !ok {
		response.SuccessWithMessage(1000, "文件类型不正确", (*context2.Context)(uploadController.Ctx))
	}
	//创建目录

	uploadDir := "./files/" + fileType + "/" + time.Now().Format("2006-01-02/")
	err := os.MkdirAll(uploadDir, os.ModePerm)
	if err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(uploadController.Ctx))
	}
	//构造文件名称
	rand.Seed(time.Now().UnixNano())
	randNum := fmt.Sprintf("%d", rand.Intn(9999)+1000)
	hashName := md5.Sum([]byte(time.Now().Format("2006_01_02_15_04_05_") + randNum))
	fileName := fmt.Sprintf("%x", hashName) + ext
	err = utils.CheckFilename(fileName)
	if err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(uploadController.Ctx))
	}
	fpath := uploadDir + fileName
	defer f.Close() //关闭上传的文件，不然的话会出现临时文件不能清除的情况
	err = uploadController.SaveToFile("file", fpath)
	if err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(uploadController.Ctx))
	}
	response.SuccessWithDetailed(200, "success", fpath, map[string]string{}, (*context2.Context)(uploadController.Ctx))
}
