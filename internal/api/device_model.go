package api

import (
	"fmt"
	"net/http"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/common"
	"project/pkg/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

type DeviceModelApi struct{}

func (*DeviceModelApi) CreateDeviceModelTelemetry(c *gin.Context) {
	var req model.CreateDeviceModelReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.CreateDeviceModelGeneral(req, model.DEVICE_MODEL_TELEMETRY, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create Device Model Telemetry successfully", data)
}

func (*DeviceModelApi) CreateDeviceModelAttributes(c *gin.Context) {
	var req model.CreateDeviceModelReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.CreateDeviceModelGeneral(req, model.DEVICE_MODEL_ATTRIBUTES, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create Device Model Attributes successfully", data)
}

func (*DeviceModelApi) CreateDeviceModelEvents(c *gin.Context) {
	var req model.CreateDeviceModelV2Req
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.CreateDeviceModelGeneralV2(req, model.DEVICE_MODEL_EVENTS, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create Device Model Events successfully", data)
}

func (*DeviceModelApi) CreateDeviceModelCommands(c *gin.Context) {
	var req model.CreateDeviceModelV2Req
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.CreateDeviceModelGeneralV2(req, model.DEVICE_MODEL_COMMANDS, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create Device Model Commands successfully", data)
}

// 物模型删除-通用
func (*DeviceModelApi) DeleteDeviceModelGeneral(c *gin.Context) {
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	var what string

	// 通过URI判断来自哪个接口
	uri := c.Request.RequestURI
	if strings.Contains(uri, "telemetry") {
		what = model.DEVICE_MODEL_TELEMETRY
	} else if strings.Contains(uri, "attributes") {
		what = model.DEVICE_MODEL_ATTRIBUTES
	} else if strings.Contains(uri, "events") {
		what = model.DEVICE_MODEL_EVENTS
	} else if strings.Contains(uri, "commands") {
		what = model.DEVICE_MODEL_COMMANDS
	} else {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("error"))
		return
	}
	err := service.GroupApp.DeviceModel.DeleteDeviceModelGeneral(id, what, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete Device Model successfully", nil)
}

func (*DeviceModelApi) UpdateDeviceModelGeneral(c *gin.Context) {
	var req model.UpdateDeviceModelReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	var what string

	// 通过URI判断来自哪个接口
	uri := c.Request.RequestURI
	if strings.Contains(uri, "telemetry") {
		what = model.DEVICE_MODEL_TELEMETRY
	} else if strings.Contains(uri, "attributes") {
		what = model.DEVICE_MODEL_ATTRIBUTES
	} else {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("error"))
		return
	}

	data, err := service.GroupApp.DeviceModel.UpdateDeviceModelGeneral(req, what, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update Device Model Telemetry successfully", data)
}

func (*DeviceModelApi) UpdateDeviceModelGeneralV2(c *gin.Context) {
	var req model.UpdateDeviceModelV2Req
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	var what string

	// 通过URI判断来自哪个接口
	uri := c.Request.RequestURI

	if strings.Contains(uri, "events") {
		what = model.DEVICE_MODEL_EVENTS
	} else if strings.Contains(uri, "commands") {
		what = model.DEVICE_MODEL_COMMANDS
	} else {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("error"))
		return
	}

	data, err := service.GroupApp.DeviceModel.UpdateDeviceModelGeneralV2(req, what, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update Device Model Telemetry successfully", data)
}

func (*DeviceModelApi) HandleDeviceModelGeneral(c *gin.Context) {
	var req model.GetDeviceModelListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	var what string

	// 通过URI判断来自哪个接口
	uri := c.Request.RequestURI
	if strings.Contains(uri, "telemetry") {
		what = model.DEVICE_MODEL_TELEMETRY
	} else if strings.Contains(uri, "attributes") {
		what = model.DEVICE_MODEL_ATTRIBUTES
	} else if strings.Contains(uri, "events") {
		what = model.DEVICE_MODEL_EVENTS
	} else if strings.Contains(uri, "commands") {
		what = model.DEVICE_MODEL_COMMANDS
	} else {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("error"))
		return
	}

	data, err := service.GroupApp.DeviceModel.GetDeviceModelListByPageGeneral(req, what, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get Device Model Telemetry By Page successfully", data)
}

func (*DeviceModelApi) HandleModelSourceAT(c *gin.Context) {
	var param model.ParamID
	if !BindAndValidate(c, &param) {
		return
	}

	data, err := service.GroupApp.DeviceModel.GetModelSourceAT(c, &param)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, data)
}

func (*DeviceModelApi) CreateDeviceModelCustomCommands(c *gin.Context) {
	var req model.CreateDeviceModelCustomCommandReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.DeviceModel.CreateDeviceModelCustomCommands(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, "")
}

func (*DeviceModelApi) DeleteDeviceModelCustomCommands(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DeviceModel.DeleteDeviceModelCustomCommands(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, "")
}

func (*DeviceModelApi) UpdateDeviceModelCustomCommands(c *gin.Context) {
	var req model.UpdateDeviceModelCustomCommandReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DeviceModel.UpdateDeviceModelCustomCommands(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, "")
}

func (*DeviceModelApi) HandleDeviceModelCustomCommandsByPage(c *gin.Context) {
	var req model.GetDeviceModelListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.DeviceModel.GetDeviceModelCustomCommandsByPage(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, data)
}

func (*DeviceModelApi) HandleDeviceModelCustomCommandsByDeviceId(c *gin.Context) {
	deviceId := c.Param("deviceId")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.GetDeviceModelCustomCommandsByDeviceId(deviceId, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, common.SUCCESS, data)
}

func (*DeviceModelApi) CreateDeviceModelCustomControl(c *gin.Context) {
	var req model.CreateDeviceModelCustomControlReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.DeviceModel.CreateDeviceModelCustomControl(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, common.SUCCESS, "")
}

func (*DeviceModelApi) DeleteDeviceModelCustomControl(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DeviceModel.DeleteDeviceModelCustomControl(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, common.SUCCESS, "")
}

func (*DeviceModelApi) UpdateDeviceModelCustomControl(c *gin.Context) {
	var req model.UpdateDeviceModelCustomControlReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DeviceModel.UpdateDeviceModelCustomControl(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, common.SUCCESS, "")
}

// /api/v1/device/model/custom/control GET
func (*DeviceModelApi) HandleDeviceModelCustomControl(c *gin.Context) {
	var req model.GetDeviceModelListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.GetDeviceModelCustomControlByPage(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, common.SUCCESS, data)
}
