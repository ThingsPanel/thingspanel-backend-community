package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"project/initialize"
	protocolplugin "project/internal/service/protocol_plugin"
	"project/pkg/constant"
	"project/pkg/errcode"
	global "project/pkg/global"
	"project/third_party/others/http_client"

	dal "project/internal/dal"
	model "project/internal/model"
	query "project/internal/query"
	common "project/pkg/common"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

type Device struct{}

func (*Device) CreateDevice(req model.CreateDeviceReq, claims *utils.UserClaims) (device model.Device, err error) {
	t := time.Now().UTC()

	device.ID = uuid.New()
	device.Name = req.Name
	if req.Voucher == nil {
		if req.DeviceConfigId != nil && *req.DeviceConfigId != "" {
			// 获取设备配置
			deviceConfig, err := dal.GetDeviceConfigByID(*req.DeviceConfigId)
			if err != nil {
				return device, err
			}
			if deviceConfig.ProtocolType != nil && *deviceConfig.ProtocolType == "MQTT" {
				if deviceConfig.VoucherType != nil && *deviceConfig.VoucherType == "BASIC" {
					device.Voucher = `{"username":"` + uuid.New()[0:22] + `","password":"` + uuid.New()[0:7] + `"}`
				} else {
					device.Voucher = `{"username":"` + uuid.New()[0:22] + `"}`
				}
			} else {
				// 其他协议默认一个UUID
				device.Voucher = `{"default":"` + uuid.New() + `"}`
			}
		} else {
			device.Voucher = `{"username":"` + uuid.New()[0:22] + `","password":"` + uuid.New()[0:7] + `"}` // 随机生成
		}
	} else {
		device.Voucher = *req.Voucher
	}
	device.TenantID = claims.TenantID
	device.CreatedAt = &t
	device.UpdateAt = &t

	// 没送默认和token一样
	if req.DeviceNumber == nil {
		device.DeviceNumber = device.ID
	} else {
		device.DeviceNumber = *req.DeviceNumber
	}

	device.ProductID = req.ProductID
	device.ParentID = req.ParentID

	device.Protocol = req.Protocol

	device.Label = req.Label
	device.Location = req.Location
	device.SubDeviceAddr = req.SubDeviceAddr
	device.CurrentVersion = req.CurrentVersion
	device.AdditionalInfo = req.AdditionalInfo
	device.ProtocolConfig = req.ProtocolConfig
	device.Remark1 = req.Remark1
	device.Remark2 = req.Remark2
	device.Remark3 = req.Remark3
	device.AccessWay = req.AccessWay
	device.Description = req.Description
	if req.DeviceConfigId != nil && *req.DeviceConfigId == "" {
		req.DeviceConfigId = nil
	}
	device.DeviceConfigID = req.DeviceConfigId
	var IsOnline int16 = 0
	device.IsOnline = IsOnline
	device.ActivateFlag = "active"
	err = dal.CreateDevice(&device)
	if err != nil {
		return device, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	return device, err
}

// 服务接入批量创建设备
func (*Device) CreateDeviceBatch(req model.BatchCreateDeviceReq, claims *utils.UserClaims) (data any, err error) {
	t := time.Now().UTC()
	var deviceList []*model.Device
	for _, v := range req.DeviceList {
		if v.DeviceName == "" && v.DeviceNumber == "" && v.DeviceConfigId == "" {
			continue
		}
		// 校验必填字段
		if v.DeviceNumber == "" {
			return nil, errcode.WithVars(100005, map[string]interface{}{
				"field": "device_number",
			})
		}

		if v.DeviceConfigId == "" {
			return nil, errcode.WithVars(100005, map[string]interface{}{
				"field": "device_config_id",
			})
		}

		if v.DeviceName == "" {
			return nil, errcode.WithVars(100005, map[string]interface{}{
				"field": "device_name",
			})
		}

		device := model.Device{
			ID:              uuid.New(),
			Name:            &v.DeviceName,
			DeviceNumber:    v.DeviceNumber,
			Voucher:         `{"username":"` + uuid.New()[0:22] + `"}`,
			TenantID:        claims.TenantID,
			CreatedAt:       &t,
			UpdateAt:        &t,
			AccessWay:       StringPtr("B"),
			Description:     v.Description,
			DeviceConfigID:  &v.DeviceConfigId,
			IsOnline:        0,
			ActivateFlag:    "active",
			ServiceAccessID: &req.ServiceAccessId,
		}
		deviceList = append(deviceList, &device)
	}
	err = dal.CreateDeviceBatch(deviceList)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	} else {
		// 发送通知给服务插件
		// 获取服务接入信息
		serviceAccess, err := dal.GetServiceAccessByID(req.ServiceAccessId)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
				"message":   "create device success, query service access failed",
			})
		}
		// 查询服务地址
		_, host, err := dal.GetServicePluginHttpAddressByID(serviceAccess.ServicePluginID)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
				"message":   "create device success, query service plugin failed",
			})
		}
		dataMap := make(map[string]interface{})
		dataMap["service_access_id"] = req.ServiceAccessId
		// 将dataMap转json字符串
		dataBytes, err := json.Marshal(dataMap)
		if err != nil {
			return nil, errcode.WithData(100004, map[string]interface{}{
				"message": "create device success, marshal data failed",
			})
		}
		// 通知服务插件
		logrus.Debug("发送通知给服务插件")

		rsp, err := http_client.Notification("1", string(dataBytes), host)
		if err != nil {
			return nil, errcode.WithVars(105001, map[string]interface{}{
				"error": "create device success, notification failed" + err.Error(),
			})
		}
		logrus.Debug("通知服务插件成功")
		logrus.Debug(string(rsp))
	}

	return deviceList, err
}

func (*Device) UpdateDevice(req model.UpdateDeviceReq, _ *utils.UserClaims) (*model.Device, error) {
	// 获取设备原信息
	oldDevice, err := dal.GetDeviceByID(req.Id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 如果req.DeviceNumber被修改，需要校验req.DeviceNumber是否系统唯一
	if req.DeviceNumber != nil && *req.DeviceNumber != "" {
		if oldDevice.DeviceNumber != *req.DeviceNumber {
			// Check if device number already exists
			exists, err := dal.CheckDeviceNumberExists(*req.DeviceNumber)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
					"message":   "check device number exists failed",
				})
			}
			if exists {
				return nil, errcode.New(204004)
			}
		}
	}

	// device.ID = req.Id
	// device.Name = req.Name

	t := time.Now().UTC()

	// if req.Voucher != nil && *req.Voucher != "" {
	// 	device.Voucher = *req.Voucher
	// }
	// 不能更新租户id
	// device.TenantID = claims.TenantID
	// device.UpdateAt = &t
	condsMap, err := StructToMapAndVerifyJson(req, "additional_info", "protocol_config")
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"message": err.Error(),
		})
	}
	condsMap["update_at"] = t

	device, err := dal.UpdateDeviceByMap(req.Id, condsMap)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 清除设备缓存
	initialize.DelDeviceCache(req.Id)

	// 如果是子设备地址被修改，需要通知插件断开网关让其重连
	if req.SubDeviceAddr != nil && *req.SubDeviceAddr != "" {
		if oldDevice.SubDeviceAddr != nil && *oldDevice.SubDeviceAddr != "" {
			if *oldDevice.SubDeviceAddr != *req.SubDeviceAddr {
				// 通知协议插件，该方法会校验协议
				if protocolplugin.DisconnectDeviceByDeviceID(req.Id) != nil {
					logrus.Error("DisconnectDeviceByDeviceID failed:", err)
				}
			}
		}
	}

	return device, err
}

func (*Device) ActiveDevice(req model.ActiveDeviceReq) (any, error) {
	device, err := dal.GetDeviceByDeviceNumber(req.DeviceNumber)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if device.ActivateFlag == "active" {
		return nil, errcode.New(204002)
	}
	device.DeviceNumber = req.DeviceNumber
	if req.Name != "" {
		req.Name = uuid.New()[0:8]
	}
	device.Name = &req.Name
	device.ActivateFlag = "active"
	t := time.Now().UTC()
	device.UpdateAt = &t
	device.ActivateAt = &t
	device, e := dal.UpdateDevice(device)
	if e != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": e.Error(),
		})
	}
	// 清除设备缓存
	initialize.DelDeviceCache(device.ID)
	return device, nil
}

func (*Device) DeleteDevice(id string, userClaims *utils.UserClaims) error {
	// 如果有子设备，不允许删除
	data, err := dal.GetSubDeviceListByParentID(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if len(data) > 0 {
		return errcode.WithData(200063, map[string]interface{}{
			"message": "device has sub device,please remove sub device first",
		})
	}
	// 关联了场景联动，不允许删除
	conditions, err1 := dal.GetDeviceTriggerConditionListByDeviceId(id)
	if err1 != nil {
		return err1
	}
	if len(conditions) > 0 {
		return errcode.WithData(200062, map[string]interface{}{
			"message": "device has scene,please remove scene first",
		})
	}

	tx := query.Use(global.DB).Begin()
	// 删除遥测当前数据
	err = dal.DeleteCurrentTelemetryDataByDeviceId(id, tx)
	if err != nil {
		tx.Rollback()
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 删除遥测历史数据
	err = dal.DeleteTelemetrDataByDeviceId(id, tx)
	if err != nil {
		tx.Rollback()
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 删除控制下发历史数据
	err = dal.DeleteTelemetrySetLogsByDeviceId(id, tx)
	if err != nil {
		tx.Rollback()
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 删除属性数据

	err = dal.DeleteAttributeDataByDeviceId(id, tx)
	if err != nil {
		tx.Rollback()
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 删除属性下发历史数据
	err = dal.DeleteAttributeDataByDeviceIdTx(id, tx)
	if err != nil {
		tx.Rollback()
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 删除事件历史数据
	err = dal.DeleteEventDataByDeviceId(id, tx)
	if err != nil {
		tx.Rollback()
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 删除事件历史数据
	err = dal.DeleteCommandSetLogsByDeviceId(id, tx)
	if err != nil {
		tx.Rollback()
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 删除设备
	err = dal.DeleteDeviceWithTx(id, userClaims.TenantID, tx)
	if err != nil {
		tx.Rollback()
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 提交事务
	tx.Commit()
	// 清除设备缓存
	initialize.DelDeviceCache(id)
	// 通知协议插件
	if protocolplugin.DisconnectDeviceByDeviceID(id) != nil {
		logrus.Error("DisconnectDeviceByDeviceID failed:", err)
	}

	return nil
}

func (*Device) GetDeviceByIDV1(id string) (map[string]interface{}, error) {
	data, err := dal.GetDeviceDetail(id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
			"message":   "get device failed",
		})
	}
	// 判断data是否有key为device_config_id
	if v, ok := data["device_config_id"]; ok {
		// 判断是否为nil或者为空字符串
		if v == nil || v == "" {
			return data, nil
		}
		// 转换为string
		deviceConfigID := fmt.Sprintf("%v", v)
		// 获取设备配置
		deviceConfig, err := dal.GetDeviceConfigByID(deviceConfigID)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
				"message":   "get device config failed",
			})
		}
		data["device_config"] = deviceConfig
		// result, err := dal.GetDeviceOnline(context.Background(), []model.DeviceOnline{
		// 	{
		// 		DeviceConfigId: &deviceConfigID,
		// 		DeviceId:       id,
		// 	},
		// })
		// if err != nil {
		// 	return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
		// 		"error":   err.Error(),
		// 		"message": "get device online failed",
		// 	})
		// }
		// if isOnline, ok := result[id]; ok {
		// 	data["device_status"] = isOnline
		// } else {
		// 	data["device_status"] = data["is_online"]
		// }
		data["device_status"] = data["is_online"]
	}

	return data, err
}

func (*Device) GetDeviceListByPage(req *model.GetDeviceListByPageReq, u *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetDeviceListByPage(req, u.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if len(list) > 0 {
		for i := range list {
			list[i].DeviceStatus = list[i].IsOnline
			if list[i].WarnStatus == "N" || list[i].WarnStatus == "" {
				list[i].WarnStatus = "N"
			} else {
				list[i].WarnStatus = "Y"
			}
		}
	}
	deviceListRsp := make(map[string]interface{})
	deviceListRsp["total"] = total
	deviceListRsp["list"] = list

	return deviceListRsp, err
}

func (d *Device) CheckDeviceNumber(deviceNumber string) (*errcode.Error, bool) {
	device, err := query.Device.Where(query.Device.DeviceNumber.Eq(deviceNumber)).First()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 如果设备不存在，说明设备号不可用
			return errcode.WithVars(204001, map[string]interface{}{
				"error": deviceNumber,
			}), false
		}
		// 数据库错误
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		}), false
	}

	if device.ActivateFlag == "active" {
		return errcode.New(204002), false
	}

	return errcode.WithVars(204003, nil), true
}

func (*Device) GetDevicePreRegisterListByPage(req *model.GetDevicePreRegisterListByPageReq, u *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetDevicePreRegisterListByPage(req, u.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	deviceListRsp := make(map[string]interface{})
	deviceListRsp["total"] = total
	deviceListRsp["list"] = list

	return deviceListRsp, err
}

// 移除子设备
func (*Device) RemoveSubDevice(id string, claims *utils.UserClaims) error {
	// 获取设备信息
	device, err := dal.GetDeviceByID(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	err = dal.RemoveSubDevice(id, claims.TenantID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 通知协议插件
	if device.ParentID != nil {
		if protocolplugin.DisconnectDeviceByDeviceID(*device.ParentID) != nil {
			logrus.Error(err)
		}
	}
	// 清除设备缓存
	initialize.DelDeviceCache(id)
	return nil
}

func (*Device) ExportDevicePreRegister(req model.ExportPreRegisterReq, claims *utils.UserClaims) (string, error) {
	qd := query.Device
	queryBuilder := qd.WithContext(context.Background())
	if req.BatchNumber != nil && *req.BatchNumber != "" {
		queryBuilder = queryBuilder.Where(qd.BatchNumber.Eq(*req.BatchNumber))
	}
	if req.ActivateFlag != nil && *req.ActivateFlag != "" {
		queryBuilder = queryBuilder.Where(qd.ActivateFlag.Eq(*req.ActivateFlag))
	}
	data, err := queryBuilder.Where(
		query.Device.ProductID.Eq(req.ProductID),
		query.Device.TenantID.Eq(claims.TenantID)).
		Select(query.Device.BatchNumber,
			query.Device.Voucher, qd.DeviceNumber).
		Find()
	if err != nil {
		return "", err
	}
	// 导出到文件
	excel_file := excelize.NewFile()
	index, _ := excel_file.NewSheet("Sheet1")
	excel_file.SetActiveSheet(index)
	excel_file.SetCellValue("Sheet1", "A1", "batchNumber")
	excel_file.SetCellValue("Sheet1", "B1", "voucher")
	excel_file.SetCellValue("Sheet1", "C1", "deviceNumber")
	// 写入数据
	for i, v := range data {
		excel_file.SetCellValue("Sheet1", fmt.Sprintf("A%d", i+2), v.BatchNumber)
		excel_file.SetCellValue("Sheet1", fmt.Sprintf("B%d", i+2), v.Voucher)
		excel_file.SetCellValue("Sheet1", fmt.Sprintf("C%d", i+2), v.DeviceNumber)
	}
	uploadDir := "./files/excel/"
	err = os.MkdirAll(uploadDir, os.ModePerm)
	if err != nil {
		return "", err
	}
	excelName := "files/excel/product_data" + time.Now().Format("20060102150405") + ".xlsx"
	if err := excel_file.SaveAs(excelName); err != nil {
		logrus.Error(err)
	}
	return excelName, nil
}

func (*Device) GetTenantDeviceList(req *model.GetDeviceMenuReq, tenantID string) ([]map[string]interface{}, error) {
	var data []map[string]interface{}
	var err error

	if req.GroupId != "" {
		// 获取设备组下的设备
		data, err = dal.GetDeviceSelectByGroupId(tenantID, req.GroupId, req.DeviceName, req.BindConfig)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
	} else {
		data, err = dal.DeviceQuery{}.GetDeviceSelect(tenantID, req.DeviceName, req.BindConfig)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
	}

	if data == nil {
		data = []map[string]interface{}{}
	}
	return data, nil
	// list, err := dal.DeviceQuery{}.Find(ctx, device.TenantID.Eq(tenantID))
	// if err != nil {
	// 	logrus.Error(ctx, "[GetTenantDeviceList]failed:", err)
	// 	return res, err
	// }

	// deviceConfigIDS := make([]string, 0, len(list))
	// for _, info := range list {
	// 	if info.DeviceConfigID != nil && !common.CheckEmpty(*info.DeviceConfigID) {
	// 		deviceConfigIDS = append(deviceConfigIDS, *info.DeviceConfigID)
	// 	}
	// }

	// configList, err := dal.DeviceConfigQuery{}.Find(ctx, deviceConfig.ID.In(deviceConfigIDS...))
	// if err != nil {
	// 	logrus.Error(ctx, "[GetTenantDeviceList]Get device_config failed:", err)
	// 	return res, err
	// }

	// res = logic.DeviceLogic{}.GetTenantDeviceList(list, configList)
}

func (*Device) GetDeviceList(ctx context.Context, userClaims *utils.UserClaims) ([]map[string]interface{}, error) {
	list, err := dal.DeviceQuery{}.GetGatewayUnrelatedDeviceList(ctx, userClaims.TenantID)
	if err != nil {
		logrus.Error(ctx, "[GetDeviceList]failed:", err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return list, err
}

func (*Device) CreateSonDevice(ctx context.Context, param *model.CreateSonDeviceRes) error {
	var (
		device = query.Device
		db     = dal.DeviceQuery{}
	)
	// param.SonID使用英文逗号分割
	sonIDs := strings.Split(param.SonID, ",")
	for _, sonID := range sonIDs {
		// 验证子设备无绑定 & 设备类型= 网关类型 & 设备设置 id not is null
		deviceInfo, err := db.First(ctx, device.ID.Eq(sonID), device.ParentID.IsNull(), device.DeviceConfigID.IsNotNull())
		if err != nil {
			logrus.Error(ctx, "[CreateSonDevice]First failed:", err)
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}

		// 验证子设备关联配置 设备类型 = 网关类型
		_, err = dal.DeviceConfigQuery{}.First(ctx, query.DeviceConfig.ID.Eq(*deviceInfo.DeviceConfigID), query.DeviceConfig.DeviceType.Eq(strconv.Itoa(constant.GATEWAY_SON_DEVICE)))
		if err != nil {
			logrus.Error(ctx, "[CreateSonDevice]First device_configs failed:", err)
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}

		deviceInfo.ParentID = &param.ID
		deviceInfo.SubDeviceAddr = StringPtr(uuid.New()[0:8])
		// 更新子设备 parentID
		if err = db.Update(ctx, deviceInfo, device.ParentID, device.SubDeviceAddr); err != nil {
			logrus.Error(ctx, "[CreateSonDevice]update failed:", err)
		} else {
			// 通知协议插件
			err := protocolplugin.DisconnectDeviceByDeviceID(param.ID)
			if err != nil {
				logrus.Error(err)
			}

		}
	}
	return nil
}

// 获取凭证表单
func (d *Device) DeviceConnectForm(ctx context.Context, param *model.DeviceConnectFormReq) (any, error) {
	var voucherType string
	var deviceType string
	var protocolType string
	// 获取设备信息
	device, err := dal.GetDeviceByID(param.DeviceID)
	if err != nil {
		logrus.Error(ctx, "get device failed:", err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 判断设备配置id不为空
	if device.DeviceConfigID != nil {
		// 获取设备配置信息
		deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			logrus.Error(ctx, "get device_config failed:", err)
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		if deviceConfig.DeviceType == strconv.Itoa(constant.GATEWAY_SON_DEVICE) {
			// 子设备没有凭证表单
			return nil, nil
		}
		// 可以没有凭证类型
		if deviceConfig.VoucherType != nil {
			voucherType = *deviceConfig.VoucherType
		}
		deviceType = deviceConfig.DeviceType
		if deviceConfig.ProtocolType != nil {
			protocolType = *deviceConfig.ProtocolType
		} else {
			return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"msg": "device_config protocol_type is null",
			})
		}

	} else {
		// 默认设备
		voucherType = "BASIC"
		deviceType = "1"
		protocolType = "MQTT"
	}
	data, err := d.GetVoucherTypeForm(voucherType, deviceType, protocolType)
	if err != nil {
		logrus.Error(ctx, "get voucher type form failed:", err)
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"msg": "get voucher type form failed",
		})
	}

	return data, nil
}

// 获取凭证类型表单
func (*Device) GetVoucherTypeForm(voucherType string, deviceType string, protocolType string) (interface{}, error) {
	// 没有设备配置，返回默认表单
	p1 := &model.DeviceConnectFormRes{
		DataKey:     "username",
		Label:       "MQTT Username",
		Placeholder: "MQTT Username",
		Type:        "input",
		Validate: model.DeviceConnectFormValidateRes{
			Message:  "The username cannot be empty",
			Required: true,
			Type:     "string",
		},
	}
	p2 := &model.DeviceConnectFormRes{
		DataKey:     "password",
		Label:       "MQTT Password",
		Placeholder: "MQTT password",
		Type:        "input",
		Validate: model.DeviceConnectFormValidateRes{
			Required: true,
			Type:     "string",
		},
	}
	if protocolType == "MQTT" {
		if voucherType == "BASIC" {
			return []*model.DeviceConnectFormRes{p1, p2}, nil
		} else if voucherType == "ACCESSTOKEN" {
			p1.Label = "MQTT Username(Password is empty)"
			return []*model.DeviceConnectFormRes{p1}, nil
		} else {
			return nil, fmt.Errorf("voucher type is error: %s", voucherType)
		}
	}
	// 去协议插件获取凭证表单
	logrus.Debug("去服务插件获取凭证表单")
	var pp ServicePlugin
	return pp.GetPluginForm(protocolType, deviceType, string(constant.VOUCHER_FORM))
}

func (*Device) DeviceConnect(ctx context.Context, param *model.DeviceConnectFormReq, lang string) (any, error) {
	// 获取设备信息
	device, err := dal.GetDeviceByID(param.DeviceID)
	if err != nil {
		logrus.Error(ctx, "[Device][DeviceConnect]GetDeviceByID failed:", err)
		// 返回错误
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device info failed:" + err.Error(),
			"id":    param.DeviceID,
		})
	}
	var protocolType string
	var deviceType string
	if device.DeviceConfigID != nil {
		// 获取设备配置信息
		deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			logrus.Error(ctx, "[Device][DeviceConnect]GetDeviceConfigByID failed:", err)
			// 返回错误
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "get device config info failed:" + err.Error(),
				"id":    param.DeviceID,
			})
		}
		if deviceConfig.ProtocolType != nil {
			protocolType = *deviceConfig.ProtocolType
			deviceType = deviceConfig.DeviceType
		}
	} else {
		protocolType = "MQTT"
		deviceType = "1"
	}

	var rsp any
	if protocolType == "MQTT" {
		// 取配置的MQTT接入地址
		accessAddress := viper.GetString("mqtt.access_address")
		if accessAddress == "" {
			accessAddress = ":1883"
		}

		if deviceType == "1" {
			rsp = map[string]string{
				global.ResponseHandler.ErrManager.GetMessage(500001, lang): accessAddress,
				global.ResponseHandler.ErrManager.GetMessage(500002, lang): "mqtt_" + param.DeviceID[0:12],
				global.ResponseHandler.ErrManager.GetMessage(500003, lang): "devices/telemetry",
				global.ResponseHandler.ErrManager.GetMessage(500004, lang): fmt.Sprintf("devices/telemetry/control/%s", device.DeviceNumber),
				global.ResponseHandler.ErrManager.GetMessage(500005, lang): "{\"switch\":1}",
			}
		} else if deviceType == "2" {
			remark := `{"gateway_data":{"switch":1},"sub_device_data":{"sub_device_address":{"switch":1}}`
			rsp = map[string]string{
				global.ResponseHandler.ErrManager.GetMessage(500001, lang): accessAddress,
				global.ResponseHandler.ErrManager.GetMessage(500002, lang): "mqtt_" + param.DeviceID[0:12],
				global.ResponseHandler.ErrManager.GetMessage(500003, lang): "gateway/telemetry",
				global.ResponseHandler.ErrManager.GetMessage(500004, lang): fmt.Sprintf("gateway/telemetry/control/%s", device.DeviceNumber),
				global.ResponseHandler.ErrManager.GetMessage(500005, lang): remark,
			}
		}
	} else {
		// 根据协议类型和设备类型获取协议插件信息
		pp, err := dal.GetServicePluginByServiceIdentifier(protocolType)
		if err != nil {
			logrus.Error(ctx, "get protocol plugin failed:", err)
			return nil, err
		}
		info := make(map[string]interface{})
		if pp.ServiceType == int32(1) {
			// pp.ServiceConfig转换为model.ProtocolAccessConfig
			var protocolAccessConfig model.ProtocolAccessConfig
			err = json.Unmarshal([]byte(*pp.ServiceConfig), &protocolAccessConfig)
			if err != nil {
				logrus.Error(ctx, "Error occurred during unmarshalling. Error: %s", err)
			}
			info[global.ResponseHandler.ErrManager.GetMessage(500001, lang)] = protocolAccessConfig.AccessAddress
		}
		rsp = info
	}
	return rsp, err
}

// 更换设备配置
func (*Device) UpdateDeviceConfig(param *model.ChangeDeviceConfigReq) error {
	// 查找原设备配置
	device, err := dal.GetDeviceByID(param.DeviceID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device info failed:" + err.Error(),
			"id":    param.DeviceID,
		})
	}
	if device.DeviceConfigID != nil {
		// 获取设备配置
		deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "get device config info failed:" + err.Error(),
				"id":    param.DeviceID,
			})
		}
		if deviceConfig.DeviceType == strconv.Itoa(constant.GATEWAY_DEVICE) {
			// 检查有没有子设备
			data, err := dal.GetSubDeviceListByParentID(param.DeviceID)
			if err != nil {
				return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get sub device list failed:" + err.Error(),
					"id":    param.DeviceID,
				})
			}
			if len(data) > 0 {
				return errcode.New(200061)
			}
		} else if deviceConfig.DeviceType == strconv.Itoa(constant.GATEWAY_SON_DEVICE) {
			// 检查有没有关联的网关
			if device.ParentID != nil {
				return errcode.New(200063)
			}
		}
	}

	if param.DeviceConfigID == nil {
		return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "device config id is null",
		})
	}
	if param.DeviceConfigID != nil && *param.DeviceConfigID == "" {
		param.DeviceConfigID = nil
	}
	// 更新设备配置id
	err = dal.DeviceQuery{}.ChangeDeviceConfig(param.DeviceID, param.DeviceConfigID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "update device config failed:" + err.Error(),
			"id":    param.DeviceID,
		})
	}
	// 清除设备缓存
	initialize.DelDeviceCache(param.DeviceID)
	// 清除设备数据脚本缓存
	initialize.DelDeviceDataScriptCache(param.DeviceID)
	return err
}

func (*Device) UpdateDeviceVoucher(ctx context.Context, param *model.UpdateDeviceVoucherReq) (string, error) {
	var (
		db     = dal.DeviceQuery{}
		device = query.Device

		voucher string
		err     error
	)
	// 查询旧凭证
	deviceInfo, err := dal.GetDeviceByID(param.DeviceID)
	if err != nil {
		return "", errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device info failed:" + err.Error(),
			"id":    param.DeviceID,
		})
	}
	if v, ok := param.Voucher.(string); ok {
		voucher = v
	} else {
		voucher, err = common.JsonToString(param.Voucher)
		if err != nil {
			logrus.Error(ctx, "[Device][UpdateDeviceVoucher]JsonToString failed:", err)
			return "", err
		}
	}
	if param.Voucher == "{}" {
		return "", nil
	}
	info := &model.Device{
		ID:      param.DeviceID,
		Voucher: voucher,
	}
	if err = db.Update(ctx, info, device.Voucher); err != nil {
		logrus.Error(ctx, "[Device][UpdateDeviceVoucher]failed:", err)
		return info.Voucher, err
	}
	// 清除设备缓存
	initialize.DelDeviceCache(param.DeviceID)
	if deviceInfo.Voucher != voucher {
		// 清除broker的缓存
		global.REDIS.Del(ctx, deviceInfo.Voucher)
	}

	info, err = db.First(ctx, device.ID.Eq(param.DeviceID))
	if err != nil {
		logrus.Error(ctx, "[Device][UpdateDeviceVoucher]first failed:", err)
		return info.Voucher, err
	}

	return info.Voucher, err
}

// GetSubList
func (*Device) GetSubList(ctx context.Context, parent_id string, page, pageSize int64, userClaims *utils.UserClaims) ([]model.GetSubListResp, int64, error) {
	data, count, err := dal.DeviceQuery{}.GetSubList(ctx, parent_id, pageSize, page, userClaims.TenantID)
	if err != nil {
		return nil, 0, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get sub device list failed:" + err.Error(),
			"id":    parent_id,
		})
	}

	return data, count, nil
}

// 获取自动化下拉标识，看板下拉标识
func (*Device) GetMetrics(device_id string) ([]model.GetModelSourceATRes, error) {
	res := make([]model.GetModelSourceATRes, 0)

	telemetryDatas, err := dal.GetCurrentTelemetryDataEvolution(device_id)
	if err != nil && len(telemetryDatas) == 0 {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get telemetry data failed:" + err.Error(),
			"id":    device_id,
		})
	}

	attributeDatas, err := dal.GetAttributeDataList(device_id)
	if err != nil && len(attributeDatas) == 0 {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get attribute data failed:" + err.Error(),
			"id":    device_id,
		})
	}

	device, err := dal.GetDeviceByID(device_id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device failed:" + err.Error(),
			"id":    device_id,
		})
	}

	var deviceConfig *model.DeviceConfig
	var eventDatas []*model.DeviceModelEvent
	var commandDatas []*model.DeviceModelCommand
	telemetryModelMap := make(map[string]*model.DeviceModelTelemetry)
	deviceAttributeModelMap := make(map[string]*model.DeviceModelAttribute)
	if device.DeviceConfigID != nil {
		// 获取设备配置
		deviceConfig, err = dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "get device config failed:" + err.Error(),
				"id":    device_id,
			})
		}
		// 是否有设备模板
		if deviceConfig.DeviceTemplateID != nil {
			// 查询遥测模型
			telemetryModel, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get device model telemetry failed:" + err.Error(),
					"id":    device_id,
				})
			}
			// 遍历并转换为map,供下面填入遥测模板数据使用
			for _, v := range telemetryModel {
				telemetryModelMap[v.DataIdentifier] = v
			}

			attributeList, err := dal.GetDeviceModelAttributeDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get device model attribute failed:" + err.Error(),
					"id":    device_id,
				})
			}
			// 遍历并转换为map
			for _, v := range attributeList {
				deviceAttributeModelMap[v.DataIdentifier] = v
			}
			eventDatas, err = dal.GetDeviceModelEventDataList(*deviceConfig.DeviceTemplateID)
			if err != nil && len(eventDatas) == 0 {
				return nil, err
			}

			commandDatas, err = dal.GetDeviceModelCommandDataList(*deviceConfig.DeviceTemplateID)
			if err != nil && len(eventDatas) == 0 {
				return nil, err
			}
		}

	}
	s := "string"
	telemetryDatasMap := make(map[string]*model.TelemetryCurrentData)
	if len(telemetryDatas) != 0 {
		resInfo := model.GetModelSourceATRes{
			DataSourceTypeRes: string(constant.TelemetrySource),
			Options:           make([]*model.Options, 0),
		}

		for _, telemetry := range telemetryDatas {
			telemetryDatasMap[telemetry.Key] = telemetry
			var dt string

			if telemetry.BoolV != nil {
				dt = "boolean"
			} else if telemetry.NumberV != nil {
				dt = "number"
			} else if telemetry.StringV != nil {
				dt = "string"
			}

			info := &model.Options{
				Key:      telemetry.Key,
				DataType: &dt,
			}

			item, ok := telemetryModelMap[telemetry.Key]
			if ok {
				if item.DataType != nil && *item.DataType == "Enum" {
					info.DataType = item.DataType
					json.Unmarshal([]byte(*item.AdditionalInfo), &info.Enum)
				}
				info.Label = item.DataName
			}

			resInfo.Options = append(resInfo.Options, info)
		}
		// 遍历telemetryModelMap，如果遥测数据telemetryDatasMap中没有，需要加到resInfo.Options
		for k, v := range telemetryModelMap {
			_, ok := telemetryDatasMap[k]
			if !ok {
				info := &model.Options{
					Key:      k,
					DataType: v.DataType,
					Label:    v.DataName,
				}
				if v.DataType != nil && *v.DataType == "Enum" {
					json.Unmarshal([]byte(*v.AdditionalInfo), &info.Enum)
				}
				resInfo.Options = append(resInfo.Options, info)
			}
		}
		res = append(res, resInfo)
	}

	// 映射
	attributeDatasMap := make(map[string]*model.AttributeData)
	if len(attributeDatas) != 0 {
		resInfo := model.GetModelSourceATRes{
			DataSourceTypeRes: string(constant.AttributeSource),
			Options:           make([]*model.Options, 0),
		}

		for _, attribute := range attributeDatas {
			var dt string
			if attribute.BoolV != nil {
				dt = "boolean"
			} else if attribute.NumberV != nil {
				dt = "number"
			} else if attribute.StringV != nil {
				dt = "string"
			}

			info := &model.Options{
				Key:      attribute.Key,
				DataType: &dt,
			}

			item, ok := deviceAttributeModelMap[attribute.Key]
			if ok {
				if item.DataType != nil && *item.DataType == "Enum" {
					info.DataType = item.DataType
					json.Unmarshal([]byte(*item.AdditionalInfo), &info.Enum)
				}
				info.Label = item.DataName
			}

			resInfo.Options = append(resInfo.Options, info)
		}
		// 遍历attributeDatasMap，如果遥测数据attributeDatasMap中没有，需要加到resInfo.Options
		for k, v := range deviceAttributeModelMap {
			_, ok := attributeDatasMap[k]
			if !ok {
				info := &model.Options{
					Key:      k,
					Label:    v.DataName,
					DataType: v.DataType,
				}
				if v.DataType != nil && *v.DataType == "Enum" {
					json.Unmarshal([]byte(*v.AdditionalInfo), &info.Enum)
				}
				resInfo.Options = append(resInfo.Options, info)
			}
		}
		res = append(res, resInfo)
	}

	if len(eventDatas) != 0 {
		resInfo := model.GetModelSourceATRes{
			DataSourceTypeRes: string(constant.EventSource),
			Options:           make([]*model.Options, 0),
		}

		for _, event := range eventDatas {
			info := &model.Options{
				Key:      event.DataIdentifier,
				Label:    event.DataName,
				DataType: &s,
			}
			info.Label = event.DataName
			resInfo.Options = append(resInfo.Options, info)
		}
		res = append(res, resInfo)
	}

	if len(commandDatas) != 0 {
		resInfo := model.GetModelSourceATRes{
			DataSourceTypeRes: string(constant.CommandSource),
			Options:           make([]*model.Options, 0),
		}

		for _, command := range commandDatas {
			info := &model.Options{
				Key:      command.DataIdentifier,
				Label:    command.DataName,
				DataType: &s,
			}
			info.Label = command.DataName
			resInfo.Options = append(resInfo.Options, info)
		}
		res = append(res, resInfo)
	}

	return res, nil
}

// 获取自动化一类设备Action下拉菜单；
// 包含遥测、属性、命令
func (*Device) GetActionByDeviceID(deviceID string) (any, error) {
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
	//
	//http://47.251.45.205:9999/api/v1/device/metrics/condition/menu?device_id=653e34cf-eb4d-2219-b182-79bc1f8379f1
	// 获取设备配置信息
	device, err := dal.GetDeviceByID(deviceID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device failed:" + err.Error(),
			"id":    deviceID,
		})
	}
	type option struct {
		Key           string  `json:"key"`
		Label         *string `json:"label"`
		DataType      *string `json:"data_type"`
		Uint          *string `json:"unit"`
		ReadWriteFlag *string `json:"read_write_flag"`
	}
	type actionModelSource struct {
		DataSourceTypeRes string    `json:"data_source_type"`
		Options           []*option `json:"options"`
		Label             string    `json:"label"`
	}
	// 获取设备遥测当前值
	telemetryDatas, err := dal.GetCurrentTelemetryDataEvolution(deviceID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device current telemetry failed:" + err.Error(),
			"id":    deviceID,
		})
	}
	var telemetryOptions []*option
	for _, telemetry := range telemetryDatas {
		var o option
		o.Key = telemetry.Key
		switch {
		case telemetry.BoolV != nil:
			o.DataType = StringPtr("Boolean")
		case telemetry.NumberV != nil:
			o.DataType = StringPtr("Number")
		case telemetry.StringV != nil:
			o.DataType = StringPtr("String")
		}
		telemetryOptions = append(telemetryOptions, &o)
	}
	// 获取设备属性当前值
	attributeDatas, err := dal.GetAttributeDataList(deviceID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device current attribute failed:" + err.Error(),
			"id":    deviceID,
		})
	}
	var attributeOptions []*option
	for _, attribute := range attributeDatas {
		var o option
		o.Key = attribute.Key
		switch {
		case attribute.BoolV != nil:
			o.DataType = StringPtr("Boolean")
		case attribute.NumberV != nil:
			o.DataType = StringPtr("Number")
		case attribute.StringV != nil:
			o.DataType = StringPtr("String")
		}
		attributeOptions = append(attributeOptions, &o)
	}
	var commandOptions []*option
	res := make([]actionModelSource, 0)
	if device.DeviceConfigID != nil {
		// 获取设备配置信息
		deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "get device config failed:" + err.Error(),
				"id":    deviceID,
			})
		}
		if deviceConfig.DeviceTemplateID != nil {
			// 获取设备模板遥测
			telemetryModel, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get device template telemetry failed:" + err.Error(),
					"id":    deviceID,
				})
			}
			// 有映射的做映射
			for _, model := range telemetryModel {
				// 存在模型对应字段的标志
				flag := false
				for _, v := range telemetryOptions {
					if model.DataIdentifier == v.Key {
						v.Label = model.DataName
						v.DataType = model.DataType
						v.Uint = model.Unit
						v.ReadWriteFlag = model.ReadWriteFlag
						flag = true
					}
				}
				if !flag {
					// 没有对应的字段，直接添加
					var o option
					o.Key = model.DataIdentifier
					o.Label = model.DataName
					o.DataType = model.DataType
					o.Uint = model.Unit
					o.ReadWriteFlag = model.ReadWriteFlag
					telemetryOptions = append(telemetryOptions, &o)
				}
			}
			// 获取设备模板属性
			attributeModel, err := dal.GetDeviceModelAttributeDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get device template attribute failed:" + err.Error(),
					"id":    deviceID,
				})
			}
			attributeOptions := make([]*option, 0)
			for _, model := range attributeModel {
				// 存在模型对应字段的标志
				flag := false
				for _, v := range attributeOptions {
					if model.DataIdentifier == v.Key {
						v.Label = model.DataName
						v.DataType = model.DataType
						v.Uint = model.Unit
						v.ReadWriteFlag = model.ReadWriteFlag
						flag = true
					}
				}
				if !flag {
					// 没有对应的字段，直接添加
					var o option
					o.Key = model.DataIdentifier
					o.Label = model.DataName
					o.DataType = model.DataType
					o.Uint = model.Unit
					o.ReadWriteFlag = model.ReadWriteFlag
					attributeOptions = append(attributeOptions, &o)
				}
			}
			// 获取设备模板命令
			commandDatas, err := dal.GetDeviceModelCommandDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get device template command failed:" + err.Error(),
					"id":    deviceID,
				})
			}

			for _, command := range commandDatas {
				var o option
				o.Key = command.DataIdentifier
				o.Label = command.DataName
				o.DataType = StringPtr("String")
				commandOptions = append(commandOptions, &o)
			}
		}

	}
	// 返回

	if len(telemetryOptions) != 0 {
		res = append(res, actionModelSource{
			Label:             "遥测",
			DataSourceTypeRes: string(constant.TelemetrySource),
			Options:           telemetryOptions,
		})
	}
	if len(attributeOptions) != 0 {
		res = append(res, actionModelSource{
			Label:             "属性",
			DataSourceTypeRes: string(constant.AttributeSource),
			Options:           attributeOptions,
		})
	}
	if len(commandOptions) != 0 {
		res = append(res, actionModelSource{
			Label:             "命令",
			DataSourceTypeRes: string(constant.CommandSource),
			Options:           commandOptions,
		})
	}
	res = append(res, actionModelSource{
		Label:             "自定义遥测",
		DataSourceTypeRes: "c_telemetry",
		Options:           []*option{},
	})
	res = append(res, actionModelSource{
		Label:             "自定义属性",
		DataSourceTypeRes: "c_attribute",
		Options:           []*option{},
	})
	res = append(res, actionModelSource{
		Label:             "自定义命令",
		DataSourceTypeRes: "c_command",
		Options:           []*option{},
	})

	return res, nil
}

// 获取自动化一类设备Condition下拉菜单；
// 包含遥测、属性、事件
func (*Device) GetConditionByDeviceID(deviceID string) (any, error) {
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
	device, err := dal.GetDeviceByID(deviceID)
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
	// 获取设备遥测当前值
	telemetryDatas, err := dal.GetCurrentTelemetryDataEvolution(deviceID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device current telemetry failed:" + err.Error(),
			"id":    deviceID,
		})
	}
	var telemetryOptions []*options
	for _, telemetry := range telemetryDatas {
		var o options
		o.Key = telemetry.Key
		switch {
		case telemetry.BoolV != nil:
			o.DataType = StringPtr("boolean")
		case telemetry.NumberV != nil:
			o.DataType = StringPtr("number")
		case telemetry.StringV != nil:
			o.DataType = StringPtr("string")
		}
		telemetryOptions = append(telemetryOptions, &o)
	}
	// 获取设备属性当前值
	attributeDatas, err := dal.GetAttributeDataList(deviceID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device current attribute failed:" + err.Error(),
			"id":    deviceID,
		})
	}
	var attributeOptions []*options
	for _, attribute := range attributeDatas {
		var o options
		o.Key = attribute.Key
		switch {
		case attribute.BoolV != nil:
			o.DataType = StringPtr("boolean")
		case attribute.NumberV != nil:
			o.DataType = StringPtr("number")
		case attribute.StringV != nil:
			o.DataType = StringPtr("string")
		}
		attributeOptions = append(attributeOptions, &o)
	}
	var eventOptions []*options
	res := make([]actionModelSource, 0)
	if device.DeviceConfigID != nil {
		// 获取设备配置信息
		deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "get device config failed:" + err.Error(),
				"id":    deviceID,
			})
		}
		if deviceConfig.DeviceTemplateID != nil {
			// 获取设备模板遥测
			telemetryModel, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get device template telemetry failed:" + err.Error(),
					"id":    deviceID,
				})
			}
			// 有映射的做映射
			for _, model := range telemetryModel {
				// 存在模型对应字段的标志
				flag := false
				for _, v := range telemetryOptions {
					if model.DataIdentifier == v.Key {
						v.Label = model.DataName
						v.DataType = model.DataType
						v.Uint = model.Unit
						flag = true
					}
				}
				if !flag {
					// 没有对应的字段，直接添加
					var o options
					o.Key = model.DataIdentifier
					o.Label = model.DataName
					o.DataType = model.DataType
					o.Uint = model.Unit
					telemetryOptions = append(telemetryOptions, &o)
				}
			}
			// 获取设备模板属性
			attributeModel, err := dal.GetDeviceModelAttributeDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get device template attribute failed:" + err.Error(),
					"id":    deviceID,
				})
			}
			// attributeOptions := make([]*options, 0)
			for _, model := range attributeModel {
				// 存在模型对应字段的标志
				flag := false
				for _, v := range attributeOptions {
					if model.DataIdentifier == v.Key {
						v.Label = model.DataName
						v.DataType = model.DataType
						v.Uint = model.Unit
						flag = true
					}
				}
				if !flag {
					// 没有对应的字段，直接添加
					var o options
					o.Key = model.DataIdentifier
					o.Label = model.DataName
					o.DataType = model.DataType
					o.Uint = model.Unit
					attributeOptions = append(attributeOptions, &o)
				}
			}
			// 获取设备模板命令
			eventDatas, err := dal.GetDeviceModelEventDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": "get device template command failed:" + err.Error(),
					"id":    deviceID,
				})
			}

			for _, event := range eventDatas {
				var o options
				o.Key = event.DataIdentifier
				o.Label = event.DataName
				o.DataType = StringPtr("string")
				eventOptions = append(eventOptions, &o)
			}
		}
	}
	// 返回

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

func (*Device) GetMapTelemetry(device_id string) (map[string]interface{}, error) {
	res := make(map[string]interface{}, 0)

	device, err := dal.GetDeviceByID(device_id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device failed:" + err.Error(),
			"id":    device_id,
		})
	}

	telemetry, err := dal.GetCurrentTelemetryDataEvolution(device_id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device current telemetry failed:" + err.Error(),
			"id":    device_id,
		})
	}

	str := make([]string, 0)

	for _, v := range telemetry {
		str = append(str, v.Key)
	}

	deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device config failed:" + err.Error(),
			"id":    device_id,
		})
	}

	labelMap, err := dal.GetDataNameByIdentifierAndTemplateId(*deviceConfig.DeviceTemplateID, str...)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device template failed:" + err.Error(),
			"id":    device_id,
		})
	}

	telemetryData := make([]map[string]interface{}, 0)
	for _, v := range telemetry {
		tmp := make(map[string]interface{})
		tmp["key"] = v.Key

		if v.BoolV != nil {
			tmp["value"] = v.BoolV
		} else if v.NumberV != nil {
			tmp["value"] = v.NumberV
		} else if v.StringV != nil {
			tmp["value"] = v.StringV
		}

		var label *string
		var unit *string
		for _, v2 := range labelMap {
			if v2.DataIdentifier == v.Key {
				label = v2.DataName
				unit = v2.Unit
			}
		}
		tmp["label"] = label
		tmp["unit"] = unit
		telemetryData = append(telemetryData, tmp)
	}

	res["device_id"] = device.ID
	res["is_online"] = device.IsOnline
	res["last_push_time"] = telemetry[0].T
	res["telemetry_data"] = telemetryData
	res["device_name"] = device.Name

	return res, nil
}

// 有模板且有图表的设备下拉菜单
func (*Device) GetDeviceTemplateChartSelect(userClaims *utils.UserClaims) (any, error) {
	// 获取设备模板
	tenantId := userClaims.TenantID
	data, err := dal.GetDeviceTemplateChartSelect(tenantId)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, nil
}

func (*Device) GetDeviceOnlineStatus(device_id string) (map[string]int, error) {
	deviceInfo, err := dal.GetDeviceByID(device_id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "get device info failed:" + err.Error(),
			"id":    device_id,
		})
	}
	// result, err := dal.GetDeviceOnline(context.Background(), []model.DeviceOnline{
	// 	{
	// 		DeviceConfigId: deviceInfo.DeviceConfigID,
	// 		DeviceId:       device_id,
	// 	},
	// })
	// if err != nil {
	// 	return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
	// 		"error": "get device online status failed:" + err.Error(),
	// 		"id":    device_id,
	// 	})
	// }
	data := make(map[string]int)
	// if isOnline, ok := result[device_id]; ok {
	// 	data["device_status"] = isOnline
	// } else {
	// 	data["device_status"] = int(deviceInfo.IsOnline)
	// }
	data["device_status"] = int(deviceInfo.IsOnline)
	data["is_online"] = data["device_status"]
	return data, nil
}

func (*Device) GatewayRegister(req model.GatewayRegisterReq) (model.GatewayRegisterRes, error) {
	var (
		device *model.Device
		err    error
	)
	device, err = dal.GetDeviceByDeviceNumber(req.GatewayId)
	if err == nil {
		var voucher model.DeviceVoucher
		_ = json.Unmarshal([]byte(device.Voucher), &voucher)

		return model.GatewayRegisterRes{
			MqttUsername: voucher.Username,
			MqttPassword: voucher.Password,
			MqttClientId: device.ID,
		}, nil
	} else {
		device = &model.Device{}
	}

	// device model.Device
	result := model.GatewayRegisterRes{
		MqttUsername: uuid.New()[0:22],
		MqttPassword: uuid.New()[0:7],
		MqttClientId: uuid.New(),
	}
	t := time.Now().UTC()

	device.ID = result.MqttClientId
	device.Name = &req.Model
	deviceConfigId := dal.GetDeviceConfigIdByName(req.Model)
	if deviceConfigId == nil || *deviceConfigId == "" {
		deviceConfigId = nil
	}
	device.DeviceConfigID = deviceConfigId
	logrus.Info(device.DeviceConfigID)
	device.Voucher = `{"username":"` + result.MqttUsername + `","password":"` + result.MqttPassword + `"}`
	device.TenantID = req.TenantId
	device.CreatedAt = &t
	device.UpdateAt = &t
	device.DeviceNumber = req.GatewayId
	device.IsOnline = 1
	device.ActivateFlag = "active"
	return result, dal.CreateDevice(device)
}

func (*Device) GatewayDeviceRegister(req model.DeviceRegisterReq) (model.DeviceRegisterRes, error) {
	device, err := dal.GetDeviceByID(req.DeviceId)
	if err != nil {
		var voucher model.DeviceVoucher
		_ = json.Unmarshal([]byte(device.Voucher), &voucher)

		return model.DeviceRegisterRes{
			Type:    "sub-register-response",
			Status:  "fail",
			Message: "未查询到网关设备信息",
		}, nil
	}
	res := model.DeviceRegisterRes{
		Type:         "sub-register-response",
		Status:       "success",
		Message:      "success",
		RegistersRes: make(map[string]model.DeviceSubRegisterRes),
	}
	t := time.Now().UTC()

	for _, v := range req.Registers {
		if dal.GetSubDeviceExists(req.DeviceId, v.SubAddr) {
			registerRes := res.RegistersRes
			registerRes[v.SubAddr] = model.DeviceSubRegisterRes{
				Result:    1,
				Errorcode: "exists",
				SubAddr:   v.SubAddr,
			}
			res.RegistersRes = registerRes
			continue
		}
		subDeviceItem := model.Device{}

		subDeviceItem.ID = uuid.New()
		deviceConfigId := dal.GetDeviceConfigIdByName(v.Model)
		if deviceConfigId == nil || *deviceConfigId == "" {
			deviceConfigId = nil
		}
		subDeviceItem.DeviceConfigID = deviceConfigId
		subDeviceItem.ParentID = &req.DeviceId
		subDeviceItem.Name = &v.Model
		subDeviceItem.Voucher = `{"username":"` + uuid.New() + `"}`
		subDeviceItem.TenantID = device.TenantID
		subDeviceItem.CreatedAt = &t
		subDeviceItem.UpdateAt = &t
		subDeviceItem.DeviceNumber = uuid.New()
		subDeviceItem.IsOnline = 1
		subDeviceItem.ActivateFlag = "active"
		subDeviceItem.SubDeviceAddr = &v.SubAddr

		// subDevices = append(subDevices, subDeviceItem)
		err = dal.CreateDevice(&subDeviceItem)
		subRegisterRes := model.DeviceSubRegisterRes{
			Result:    0,
			Errorcode: "",
			Message:   "success",
			SubAddr:   v.SubAddr,
		}
		if err != nil {
			subRegisterRes.Result = 1
			subRegisterRes.Errorcode = "exists"
		}
		registerRes := res.RegistersRes
		registerRes[v.SubAddr] = subRegisterRes
		res.RegistersRes = registerRes
	}

	return res, nil
}

// 设备单指标图表数据查询
func (*Device) GetDeviceMetricsChart(param *model.GetDeviceMetricsChartReq, userClaims *utils.UserClaims) (any, error) {
	var data model.DeviceMetricsChartData

	data.DeviceID = param.DeviceID
	data.DataType = param.DataType
	data.Key = param.Key
	data.AggregateWindow = param.AggregateWindow
	data.AggregateFunction = param.AggregateFunction
	data.TimeRange = param.TimeRange

	switch param.DataType {
	case "telemetry":
		// 获取设备单指标最新值
		telemetryCurrentDataList, err := dal.GetCurrentTelemetryDataEvolutionByKeys(param.DeviceID, []string{param.Key})
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "get device metrics latest value failed:" + err.Error(),
				"id":    param.DeviceID,
			})
		}

		if len(telemetryCurrentDataList) > 0 {
			if telemetryCurrentDataList[0].BoolV != nil {
				var v interface{} = *telemetryCurrentDataList[0].BoolV
				data.Value = &v
			} else if telemetryCurrentDataList[0].NumberV != nil {
				var v interface{} = *telemetryCurrentDataList[0].NumberV
				data.Value = &v
			} else if telemetryCurrentDataList[0].StringV != nil {
				var v interface{} = *telemetryCurrentDataList[0].StringV
				data.Value = &v
			}
			// 毫秒
			timestamp := telemetryCurrentDataList[0].T.Unix() * 1000
			data.Timestamp = &timestamp
		}

		if param.DataMode == "history" {
			// 获取设备单指标历史数据
			var req model.GetTelemetryStatisticReq
			req.DeviceId = param.DeviceID
			req.Key = param.Key

			if param.AggregateWindow != nil {
				req.AggregateWindow = *param.AggregateWindow
				if req.AggregateWindow != "no_aggregate" {
					if param.AggregateFunction != nil {
						req.AggregateFunction = *param.AggregateFunction
					} else {
						req.AggregateFunction = "avg"
						data.AggregateFunction = &req.AggregateFunction
					}
				}
			} else {
				req.AggregateWindow = "no_aggregate"
				data.AggregateWindow = &req.AggregateWindow
			}
			if param.TimeRange != nil {
				req.TimeRange = *param.TimeRange
			} else {
				req.TimeRange = "last_1h"
				data.TimeRange = &req.TimeRange
			}
			historyData, err := GroupApp.TelemetryData.GetTelemetrServeStatisticData(&req)
			if err != nil {
				return nil, err
			}
			// 如果historyData不是[]map[string]interface{}类型，则返回空points
			if _, ok := historyData.([]map[string]interface{}); !ok {
				// 空数组
				data.Points = &[]model.DataPoint{}
			} else {
				// 将historyData赋值给data.Points
				hData := historyData.([]map[string]interface{})
				points := make([]model.DataPoint, 0)
				for _, v := range hData {
					point := model.DataPoint{
						T: v["x"].(int64),
					}
					// 根据类型断言处理 y 值
					if yVal, ok := v["y"]; ok {
						switch val := yVal.(type) {
						case float64:
							point.V = val
						case int64:
							point.V = float64(val)
						case int:
							point.V = float64(val)
						case string:
							if f, err := strconv.ParseFloat(val, 64); err == nil {
								point.V = f
							}
						}
					}
					points = append(points, point)
				}
				data.Points = &points
			}
		}
	case "attribute":
		attributeData, err := dal.GetAttributeOneKeysByDeviceId(param.DeviceID, param.Key)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "get device metrics latest value failed:" + err.Error(),
				"id":    param.DeviceID,
			})
		}
		if attributeData != nil {
			if attributeData.BoolV != nil {
				var v interface{} = *attributeData.BoolV
				data.Value = &v
			} else if attributeData.NumberV != nil {
				var v interface{} = *attributeData.NumberV
				data.Value = &v
			} else if attributeData.StringV != nil {
				var v interface{} = *attributeData.StringV
				data.Value = &v
			}
			// 毫秒
			timestamp := attributeData.T.Unix() * 1000
			data.Timestamp = &timestamp
		}
	case "event":
		eventData, err := dal.GetEventDataOneKeysByDeviceId(param.DeviceID, param.Key)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "get device metrics latest value failed:" + err.Error(),
				"id":    param.DeviceID,
			})
		}
		if eventData != nil {
			var v interface{} = *eventData.Datum
			data.Value = &v
		}

	case "command":
		data.Value = nil
	}

	return data, nil
}

// 设备选择器
func (*Device) GetDeviceSelector(req model.DeviceSelectorReq, userClaims *utils.UserClaims) (*model.DeviceSelectorRes, error) {
	tenantId := userClaims.TenantID
	list, err := dal.GetDeviceSelector(req, tenantId)
	if err != nil {
		return nil, err
	}
	return list, nil
}
