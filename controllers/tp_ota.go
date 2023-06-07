package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpOtaController struct {
	beego.Controller
}

// 列表
func (c *TpOtaController) List() {
	reqData := valid.TpOtaPaginationValidate{}
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
	var TpOtaService services.TpOtaService
	isSuccess, d, t := TpOtaService.GetTpOtaList(reqData, tenantId)

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

// 新增
func (c *TpOtaController) Add() {
	reqData := valid.AddTpOtaValidate{}
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

	// 判断文件是否存在|| !utils.FileExist(reqData.PackageUrl)
	path := "." + utils.GetUrlPath(reqData.PackageUrl)[17:]
	if !utils.FileExist(path) {
		utils.SuccessWithMessage(400, "升级包不存在", (*context2.Context)(c.Ctx))
		return
	}
	if err := utils.CheckPathFilename(path); err != nil || reqData.PackageUrl == "" {
		utils.SuccessWithMessage(400, "升级包路径不合法或升级包路径是空", (*context2.Context)(c.Ctx))
		return
	}
	//文件sign计算
	packagesign, sign_err := utils.FileSign(path, reqData.SignatureAlgorithm)
	if sign_err != nil {
		utils.SuccessWithMessage(400, "文件签名计算失败", (*context2.Context)(c.Ctx))
		return
	}

	//文件大小检查
	packageLength, pl_err := utils.GetFileSize(path)
	if pl_err != nil {
		utils.SuccessWithMessage(400, "文件大小计算失败", (*context2.Context)(c.Ctx))
		return
	}
	if packageLength > 1024*1024*1024*1024 {
		utils.SuccessWithMessage(400, "文件大小超出1G", (*context2.Context)(c.Ctx))
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpOtaService services.TpOtaService
	id := utils.GetUuid()
	TpOta := models.TpOta{
		Id:                 id,
		PackageName:        reqData.PackageName,
		PackageVersion:     reqData.PackageVersion,
		PackageModule:      reqData.PackageModule,
		ProductId:          reqData.ProductId,
		SignatureAlgorithm: reqData.SignatureAlgorithm,
		PackageUrl:         reqData.PackageUrl,
		Description:        reqData.Description,
		AdditionalInfo:     reqData.AdditionalInfo,
		CreatedAt:          time.Now().Unix(),
		Sign:               packagesign,
		FileSize:           fmt.Sprintf("%d", packageLength),
		TenantId:           tenantId,
	}
	d, rsp_err := TpOtaService.AddTpOta(TpOta)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23505")
		if isTrue {
			err = "有值不能重复！"
		} else {
			err = rsp_err.Error()
		}
		utils.SuccessWithMessage(400, err, (*context2.Context)(c.Ctx))
	}
}

//删除
func (c *TpOtaController) Delete() {
	reqData := valid.TpOtaIdValidate{}
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
	if reqData.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(c.Ctx))
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpOtaService services.TpOtaService
	TpOta := models.TpOta{
		Id:       reqData.Id,
		TenantId: tenantId,
	}
	rsp_err := TpOtaService.DeleteTpOta(TpOta)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}
}
