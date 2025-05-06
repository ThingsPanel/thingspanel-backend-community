package service

import (
	"context"
	"time"

	"project/initialize"
	"project/internal/query"
	protocolplugin "project/internal/service/protocol_plugin"
	"project/pkg/common"
	"project/pkg/constant"
	"project/pkg/errcode"

	dal "project/internal/dal"
	model "project/internal/model"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type DeviceConfig struct{}

func (*DeviceConfig) CreateDeviceConfig(req *model.CreateDeviceConfigReq, claims *utils.UserClaims) (deviceconfig model.DeviceConfig, err error) {
	deviceconfig.ID = uuid.New()
	deviceconfig.Name = req.Name
	deviceconfig.Description = req.Description
	deviceconfig.DeviceConnType = req.DeviceConnType
	// 特殊处理
	if req.DeviceTemplateId != nil && *req.DeviceTemplateId == "" {
		req.DeviceTemplateId = nil
	}
	deviceconfig.DeviceTemplateID = req.DeviceTemplateId
	deviceconfig.DeviceType = req.DeviceType
	if req.AdditionalInfo != nil && !IsJSON(*req.AdditionalInfo) {
		return deviceconfig, errcode.NewWithMessage(errcode.CodeParamError, "additional_info is not a valid JSON")
	}
	deviceconfig.AdditionalInfo = req.AdditionalInfo
	if req.ProtocolConfig != nil && !IsJSON(*req.ProtocolConfig) {
		return deviceconfig, errcode.NewWithMessage(errcode.CodeParamError, "protocol_config is not a valid JSON")
	}
	deviceconfig.ProtocolConfig = req.ProtocolConfig
	// 如果协议类型没有传，则默认为MQTT
	if req.ProtocolType == nil {
		deviceconfig.ProtocolType = StringPtr("MQTT")
	} else {
		deviceconfig.ProtocolType = req.ProtocolType
	}
	if req.VoucherType == nil {
		deviceconfig.VoucherType = StringPtr("ACCESSTOKEN")
	} else {
		deviceconfig.VoucherType = req.VoucherType
	}
	deviceconfig.Remark = req.Remark
	t := time.Now().UTC()
	deviceconfig.CreatedAt = t
	deviceconfig.UpdatedAt = t
	deviceconfig.TenantID = claims.TenantID
	deviceconfig.TemplateSecret = StringPtr(uuid.New())

	err = dal.CreateDeviceConfig(&deviceconfig)
	if err != nil {
		logrus.Error(err)
		return deviceconfig, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	return deviceconfig, err
}

func (*DeviceConfig) UpdateDeviceConfig(req model.UpdateDeviceConfigReq) (any, error) {
	// 对修改设备模板id进行特殊处理
	if req.DeviceTemplateId != nil && *req.DeviceTemplateId == "" {
		err := dal.UpdateDeviceConfigTemplateID(req.Id, nil)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		req.DeviceTemplateId = nil
	}
	condsMap, err := StructToMapAndVerifyJson(req, "additional_info", "protocol_config", "other_config")
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}
	// 获取原配置信息
	oldConfig, err := dal.GetDeviceConfigByID(req.Id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	logrus.Debug("condsMap:", condsMap)
	err = dal.UpdateDeviceConfig(req.Id, condsMap)
	if err != nil {
		logrus.Error(err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 清除设备配置信息缓存
	initialize.DelDeviceConfigCache(req.Id)
	// 获取设备配置信息
	data, err := dal.GetDeviceConfigByID(req.Id)
	if err != nil {
		return data, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if data.ProtocolType != nil && *data.ProtocolType != "MQTT" {
		// 判断协议配置是否有变化
		if oldConfig.ProtocolConfig != nil && data.ProtocolConfig != nil && *oldConfig.ProtocolConfig != *data.ProtocolConfig {
			// 协议配置有变化，断开设备连接
			err = protocolplugin.DeviceConfigUpdateAndDisconnect(req.Id, *data.ProtocolType, data.DeviceType)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
				})
			}
		}
	}
	// 判断协议类型是否变化
	if oldConfig.ProtocolType != nil && data.ProtocolType != nil && *oldConfig.ProtocolType != *data.ProtocolType {
		// 协议类型有变化，要删除凭证类型，但如果新的协议类型是MQTT，要改为ACCESSTOKEN
		if *data.ProtocolType == "MQTT" {
			err = dal.UpdateDeviceConfigVoucherType(req.Id, StringPtr("ACCESSTOKEN"))
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
				})
			}
		} else {
			err = dal.UpdateDeviceConfigVoucherType(req.Id, nil)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
				})
			}
		}
	}
	return data, nil
}

func (*DeviceConfig) DeleteDeviceConfig(id string) error {
	// 检查是否存在关联的 device 记录
	devices, err := dal.GetDevicesByDeviceConfigID(id)
	if err != nil {
		return err
	}
	if len(devices) > 0 {
		return errcode.WithVars(200051, map[string]interface{}{
			"count": len(devices),
		})
	}

	// 删除 device config
	err = dal.DeleteDeviceConfig(id)
	if err != nil {
		logrus.Error(err)
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 清除设备配置信息缓存
	initialize.DelDeviceConfigCache(id)
	initialize.DelDeviceDataScriptCache(id)

	return nil
}

func (*DeviceConfig) GetDeviceConfigByID(ctx context.Context, id string) (any, error) {
	db := dal.DeviceConfigQuery{}
	info, err := db.First(ctx, query.DeviceConfig.ID.Eq(id))
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// res := dal.DeviceConfigVo{}.PoToVo(info)
	return info, nil
}

func (*DeviceConfig) GetDeviceConfigListByPage(req *model.GetDeviceConfigListByPageReq, claims *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetDeviceConfigListByPage(req, claims)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	deviceconfigListRsp := make(map[string]interface{})
	deviceconfigListRsp["total"] = total
	if total == int64(0) {
		list = make([]*map[string]interface{}, 0)
	}
	deviceconfigListRsp["list"] = list

	return deviceconfigListRsp, err
}

func (*DeviceConfig) GetDeviceConfigListMenu(req *model.GetDeviceConfigListMenuReq, claims *utils.UserClaims) (any, error) {
	data, err := dal.GetDeviceConfigSelectList(req.DeviceConfigName, claims.TenantID, req.DeviceType, req.ProtocolType)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, nil
}

func (*DeviceConfig) BatchUpdateDeviceConfig(req *model.BatchUpdateDeviceConfigReq) error {
	err := dal.BatchUpdateDeviceConfig(req)
	if err != nil {
		logrus.Error(err)
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 清除设备信息缓存
	for _, id := range req.DeviceIds {
		initialize.DelDeviceCache(id)
		// initialize.DelDeviceDataScriptCache(id)
	}
	return err
}

func (*DeviceConfig) GetDeviceConfigConnect(ctx context.Context, deviceID string) (res *model.DeviceConfigConnectRes, err error) {
	var (
		db     = dal.DeviceQuery{}
		device = query.Device

		db1          = dal.DeviceConfigQuery{}
		deviceConfig = query.DeviceConfig
	)
	deviceInfo, err := db.First(ctx, device.ID.Eq(deviceID))
	if err != nil {
		logrus.Error(ctx, "[DeviceConfig][GetDeviceConfigConnect] device failed:", err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if deviceInfo.DeviceConfigID == nil || common.CheckEmpty(*deviceInfo.DeviceConfigID) {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"msg": "return not found",
		})
	}

	_, err = db1.First(ctx, deviceConfig.ID.Eq(*deviceInfo.DeviceConfigID), deviceConfig.ProtocolType.Eq("MQTT"))
	if err != nil {
		logrus.Error(ctx, "[DeviceConfig][GetDeviceConfigConnect]deviceConfig failed:", err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	res = &model.DeviceConfigConnectRes{
		AccessToken: "ACCESSTOKEN",
		Basic:       "BASIC",
	}
	return
}

// 获取凭证类型表单
func (*DeviceConfig) GetVoucherTypeForm(deviceType string, protocolType string) (data interface{}, err error) {
	// 判断协议类型是否来自协议插件
	if protocolType == "MQTT" {
		data = map[string]interface{}{
			"AccessToken": "ACCESSTOKEN",
			"Basic":       "BASIC",
		}
		return
	}
	var pd ServicePlugin
	data, err = pd.GetPluginForm(protocolType, deviceType, string(constant.VOUCHER_TYPE_FORM))
	if err != nil {
		logrus.Error(err)
		return data, err
	}
	return
}

// 获取自动化一类设备Action下拉菜单；
// 包含遥测、属性、命令
func (*DeviceConfig) GetActionByDeviceConfigID(deviceConfigID string) (any, error) {
	/*返回数据结构
	{
		"data_source_type": "telemetry",
		"options": [
			{
				"key": "temp",
				"label": "温度",
				"data_type": "number",
				"unit": "℃"
			}
		]
	},
	{
		"data_source_type": "attribute",
		"options": [
			{
				"key": "version",
				"label": "固件版本",
				"data_type": "string"
			}
		]
	},
	*/
	// 获取设备配置信息
	deviceConfig, err := dal.GetDeviceConfigByID(deviceConfigID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if deviceConfig.DeviceTemplateID == nil {
		return nil, nil
	}
	// 获取设备模板遥测
	telemetryDatas, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	type options struct {
		Key           string  `json:"key"`
		Label         *string `json:"label"`
		DataType      *string `json:"data_type"`
		Uint          *string `json:"unit"`
		ReadWriteFlag *string `json:"read_write_flag"`
	}
	type actionModelSource struct {
		DataSourceTypeRes string     `json:"data_source_type"`
		Options           []*options `json:"options"`
	}

	telemetryOptions := make([]*options, 0)
	for _, telemetry := range telemetryDatas {
		var o options
		o.Key = telemetry.DataIdentifier
		o.Label = telemetry.DataName
		o.DataType = telemetry.DataType
		o.Uint = telemetry.Unit
		o.ReadWriteFlag = telemetry.ReadWriteFlag
		telemetryOptions = append(telemetryOptions, &o)
	}
	// 获取设备模板属性
	attributeDatas, err := dal.GetDeviceModelAttributeDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	attributeOptions := make([]*options, 0)
	for _, attribute := range attributeDatas {
		var o options
		o.Key = attribute.DataIdentifier
		o.Label = attribute.DataName
		o.DataType = attribute.DataType
		o.Uint = attribute.Unit
		o.ReadWriteFlag = attribute.ReadWriteFlag
		attributeOptions = append(attributeOptions, &o)
	}
	// 获取设备模板命令
	commandDatas, err := dal.GetDeviceModelCommandDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	commandOptions := make([]*options, 0)
	for _, command := range commandDatas {
		var o options
		o.Key = command.DataIdentifier
		o.Label = command.DataName
		o.DataType = StringPtr("string")
		commandOptions = append(commandOptions, &o)
	}
	// 返回
	res := make([]actionModelSource, 0)
	if len(telemetryOptions) != 0 {
		res = append(res, actionModelSource{
			DataSourceTypeRes: string(constant.TelemetrySource),
			Options:           telemetryOptions,
		})
	}
	if len(attributeOptions) != 0 {
		res = append(res, actionModelSource{
			DataSourceTypeRes: string(constant.AttributeSource),
			Options:           attributeOptions,
		})
	}
	if len(commandOptions) != 0 {
		res = append(res, actionModelSource{
			DataSourceTypeRes: string(constant.CommandSource),
			Options:           commandOptions,
		})
	}
	return res, nil
}

// 获取自动化一类设备Condition下拉菜单；
// 包含遥测、属性、事件
func (*DeviceConfig) GetConditionByDeviceConfigID(deviceConfigID string) (any, error) {
	/*返回数据结构
	{
		"data_source_type": "telemetry",
		"options": [
			{
				"key": "temp",
				"label": "温度",
				"data_type": "number",
				"unit": "℃"
			}
		]
	},
	{
		"data_source_type": "attribute",
		"options": [
			{
				"key": "version",
				"label": "固件版本",
				"data_type": "string"
			}
		]
	},
	*/
	// 获取设备配置信息
	deviceConfig, err := dal.GetDeviceConfigByID(deviceConfigID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if deviceConfig.DeviceTemplateID == nil {
		return nil, nil
	}
	// 获取设备模板遥测
	telemetryDatas, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	type options struct {
		Key      string  `json:"key"`
		Label    *string `json:"label"`
		DataType *string `json:"data_type"`
		Uint     *string `json:"unit"`
	}
	type actionModelSource struct {
		DataSourceTypeRes string     `json:"data_source_type"`
		Options           []*options `json:"options"`
	}

	telemetryOptions := make([]*options, 0)
	for _, telemetry := range telemetryDatas {
		var o options
		o.Key = telemetry.DataIdentifier
		o.Label = telemetry.DataName
		o.DataType = telemetry.DataType
		o.Uint = telemetry.Unit
		telemetryOptions = append(telemetryOptions, &o)
	}
	// 获取设备模板属性
	attributeDatas, err := dal.GetDeviceModelAttributeDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	attributeOptions := make([]*options, 0)
	for _, attribute := range attributeDatas {
		var o options
		o.Key = attribute.DataIdentifier
		o.Label = attribute.DataName
		o.DataType = attribute.DataType
		o.Uint = attribute.Unit
		attributeOptions = append(attributeOptions, &o)
	}
	// 获取设备模板命令
	// eventDatas, err := dal.GetDeviceModelCommandDataList(*deviceConfig.DeviceTemplateID)
	eventDatas, err := dal.GetDeviceModelEventDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, err
	}
	eventOptions := make([]*options, 0)
	for _, event := range eventDatas {
		var o options
		o.Key = event.DataIdentifier
		o.Label = event.DataName
		o.DataType = StringPtr("string")
		eventOptions = append(eventOptions, &o)
	}
	// 返回
	res := make([]actionModelSource, 0)
	if len(telemetryOptions) != 0 {
		res = append(res, actionModelSource{
			DataSourceTypeRes: string(constant.TelemetrySource),
			Options:           telemetryOptions,
		})
	}
	if len(attributeOptions) != 0 {
		res = append(res, actionModelSource{
			DataSourceTypeRes: string(constant.AttributeSource),
			Options:           attributeOptions,
		})
	}
	if len(eventOptions) != 0 {
		res = append(res, actionModelSource{
			DataSourceTypeRes: string(constant.EventSource),
			Options:           eventOptions,
		})
	}
	return res, nil
}
