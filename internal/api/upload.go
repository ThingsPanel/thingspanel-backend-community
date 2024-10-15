package api

import (
	"crypto/md5"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type UpLoadApi struct{}

// UpFile
// @Tags     文件上传
// @Summary  上传文件
// @Description 上传文件
// @Description type: ota升级包：upgradePackage，批量导入：importBatch，插件：d_plugin，
// @Description 其他：随便填，会在files目录下创建对应的文件夹，文件夹名为type值
// @Produce   application/json
// @Param	type formData  string true "类型(ota升级包：upgradePackage，批量导入：importBatch，插件：d_plugin，其他：随便填))"
// @Param	file formData file true "file"
// @Success  200    {object}  ApiResponse  "上传成功"
// @Failure  400  {object}  ApiResponse  "无效的请求"
// @Failure  422  {object}  ApiResponse  "文件类型验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/file/up [post]
func (a *UpLoadApi) UpFile(c *gin.Context) {
	//从请求中获取type
	//从请求中获取file
	file, err := c.FormFile("file")
	fileType, e := c.GetPostForm("type")
	if !e {
		ErrorHandler(c, http.StatusBadRequest, errors.New("无效的请求"))
		return
	}
	if fileType == "" {
		ErrorHandler(c, http.StatusBadRequest, errors.New("无效的请求"))
		return
	} else {
		//检查文件路径是否合法
		if err := utils.CheckPath(fileType); err != nil {
			ErrorHandler(c, http.StatusUnprocessableEntity, err)
			return
		}

	}

	if err != nil {
		ErrorHandler(c, http.StatusBadRequest, err)
		return
	}
	if file == nil {
		ErrorHandler(c, http.StatusBadRequest, err)
		return
	}

	// 文件后缀校验

	if !utils.ValidateFileType(file.Filename, fileType) {
		ErrorHandler(c, http.StatusUnprocessableEntity, errors.New("文件类型验证失败"))
		return
	}
	//创建目录
	uploadDir := "./files/" + fileType + "/" + time.Now().Format("2006-01-02/")
	//如果没有filepath文件目录就创建一个
	if _, err := os.Stat(uploadDir); err != nil {
		if !os.IsExist(err) {
			os.MkdirAll(uploadDir, os.ModePerm)
		}
	}
	ext := strings.ToLower(path.Ext(file.Filename))
	//构造文件名称
	rand.Seed(time.Now().UnixNano())
	randNum := fmt.Sprintf("%d", rand.Intn(9999)+1000)
	hashName := md5.Sum([]byte(time.Now().Format("2006_01_02_15_04_05_") + randNum))
	fileName := fmt.Sprintf("%x", hashName) + ext
	//文件名合法检查
	err = utils.CheckFilename(fileName)
	if err != nil {
		ErrorHandler(c, http.StatusBadRequest, err)
		return
	}
	fpath := uploadDir + fileName
	//上传文件
	if err := c.SaveUploadedFile(file, uploadDir+fileName); err != nil {
		ErrorHandler(c, http.StatusUnprocessableEntity, err)
		return
	}
	if fileType == "upgradePackage" {
		fpath = "./api/v1/ota/download/files/" + fileType + "/" + time.Now().Format("2006-01-02/") + fileName
	}
	fpathmap := make(map[string]interface{})
	fpathmap["path"] = fpath
	SuccessHandler(c, "上传成功", fpathmap)
}
