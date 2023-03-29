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
func (TpOtaTaskController *TpOtaTaskController) List() {
	PaginationValidate := valid.TpOtaTaskPaginationValidate{}
	err := json.Unmarshal(TpOtaTaskController.Ctx.Input.RequestBody, &PaginationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(PaginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(PaginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpOtaTaskController.Ctx))
			break
		}
		return
	}
	var TpOtaTaskService services.TpOtaTaskService
	isSuccess, d, t := TpOtaTaskService.GetTpOtaTaskList(PaginationValidate)

	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(TpOtaTaskController.Ctx))
		return
	}
	dd := valid.RspTpOtaTaskPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpOtaTaskController.Ctx))

}

// 新增
func (TpOtaTaskController *TpOtaTaskController) Add() {
	AddTpOtaTaskValidate := valid.AddTpOtaTaskValidate{}
	err := json.Unmarshal(TpOtaTaskController.Ctx.Input.RequestBody, &AddTpOtaTaskValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpOtaTaskValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpOtaTaskValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpOtaTaskController.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	var dcount int64
	var devices []models.Device
	// 0: 全部 1: 指定
	if AddTpOtaTaskValidate.SelectDeviceFlag == "0" {
		devices, dcount = DeviceService.GetDevicesByProductID(AddTpOtaTaskValidate.ProductId)
		if dcount == 0 {
			utils.SuccessWithMessage(400, "无对应设备信息", (*context2.Context)(TpOtaTaskController.Ctx))
			return
		}
	} else {
		for _, v := range AddTpOtaTaskValidate.DeviceIdList {
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
	if AddTpOtaTaskValidate.UpgradeTimeType == "1" {
		taskstatus = "0"
		upgradestatus = "0"
		st, _ := time.Parse("2006-01-02T15:04:05Z", AddTpOtaTaskValidate.StartTime)
		et, _ := time.Parse("2006-01-02T15:04:05Z", AddTpOtaTaskValidate.EndTime)
		starttime = st.Format("2006-01-02 15:04:05")
		endtime = et.Format("2006-01-02 15:04:05")
		statusdetail = fmt.Sprintf("定时：(%s)", starttime)
	}
	TpOtaTask := models.TpOtaTask{
		Id:              id,
		TaskName:        AddTpOtaTaskValidate.TaskName,
		UpgradeTimeType: AddTpOtaTaskValidate.UpgradeTimeType,
		StartTime:       starttime,
		EndTime:         endtime,
		DeviceCount:     dcount,
		TaskStatus:      taskstatus,
		Description:     AddTpOtaTaskValidate.Description,
		CreatedAt:       time.Now().Unix(),
		OtaId:           AddTpOtaTaskValidate.OtaId,
	}
	d, rsp_err := TpOtaTaskService.AddTpOtaTask(TpOtaTask)
	if rsp_err != nil {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpOtaTaskController.Ctx))
	}
	var OtaService services.TpOtaService
	isSuccess, tpota := OtaService.GetTpOtaVersionById(AddTpOtaTaskValidate.OtaId)
	if !isSuccess {
		utils.SuccessWithMessage(400, "无对应ota信息", (*context2.Context)(TpOtaTaskController.Ctx))
	}
	var tp_ota_devices []models.TpOtaDevice
	for _, device := range devices {
		tp_ota_devices = append(tp_ota_devices, models.TpOtaDevice{
			Id:            utils.GetUuid(),
			DeviceId:      device.ID,
			TargetVersion: tpota.PackageVersion,
			OtaTaskId:     d.Id,
			UpgradeStatus: upgradestatus,
			StatusDetail:  statusdetail,
		})
	}
	_, rsp_device_err := TpOtaDeviceService.AddBathTpOtaDevice(tp_ota_devices)
	// 如果升级任务和升级设备都添加成功，且是立即升级，发送升级消息
	if rsp_err == nil && rsp_device_err == nil {
		if AddTpOtaTaskValidate.UpgradeTimeType == "0" {
			go TpOtaDeviceService.OtaToUpgradeMsg(devices, AddTpOtaTaskValidate.OtaId, id)
		}
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpOtaTaskController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpOtaTaskController.Ctx))
	}

}

//删除
func (TpOtaTaskController *TpOtaTaskController) Delete() {
	TpOtaTaskIdValidate := valid.TpOtaTaskIdValidate{}
	err := json.Unmarshal(TpOtaTaskController.Ctx.Input.RequestBody, &TpOtaTaskIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpOtaTaskIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpOtaTaskIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpOtaTaskController.Ctx))
			break
		}
		return
	}
	if TpOtaTaskIdValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpOtaTaskController.Ctx))
	}
	var TpOtaTaskService services.TpOtaTaskService
	TpOtaTask := models.TpOtaTask{
		Id: TpOtaTaskIdValidate.Id,
	}
	rsp_err := TpOtaTaskService.DeleteTpOtaTask(TpOtaTask)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpOtaTaskController.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpOtaTaskController.Ctx))
	}
}
