package api

import (
	"net/http"
	"project/internal/adapter"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

type DeviceModelApi struct{}

// deprecated410 writes a 410 Gone response directing clients to the new API.
func deprecated410(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{
		"code":    "API_DEPRECATED",
		"message": "This v1 device-model write API is deprecated. Use the new ThingModel API.",
		"see":     "/api/thing-models",
	})
}

// /api/v1/device/model/telemetry
func (*DeviceModelApi) CreateDeviceModelTelemetry(c *gin.Context) {
	deprecated410(c)
}

// /api/v1/device/model/attributes [post]
func (*DeviceModelApi) CreateDeviceModelAttributes(c *gin.Context) {
	deprecated410(c)
}

func (*DeviceModelApi) CreateDeviceModelEvents(c *gin.Context) {
	deprecated410(c)
}

func (*DeviceModelApi) CreateDeviceModelCommands(c *gin.Context) {
	deprecated410(c)
}

// 物模型删除-通用
func (*DeviceModelApi) DeleteDeviceModelGeneral(c *gin.Context) {
	deprecated410(c)
}

// /api/v1/device/model/telemetry  [put]
func (*DeviceModelApi) UpdateDeviceModelGeneral(c *gin.Context) {
	deprecated410(c)
}

func (*DeviceModelApi) UpdateDeviceModelGeneralV2(c *gin.Context) {
	deprecated410(c)
}

func (*DeviceModelApi) HandleDeviceModelGeneral(c *gin.Context) {
	var req model.GetDeviceModelListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	// 通过URI判断来自哪个接口
	uri := c.Request.RequestURI
	var what string
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

	// T24 grayscale router: use new device-metadata service when tenant is enabled.
	if adapter.DefaultRouter.ShouldUseThingModel(c.Request.Context(), userClaims.TenantID) {
		items, err := adapter.DeviceMetadata().GetItemsByTemplate(c.Request.Context(), userClaims.TenantID, req.DeviceTemplateId)
		if err != nil {
			c.Error(err)
			return
		}
		var data interface{}
		switch what {
		case model.DEVICE_MODEL_TELEMETRY:
			list := adapter.TranslateToV1Telemetry(items)
			data = map[string]interface{}{"total": len(list), "list": list}
		case model.DEVICE_MODEL_ATTRIBUTES:
			list := adapter.TranslateToV1Attribute(items)
			data = map[string]interface{}{"total": len(list), "list": list}
		case model.DEVICE_MODEL_EVENTS:
			list := adapter.TranslateToV1Event(items)
			data = map[string]interface{}{"total": len(list), "list": list}
		case model.DEVICE_MODEL_COMMANDS:
			list := adapter.TranslateToV1Command(items)
			data = map[string]interface{}{"total": len(list), "list": list}
		}
		c.Set("data", data)
		return
	}

	// Legacy path
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
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

func (*DeviceModelApi) DeleteDeviceModelCustomControl(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DeviceModel.DeleteDeviceModelCustomControl(id)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

func (*DeviceModelApi) UpdateDeviceModelCustomControl(c *gin.Context) {
	var req model.UpdateDeviceModelCustomControlReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DeviceModel.UpdateDeviceModelCustomControl(req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
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
		c.Error(err)
		return
	}
	c.Set("data", data)
}
