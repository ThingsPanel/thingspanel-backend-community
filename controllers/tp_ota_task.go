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

type TpOtaTaskController struct {
	beego.Controller
}

// 列表
func (c *TpOtaTaskController) List() {
	reqData := valid.TpOtaTaskPaginationValidate{}
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
	var TpOtaTaskService services.TpOtaTaskService
	isSuccess, d, t := TpOtaTaskService.GetTpOtaTaskList(reqData)

	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpOtaTaskPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 新增
func (c *TpOtaTaskController) Add() {
	reqData := valid.AddTpOtaTaskValidate{}
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
	var DeviceService services.DeviceService
	var dcount int64
	var devices []models.Device
	// 0: 全部 1: 指定
	if reqData.SelectDeviceFlag == "0" {
		devices, dcount = DeviceService.GetDevicesByProductID(reqData.ProductId)
		if dcount == 0 {
			utils.SuccessWithMessage(400, "无对应设备信息", (*context2.Context)(c.Ctx))
			return
		}
	} else {
		for _, v := range reqData.DeviceIdList {
			device, _ := DeviceService.GetDeviceByID(v)
			devices = append(devices, *device)
			dcount += 1
		}
	}

	var TpOtaTaskService services.TpOtaTaskService
	var TpOtaDeviceService services.TpOtaDeviceService
	id := utils.GetUuid()
	taskstatus := "1"
	upgradestatus := "1"
	statusdetail := ""
	starttime := ""
	endtime := ""
	if reqData.UpgradeTimeType == "1" {
		taskstatus = "0"
		upgradestatus = "0"
		st, _ := time.Parse("2006-01-02T15:04:05Z", reqData.StartTime)
		et, _ := time.Parse("2006-01-02T15:04:05Z", reqData.EndTime)
		starttime = st.Format("2006-01-02 15:04:05")
		endtime = et.Format("2006-01-02 15:04:05")
		statusdetail = fmt.Sprintf("定时：(%s)", starttime)
	}
	TpOtaTask := models.TpOtaTask{
		Id:              id,
		TaskName:        reqData.TaskName,
		UpgradeTimeType: reqData.UpgradeTimeType,
		StartTime:       starttime,
		EndTime:         endtime,
		DeviceCount:     dcount,
		TaskStatus:      taskstatus,
		Description:     reqData.Description,
		CreatedAt:       time.Now().Unix(),
		OtaId:           reqData.OtaId,
	}
	d, rsp_err := TpOtaTaskService.AddTpOtaTask(TpOtaTask)
	if rsp_err != nil {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	var OtaService services.TpOtaService
	isSuccess, tpota := OtaService.GetTpOtaVersionById(reqData.OtaId)
	if !isSuccess {
		utils.SuccessWithMessage(400, "无对应ota信息", (*context2.Context)(c.Ctx))
		return
	}
	//推送次数
	if reqData.RetryCount == 0 {
		reqData.RetryCount = 3
	}
	var tp_ota_devices []models.TpOtaDevice
	for _, device := range devices {
		tp_ota_devices = append(tp_ota_devices, models.TpOtaDevice{
			Id:             utils.GetUuid(),
			DeviceId:       device.ID,
			CurrentVersion: device.CurrentVersion,
			TargetVersion:  tpota.PackageVersion,
			OtaTaskId:      d.Id,
			UpgradeStatus:  upgradestatus,
			StatusDetail:   statusdetail,
			RetryCount:     reqData.RetryCount,
		})
	}
	_, rsp_device_err := TpOtaDeviceService.AddBathTpOtaDevice(tp_ota_devices)
	// 如果升级任务和升级设备都添加成功，且是立即升级，发送升级消息
	if rsp_err == nil && rsp_device_err == nil {
		if reqData.UpgradeTimeType == "0" {
			go TpOtaDeviceService.OtaToUpgradeMsg(devices, reqData.OtaId, id)
		}
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}

}

//删除
func (c *TpOtaTaskController) Delete() {
	reqData := valid.TpOtaTaskIdValidate{}
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
	var TpOtaTaskService services.TpOtaTaskService
	TpOtaTask := models.TpOtaTask{
		Id: reqData.Id,
	}
	rsp_err := TpOtaTaskService.DeleteTpOtaTask(TpOtaTask)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}
}
