package api

import (
	"net/http"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/common"
	"project/pkg/errcode"
	"project/pkg/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

type DeviceModelApi struct{}

// /api/v1/device/model/telemetry
func (*DeviceModelApi) CreateDeviceModelTelemetry(c *gin.Context) {
	var req model.CreateDeviceModelReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.CreateDeviceModelGeneral(req, model.DEVICE_MODEL_TELEMETRY, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/device/model/attributes
func (*DeviceModelApi) CreateDeviceModelAttributes(c *gin.Context) {
	var req model.CreateDeviceModelReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.CreateDeviceModelGeneral(req, model.DEVICE_MODEL_ATTRIBUTES, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

func (*DeviceModelApi) CreateDeviceModelEvents(c *gin.Context) {
	var req model.CreateDeviceModelV2Req
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.CreateDeviceModelGeneralV2(req, model.DEVICE_MODEL_EVENTS, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

func (*DeviceModelApi) CreateDeviceModelCommands(c *gin.Context) {
	var req model.CreateDeviceModelV2Req
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.CreateDeviceModelGeneralV2(req, model.DEVICE_MODEL_COMMANDS, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
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
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"param_err": "url param is not a valid JSON",
		}))
		return
	}
	err := service.GroupApp.DeviceModel.DeleteDeviceModelGeneral(id, what, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/device/model/telemetry  [put]
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
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"param_err": "url param is not a valid JSON",
		}))
		return
	}

	data, err := service.GroupApp.DeviceModel.UpdateDeviceModelGeneral(req, what, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
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
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"param_err": "url param is not a valid JSON",
		}))
		return
	}

	data, err := service.GroupApp.DeviceModel.UpdateDeviceModelGeneralV2(req, what, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
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
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"param_err": "url param is not a valid JSON",
		}))
		return
	}

	data, err := service.GroupApp.DeviceModel.GetDeviceModelListByPageGeneral(req, what, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/device/model/source/at/list
func (*DeviceModelApi) HandleModelSourceAT(c *gin.Context) {
	var param model.ParamID
	if !BindAndValidate(c, &param) {
		return
	}

	data, err := service.GroupApp.DeviceModel.GetModelSourceAT(c, &param)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/device/model/custom/commands/
func (*DeviceModelApi) CreateDeviceModelCustomCommands(c *gin.Context) {
	var req model.CreateDeviceModelCustomCommandReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.DeviceModel.CreateDeviceModelCustomCommands(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

func (*DeviceModelApi) DeleteDeviceModelCustomCommands(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DeviceModel.DeleteDeviceModelCustomCommands(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

func (*DeviceModelApi) UpdateDeviceModelCustomCommands(c *gin.Context) {
	var req model.UpdateDeviceModelCustomCommandReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DeviceModel.UpdateDeviceModelCustomCommands(req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

func (*DeviceModelApi) HandleDeviceModelCustomCommandsByPage(c *gin.Context) {
	var req model.GetDeviceModelListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	data, err := service.GroupApp.DeviceModel.GetDeviceModelCustomCommandsByPage(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

func (*DeviceModelApi) HandleDeviceModelCustomCommandsByDeviceId(c *gin.Context) {
	deviceId := c.Param("deviceId")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.DeviceModel.GetDeviceModelCustomCommandsByDeviceId(deviceId, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
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
