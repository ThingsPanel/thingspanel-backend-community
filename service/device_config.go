package service

import (
	"context"
	"errors"
	"fmt"
	"project/common"
	"project/constant"
	"project/initialize"
	"project/query"
	protocolplugin "project/service/protocol_plugin"
	"time"

	dal "project/dal"
	model "project/internal/model"
	utils "project/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type DeviceConfig struct{}

func (p *DeviceConfig) CreateDeviceConfig(req *model.CreateDeviceConfigReq, claims *utils.UserClaims) (deviceconfig model.DeviceConfig, err error) {

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
		return deviceconfig, fmt.Errorf("additional_info is not a valid JSON")
	}
	deviceconfig.AdditionalInfo = req.AdditionalInfo
	if req.ProtocolConfig != nil && !IsJSON(*req.ProtocolConfig) {
		return deviceconfig, fmt.Errorf("protocol_config is not a valid JSON")
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

	err = dal.CreateDeviceConfig(&deviceconfig)

	if err != nil {
		logrus.Error(err)
	}

	return deviceconfig, err
}

func (p *DeviceConfig) UpdateDeviceConfig(req model.UpdateDeviceConfigReq) (any, error) {
	// 对修改设备模板id进行特殊处理
	if req.DeviceTemplateId != nil && *req.DeviceTemplateId == "" {
		err := dal.UpdateDeviceConfigTemplateID(req.Id, nil)
		if err != nil {
			return nil, err
		}
		req.DeviceTemplateId = nil
	}
	condsMap, err := StructToMapAndVerifyJson(req, "additional_info", "protocol_config", "other_config")
	if err != nil {
		return nil, err
	}
	// 获取原配置信息
	oldConfig, err := dal.GetDeviceConfigByID(req.Id)
	if err != nil {
		return nil, err
	}

	logrus.Debug("condsMap:", condsMap)
	err = dal.UpdateDeviceConfig(req.Id, condsMap)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	// 清除设备配置信息缓存
	initialize.DelDeviceConfigCache(req.Id)
	// 获取设备配置信息
	data, err := dal.GetDeviceConfigByID(req.Id)
	if err != nil {
		return data, err
	}
	if data.ProtocolType != nil && *data.ProtocolType != "MQTT" {
		// 判断协议配置是否有变化
		if oldConfig.ProtocolConfig != nil && data.ProtocolConfig != nil && *oldConfig.ProtocolConfig != *data.ProtocolConfig {
			// 协议配置有变化，断开设备连接
			err = protocolplugin.DeviceConfigUpdateAndDisconnect(req.Id, *data.ProtocolType, data.DeviceType)
			if err != nil {
				return nil, err
			}
		}
	}
	// 判断协议类型是否变化
	if oldConfig.ProtocolType != nil && data.ProtocolType != nil && *oldConfig.ProtocolType != *data.ProtocolType {
		// 协议类型有变化，要删除凭证类型，但如果新的协议类型是MQTT，要改为ACCESSTOKEN
		if *data.ProtocolType == "MQTT" {
			err = dal.UpdateDeviceConfigVoucherType(req.Id, StringPtr("ACCESSTOKEN"))
			if err != nil {
				return nil, err
			}
		} else {
			err = dal.UpdateDeviceConfigVoucherType(req.Id, nil)
			if err != nil {
				return nil, err
			}
		}
	}
	return data, nil
}

func (p *DeviceConfig) DeleteDeviceConfig(id string) error {
	err := dal.DeleteDeviceConfig(id)
	if err != nil {
		logrus.Error(err)
		return err
	}
	// 清除设备配置信息缓存
	initialize.DelDeviceConfigCache(id)
	initialize.DelDeviceDataScriptCache(id)
	return err
}

func (p *DeviceConfig) GetDeviceConfigByID(ctx context.Context, id string) (any, error) {
	var (
		db = dal.DeviceConfigQuery{}
	)
	info, err := db.First(ctx, query.DeviceConfig.ID.Eq(id))
	if err != nil {
		return nil, err
	}
	//res := dal.DeviceConfigVo{}.PoToVo(info)
	return info, nil
}

func (p *DeviceConfig) GetDeviceConfigListByPage(req *model.GetDeviceConfigListByPageReq, claims *utils.UserClaims) (map[string]interface{}, error) {

	total, list, err := dal.GetDeviceConfigListByPage(req, claims)
	if err != nil {
		return nil, err
	}
	deviceconfigListRsp := make(map[string]interface{})
	deviceconfigListRsp["total"] = total
	deviceconfigListRsp["list"] = list

	return deviceconfigListRsp, err
}

func (p *DeviceConfig) GetDeviceConfigListMenu(req *model.GetDeviceConfigListMenuReq, claims *utils.UserClaims) (any, error) {

	return dal.GetDeviceConfigSelectList(req.DeviceConfigName, claims.TenantID, req.DeviceType, req.ProtocolType)
}

func (p *DeviceConfig) BatchUpdateDeviceConfig(req *model.BatchUpdateDeviceConfigReq) error {
	err := dal.BatchUpdateDeviceConfig(req)
	if err != nil {
		logrus.Error(err)
		return err
	}
	// 清除设备信息缓存
	for _, id := range req.DeviceIds {
		initialize.DelDeviceCache(id)
		initialize.DelDeviceDataScriptCache(id)
	}
	return err
}

func (p *DeviceConfig) GetDeviceConfigConnect(ctx context.Context, deviceID string) (res *model.DeviceConfigConnectRes, err error) {
	var (
		db     = dal.DeviceQuery{}
		device = query.Device

		db1          = dal.DeviceConfigQuery{}
		deviceConfig = query.DeviceConfig
	)
	deviceInfo, err := db.First(ctx, device.ID.Eq(deviceID))
	if err != nil {
		logrus.Error(ctx, "[DeviceConfig][GetDeviceConfigConnect] device failed:", err)
		return
	}
	if deviceInfo.DeviceConfigID == nil || common.CheckEmpty(*deviceInfo.DeviceConfigID) {
		err = errors.New("return not found")
		return
	}

	_, err = db1.First(ctx, deviceConfig.ID.Eq(*deviceInfo.DeviceConfigID), deviceConfig.ProtocolType.Eq("MQTT"))
	if err != nil {
		logrus.Error(ctx, "[DeviceConfig][GetDeviceConfigConnect]deviceConfig failed:", err)
		return
	}
	res = &model.DeviceConfigConnectRes{
		AccessToken: "ACCESSTOKEN",
		Basic:       "BASIC",
	}
	return
}

// 获取凭证类型表单
func (p *DeviceConfig) GetVoucherTypeForm(deviceType string, protocolType string) (data interface{}, err error) {
	// 判断协议类型是否来自协议插件
	if protocolType == "MQTT" {
		data = map[string]interface{}{
			"AccessToken": "ACCESSTOKEN",
			"Basic":       "BASIC",
		}
		return
	}
	var pd ServicePlugin
	return pd.GetPluginForm(protocolType, deviceType, string(constant.VOUCHER_TYPE_FORM))
}

// 获取自动化一类设备Action下拉菜单；
// 包含遥测、属性、命令
func (d *DeviceConfig) GetActionByDeviceConfigID(deviceConfigID string) (any, error) {
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
		return nil, err
	}
	if deviceConfig.DeviceTemplateID == nil {
		return nil, nil
	}
	// 获取设备模板遥测
	telemetryDatas, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, err
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
		return nil, err
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
	commandDatas, err := dal.GetDeviceModelCommandDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, err
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
func (d *DeviceConfig) GetConditionByDeviceConfigID(deviceConfigID string) (any, error) {
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
		return nil, err
	}
	if deviceConfig.DeviceTemplateID == nil {
		return nil, nil
	}
	// 获取设备模板遥测
	telemetryDatas, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
	if err != nil {
		return nil, err
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
		return nil, err
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
	//eventDatas, err := dal.GetDeviceModelCommandDataList(*deviceConfig.DeviceTemplateID)
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
