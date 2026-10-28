package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func (*DeviceConfig) UpdateDeviceConfig(ctx context.Context, req model.UpdateDeviceConfigReq, claims *utils.UserClaims, authToken string) (any, error) {
	condsMap, err := StructToMapAndVerifyJson(req, "additional_info", "protocol_config", "other_config")
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}
	oldConfig, err := dal.GetDeviceConfigByIDForTenant(context.Background(), req.Id, claims.TenantID)
	if err != nil {
		return nil, errcode.New(errcode.CodeOpDenied)
	}
	delete(condsMap, "id")
	delete(condsMap, "native_thing_model_id")
	delete(condsMap, "native_thing_model_snapshot_id")
	delete(condsMap, "native_thing_model_revision")
	delete(condsMap, "native_binding_revision")
	if req.DeviceTemplateId != nil {
		if strings.TrimSpace(*req.DeviceTemplateId) == "" {
			condsMap["device_template_id"] = nil
		} else if oldConfig.NativeThingModelID != nil {
			return nil, errcode.NewWithMessage(errcode.CodeParamError, "native and legacy templates cannot be bound at the same time")
		}
	}
	bindingFieldsProvided := req.NativeThingModelID != nil || req.NativeThingModelSnapshotID != nil || req.NativeThingModelRevision != nil
	if req.NativeBindingRevision != nil && !bindingFieldsProvided {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, "native_binding_revision is only valid with a native binding update")
	}
	if bindingFieldsProvided {
		if req.NativeThingModelID == nil || req.NativeThingModelSnapshotID == nil || req.NativeThingModelRevision == nil || req.NativeBindingRevision == nil {
			return nil, errcode.NewWithMessage(errcode.CodeParamError, "native binding fields and native_binding_revision must be supplied together")
		}
		if *req.NativeBindingRevision != oldConfig.NativeBindingRevision {
			return nil, errcode.NewWithMessage(errcode.CodeOpDenied, "native binding changed; reload and try again")
		}
		if req.DeviceTemplateId != nil && strings.TrimSpace(*req.DeviceTemplateId) != "" {
			return nil, errcode.NewWithMessage(errcode.CodeParamError, "native and legacy templates cannot be bound at the same time")
		}
		modelID := strings.TrimSpace(*req.NativeThingModelID)
		snapshotID := strings.TrimSpace(*req.NativeThingModelSnapshotID)
		revision := *req.NativeThingModelRevision
		if modelID == "" && snapshotID == "" && revision == 0 {
			condsMap["native_thing_model_id"] = nil
			condsMap["native_thing_model_snapshot_id"] = nil
			condsMap["native_thing_model_revision"] = nil
		} else {
			if modelID == "" || snapshotID == "" || revision <= 0 {
				return nil, errcode.NewWithMessage(errcode.CodeParamError, "a native binding requires model ID, published revision, and snapshot ID")
			}
			if err := validateNativeSnapshot(ctx, authToken, modelID, revision, snapshotID); err != nil {
				return nil, errcode.NewWithMessage(errcode.CodeParamError, "the selected native published snapshot could not be verified")
			}
			condsMap["native_thing_model_id"] = modelID
			condsMap["native_thing_model_snapshot_id"] = snapshotID
			condsMap["native_thing_model_revision"] = revision
			condsMap["device_template_id"] = nil
		}
	}
	// 判断other_config是否变更，如果变更要对online_timeout和heartbeat进行校验，如果online_timeout和heartbeat都不为0,则组织修改，提示只允许设置其中一个
	if req.OtherConfig != nil && req.OtherConfig != oldConfig.OtherConfig {
		var otherConfig model.DeviceConfigOtherConfig
		err := json.Unmarshal([]byte(*req.OtherConfig), &otherConfig)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"err": err.Error(),
			})
		}
		if otherConfig.OnlineTimeout != 0 && otherConfig.Heartbeat != 0 {
			return nil, errcode.New(210001)
		}
	}

	logrus.Debug("condsMap:", condsMap)
	if bindingFieldsProvided {
		err = dal.UpdateDeviceConfigWithBindingCAS(req.Id, claims.TenantID, *req.NativeBindingRevision, condsMap)
	} else {
		err = dal.UpdateDeviceConfigForTenant(req.Id, claims.TenantID, condsMap)
	}
	if err != nil {
		if errors.Is(err, dal.ErrNativeBindingRevisionConflict) {
			return nil, errcode.NewWithMessage(errcode.CodeOpDenied, "native binding changed; reload and try again")
		}
		logrus.Error(err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 清除设备配置信息缓存
	initialize.DelDeviceConfigCache(req.Id)
	// 获取设备配置信息
	data, err := dal.GetDeviceConfigByIDForTenant(context.Background(), req.Id, claims.TenantID)
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

func (*DeviceConfig) DeleteDeviceConfig(id string, claims *utils.UserClaims) error {
	if _, err := dal.GetDeviceConfigByIDForTenant(context.Background(), id, claims.TenantID); err != nil {
		return errcode.New(errcode.CodeOpDenied)
	}
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
	err = dal.DeleteDeviceConfigForTenant(id, claims.TenantID)
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

func (*DeviceConfig) GetDeviceConfigByID(ctx context.Context, id string, claims *utils.UserClaims) (any, error) {
	info, err := dal.GetDeviceConfigByIDForTenant(ctx, id, claims.TenantID)
	if err != nil {
		return nil, errcode.New(errcode.CodeOpDenied)
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
	// 检查设备是否绑定了模板
	for _, deviceID := range req.DeviceIds {
		deviceInfo, err := dal.GetDeviceByID(deviceID)
		if err != nil {
			logrus.Error(err)
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		if deviceInfo.DeviceConfigID != nil && !common.CheckEmpty(*deviceInfo.DeviceConfigID) {
			deviceName := ""
			if deviceInfo.Name != nil {
				deviceName = *deviceInfo.Name
			}
			return errcode.WithVars(200071, map[string]interface{}{
				"device_name": deviceName,
			})
		}

		// 绑定设备模板
		err = dal.UpdateDeviceDeviceConfigID(deviceID, &req.DeviceConfigID)
		if err != nil {
			logrus.Error(err)
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		// 清除设备信息缓存
		initialize.DelDeviceCache(deviceID)

	}
	return nil
}

func (*DeviceConfig) GetDeviceConfigConnect(ctx context.Context, deviceID string, lang string) (res *model.DeviceConfigConnectRes, err error) {
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

	basicLabel := "账号密码认证"
	tokenLabel := "账号认证（无密码）"
	if lang != "" && (strings.Contains(lang, "en") || strings.Contains(lang, "En")) {
		basicLabel = "Username & Password"
		tokenLabel = "Username (No Password)"
	}

	res = &model.DeviceConfigConnectRes{
		Basic:       basicLabel,
		AccessToken: tokenLabel,
	}
	return
}

// 获取凭证类型表单
func (*DeviceConfig) GetVoucherTypeForm(deviceType string, protocolType string, lang string) (data interface{}, err error) {
	// 判断协议类型是否来自协议插件
	if protocolType == "MQTT" {
		basicAuth := "账号密码认证"
		tokenAuth := "账号认证（无密码）"
		if lang != "" && (strings.Contains(lang, "en") || strings.Contains(lang, "En")) {
			basicAuth = "Username & Password"
			tokenAuth = "Username (No Password)"
		}
		data = map[string]interface{}{
			basicAuth: "BASIC",
			tokenAuth: "ACCESSTOKEN",
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

func (*DeviceConfig) GetActionByDeviceConfigID(ctx context.Context, configID string, claims *utils.UserClaims, authorization string) ([]nativeMetricGroup, error) {
	return nativeConfigMetricMenu(ctx, configID, claims, authorization, "COMMAND")
}

func (*DeviceConfig) GetConditionByDeviceConfigID(ctx context.Context, configID string, claims *utils.UserClaims, authorization string) ([]nativeMetricGroup, error) {
	return nativeConfigMetricMenu(ctx, configID, claims, authorization, "TELEMETRY", "ATTRIBUTE", "EVENT")
}
