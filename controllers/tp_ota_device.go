package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpOtaDeviceController struct {
	beego.Controller
}

// 列表
// 增加状态分类
func (c *TpOtaDeviceController) List() {
	reqData := valid.TpOtaDevicePaginationValidate{}
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
	var TpOtaDeviceService services.TpOtaDeviceService
	isSuccess, d, t := TpOtaDeviceService.GetTpOtaDeviceList(reqData)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	datamap := make(map[string]interface{})
	datamap["list"] = d
	success, count := TpOtaDeviceService.GetTpOtaDeviceStatusCount(reqData)
	if !success {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	datamap["statuscount"] = count
	dd := valid.RspTpOtaDevicePaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        datamap,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 新增
func (c *TpOtaDeviceController) Add() {
	reqData := valid.AddTpOtaDeviceValidate{}
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
	var TpOtaDeviceService services.TpOtaDeviceService
	id := utils.GetUuid()
	TpOtaDevice := models.TpOtaDevice{
		Id:               id,
		DeviceId:         reqData.DeviceId,
		CurrentVersion:   reqData.CurrentVersion,
		TargetVersion:    reqData.TargetVersion,
		UpgradeProgress:  reqData.UpgradeProgress,
		StatusUpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		UpgradeStatus:    reqData.UpgradeStatus,
		StatusDetail:     reqData.StatusDetail,
	}
	d, rsp_err := TpOtaDeviceService.AddTpOtaDevice(TpOtaDevice)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23505")
		if isTrue {
			err = "批次编号不能重复！"
		} else {
			err = rsp_err.Error()
		}
		utils.SuccessWithMessage(400, err, (*context2.Context)(c.Ctx))
	}
}

//修改状态
func (c *TpOtaDeviceController) ModfiyUpdate() {
	reqData := valid.TpOtaDeviceIdValidate{}
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
	if reqData.Id == "" && reqData.OtaTaskId == "" {
		utils.SuccessWithMessage(1000, "id与任务id不能全部为空", (*context2.Context)(c.Ctx))
		return
	}
	var TpOtaDeviceService services.TpOtaDeviceService
	rsp_err := TpOtaDeviceService.ModfiyUpdateDevice(reqData)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}
}
