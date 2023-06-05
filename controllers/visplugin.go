package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpVisPluginController struct {
	beego.Controller
}

// 列表
func (c *TpVisPluginController) List() {
	reqData := valid.TpVisPluginPaginationValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var tpvisplugin services.TpVis
	isSuccess, d, t := tpvisplugin.GetTpVisPluginList(reqData, tenantId)

	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpOtaPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 上传
func (c *TpVisPluginController) Upload() {
	plugin_name := c.GetString("plugin_name")

	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	files, err := c.GetFiles("files")
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	var visfiles []map[string]string
	for i := range files {
		file, err := files[i].Open()
		if err != nil {
			utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
			return
		}
		defer file.Close()
		//创建目录
		uploadDir := "./files/visplugin/" + time.Now().Format("2006-01-02/")
		err = os.MkdirAll(uploadDir, os.ModePerm)
		if err != nil {
			response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
			return
		}
		//构造文件名称
		rand.Seed(time.Now().UnixNano())
		randNum := fmt.Sprintf("%d", rand.Intn(9999)+1000)
		hashName := md5.Sum([]byte(time.Now().Format("2006_01_02_15_04_05_") + randNum))
		ext := path.Ext(files[i].Filename)
		fileName := fmt.Sprintf("%x", hashName) + ext
		err = utils.CheckFilename(fileName)
		if err != nil {
			response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
			return
		}
		fpath := uploadDir + fileName

		dst, err := os.Create(fpath)
		if err != nil {
			response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, file); err != nil {
			response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
			return
		}

		visfiles = append(visfiles, map[string]string{
			"file_name": files[i].Filename,
			"file_url":  fpath,
			"file_size": fmt.Sprintf("%d", files[i].Size),
		})
	}
	var tpvisplugin services.TpVis
	isSuccess := tpvisplugin.UploadTpVisPlugin(plugin_name, tenantId, visfiles)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "上传失败", (*context2.Context)(c.Ctx))
		return
	}
	utils.SuccessWithMessage(200, "上传成功", (*context2.Context)(c.Ctx))

}
