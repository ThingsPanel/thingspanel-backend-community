package service

import (
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
)

type DeviceTemplate struct{}

func (*DeviceTemplate) CreateDeviceTemplate(req model.CreateDeviceTemplateReq, claims *utils.UserClaims) (*model.DeviceTemplate, error) {

	var deviceTemplate = model.DeviceTemplate{}

	deviceTemplate.ID = uuid.New()
	deviceTemplate.Name = req.Name
	deviceTemplate.Author = req.Author
	deviceTemplate.Version = req.Version
	deviceTemplate.Description = req.Description
	deviceTemplate.TenantID = claims.TenantID

	deviceTemplate.Path = req.Path
	deviceTemplate.Label = req.Label

	t := time.Now().UTC()

	deviceTemplate.CreatedAt = t
	deviceTemplate.UpdatedAt = t

	data, err := dal.CreateDeviceTemplate(&deviceTemplate)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, err
}

func (*DeviceTemplate) UpdateDeviceTemplate(req model.UpdateDeviceTemplateReq, claims *utils.UserClaims) (*model.DeviceTemplate, error) {
	// 根据ID 获取模版
	t, err := dal.GetDeviceTemplateById(req.Id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 权限校验
	if *t.Flag == dal.DEVICE_TEMPLATE_PUBLIC && claims.Authority == dal.TENANT_USER {
		return nil, errcode.New(errcode.CodeOpDenied)
	}
	t.ID = req.Id
	if req.Name != nil {
		t.Name = *req.Name
	}
	if req.Author != nil {
		t.Author = req.Author
	}
	if req.Version != nil {
		t.Version = req.Version
	}
	if req.Description != nil {
		t.Description = req.Description
	}
	if req.Path != nil {
		t.Path = req.Path
	}
	if req.Label != nil {
		t.Label = req.Label
	}
	if req.Remark != nil {
		t.Remark = req.Remark
	}
	if req.WebChartConfig != nil {
		if !IsJSON(*req.WebChartConfig) {
			return nil, errcode.NewWithMessage(errcode.CodeParamError, "web_chart_config is not a valid JSON")
		}
		t.WebChartConfig = req.WebChartConfig
	}

	if req.AppChartConfig != nil {
		if !IsJSON(*req.AppChartConfig) {
			return nil, errcode.NewWithMessage(errcode.CodeParamError, "app_chart_config is not a valid JSON")
		}
		t.AppChartConfig = req.AppChartConfig
	}
	t.UpdatedAt = time.Now().UTC()
	data, err := dal.UpdateDeviceTemplate(t)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, err
}

func (*DeviceTemplate) GetDeviceTemplate(id string) (*model.DeviceTemplate, error) {
	// 根据ID 获取模版
	t, err := dal.GetDeviceTemplateById(id)
	if err != nil {
		return t, err
	}

	return t, nil
}

func (*DeviceTemplate) GetDeviceTemplateById(id string) (*model.DeviceTemplate, error) {
	// 根据ID 获取模版
	t, err := dal.GetDeviceTemplateById(id)
	if err != nil {
		return t, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return t, nil
}

// GetDeviceTemplateByDeviceId 根据设备ID获取模板
func (*DeviceTemplate) GetDeviceTemplateByDeviceId(deviceId string) (any, error) {
	// 根据ID 获取模版
	t, err := dal.GetDeviceTemplateByDeviceId(deviceId)
	if err != nil {
		return t, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return t, nil
}

func (*DeviceTemplate) DeleteDeviceTemplate(id string, claims *utils.UserClaims) error {
	// 根据ID 获取模版
	t, err := dal.GetDeviceTemplateById(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 权限校验
	if *t.Flag == dal.DEVICE_TEMPLATE_PUBLIC && claims.Authority == dal.TENANT_USER {
		return errcode.New(errcode.CodeOpDenied)
	}
	// 根据功能模板ID查询想关联的配置模板数量
	count, err := dal.GetDeviceConfigCountByFuncTemplateId(t.ID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
			"msg":       "get device config count by func template id error",
		})
	}
	if count > 0 {
		return errcode.WithVars(200050, map[string]interface{}{
			"count": count,
		})
	}

	err = dal.DeleteDeviceTemplate(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return err
}

func (*DeviceTemplate) GetDeviceTemplateListByPage(req model.GetDeviceTemplateListByPageReq, claims *utils.UserClaims) (interface{}, error) {

	total, list, err := dal.GetDeviceTemplateListByPage(&req, claims)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	deviceTemplateMap := make(map[string]interface{})
	deviceTemplateMap["total"] = total
	deviceTemplateMap["list"] = list

	return deviceTemplateMap, nil
}

// 获取模板下拉菜单
func (*DeviceTemplate) GetDeviceTemplateMenu(req model.GetDeviceTemplateMenuReq, claims *utils.UserClaims) (interface{}, error) {

	data, err := dal.GetDeviceTemplateMenu(&req, claims)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, nil
}
