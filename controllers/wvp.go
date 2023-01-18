package controllers

import (
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	"encoding/json"
	"fmt"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type WvpController struct {
	beego.Controller
}

// ptz控制接口
func (widgetController *WvpController) PtzControl() {
	var ptzControlValid = make(map[string]string)
	err := json.Unmarshal(widgetController.Ctx.Input.RequestBody, &ptzControlValid)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var parentId, subDeviceAddr string
	if ptzControlValid["parent_id"] == "" {
		response.SuccessWithMessage(400, "parent_id不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		parentId = ptzControlValid["parent_id"]
		delete(ptzControlValid, "parent_id")
	}
	if ptzControlValid["sub_device_addr"] == "" {
		response.SuccessWithMessage(400, "sub_device_addr不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		subDeviceAddr = ptzControlValid["sub_device_addr"]
		delete(ptzControlValid, "sub_device_addr")
	}
	var wvpDeviceService services.WvpDeviceService
	err = wvpDeviceService.PtzControl(parentId, subDeviceAddr, ptzControlValid)
	if err == nil {
		// 修改成功
		response.SuccessWithMessage(200, "success", (*context2.Context)(widgetController.Ctx))
		return
	}
	// 修改失败
	response.SuccessWithMessage(400, err.Error(), (*context2.Context)(widgetController.Ctx))
}

// 获取通道的视频列表
func (widgetController *WvpController) GetVideoList() {
	var ptzControlValid = make(map[string]string)
	err := json.Unmarshal(widgetController.Ctx.Input.RequestBody, &ptzControlValid)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var parentId, subDeviceAddr string
	if ptzControlValid["parent_id"] == "" {
		response.SuccessWithMessage(400, "parent_id不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		parentId = ptzControlValid["parent_id"]
		delete(ptzControlValid, "parent_id")
	}
	if ptzControlValid["sub_device_addr"] == "" {
		response.SuccessWithMessage(400, "sub_device_addr不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		subDeviceAddr = ptzControlValid["sub_device_addr"]
		delete(ptzControlValid, "sub_device_addr")
	}
	var wvpDeviceService services.WvpDeviceService
	rspJson, err := wvpDeviceService.GetVideoList(parentId, subDeviceAddr, ptzControlValid)
	if err == nil {
		response.SuccessWithDetailed(200, "success", rspJson, map[string]string{}, (*context2.Context)(widgetController.Ctx))
		return
	}
	response.SuccessWithMessage(400, err.Error(), (*context2.Context)(widgetController.Ctx))
}

// 获取通道的视频列表
func (widgetController *WvpController) GetPlaybackAddr() {
	var ptzControlValid = make(map[string]string)
	err := json.Unmarshal(widgetController.Ctx.Input.RequestBody, &ptzControlValid)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var parentId, subDeviceAddr string
	if ptzControlValid["parent_id"] == "" {
		response.SuccessWithMessage(400, "parent_id不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		parentId = ptzControlValid["parent_id"]
		delete(ptzControlValid, "parent_id")
	}
	if ptzControlValid["sub_device_addr"] == "" {
		response.SuccessWithMessage(400, "sub_device_addr不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		subDeviceAddr = ptzControlValid["sub_device_addr"]
		delete(ptzControlValid, "sub_device_addr")
	}
	var wvpDeviceService services.WvpDeviceService
	rspJson, err := wvpDeviceService.GetPlaybackAddr(parentId, subDeviceAddr, ptzControlValid)
	if err == nil {
		response.SuccessWithDetailed(200, "success", rspJson, map[string]string{}, (*context2.Context)(widgetController.Ctx))
		return
	}
	response.SuccessWithMessage(400, err.Error(), (*context2.Context)(widgetController.Ctx))
}

//停止播放录像
func (widgetController *WvpController) GetStopPlayback() {
	var ptzControlValid = make(map[string]string)
	err := json.Unmarshal(widgetController.Ctx.Input.RequestBody, &ptzControlValid)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var parentId, subDeviceAddr string
	if ptzControlValid["parent_id"] == "" {
		response.SuccessWithMessage(400, "parent_id不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		parentId = ptzControlValid["parent_id"]
		delete(ptzControlValid, "parent_id")
	}
	if ptzControlValid["sub_device_addr"] == "" {
		response.SuccessWithMessage(400, "sub_device_addr不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		subDeviceAddr = ptzControlValid["sub_device_addr"]
		delete(ptzControlValid, "sub_device_addr")
	}
	var wvpDeviceService services.WvpDeviceService
	rspJson, err := wvpDeviceService.GetStopPlayback(parentId, subDeviceAddr)
	if err == nil {
		response.SuccessWithDetailed(200, "success", rspJson, map[string]string{}, (*context2.Context)(widgetController.Ctx))
		return
	}
	response.SuccessWithMessage(400, err.Error(), (*context2.Context)(widgetController.Ctx))
}

// 获取wvp设备列表
func (widgetController *WvpController) GetDeviceList() {
	var ptzControlValid = make(map[string]string)
	err := json.Unmarshal(widgetController.Ctx.Input.RequestBody, &ptzControlValid)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var parentId string
	if ptzControlValid["id"] == "" {
		response.SuccessWithMessage(400, "parent_id不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		parentId = ptzControlValid["id"]
		delete(ptzControlValid, "id")
	}
	var wvpDeviceService services.WvpDeviceService
	rspJson, err := wvpDeviceService.GetDeviceList(parentId, ptzControlValid)
	if err == nil {
		response.SuccessWithDetailed(200, "success", rspJson, map[string]string{}, (*context2.Context)(widgetController.Ctx))
		return
	}
	response.SuccessWithMessage(400, err.Error(), (*context2.Context)(widgetController.Ctx))
}

// 开始播放
func (widgetController *WvpController) GetPlayAddr() {
	var ptzControlValid = make(map[string]string)
	err := json.Unmarshal(widgetController.Ctx.Input.RequestBody, &ptzControlValid)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var parentId, subDeviceAddr string
	if ptzControlValid["parent_id"] == "" {
		response.SuccessWithMessage(400, "parent_id不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		parentId = ptzControlValid["parent_id"]
		delete(ptzControlValid, "parent_id")
	}
	if ptzControlValid["sub_device_addr"] == "" {
		response.SuccessWithMessage(400, "sub_device_addr不能为空", (*context2.Context)(widgetController.Ctx))
		return
	} else {
		subDeviceAddr = ptzControlValid["sub_device_addr"]
		delete(ptzControlValid, "sub_device_addr")
	}
	var wvpDeviceService services.WvpDeviceService
	rspJson, err := wvpDeviceService.GetPlayAddr(parentId, subDeviceAddr, ptzControlValid)
	if err == nil {
		response.SuccessWithDetailed(200, "success", rspJson, map[string]string{}, (*context2.Context)(widgetController.Ctx))
		return
	}
	response.SuccessWithMessage(400, err.Error(), (*context2.Context)(widgetController.Ctx))
}
