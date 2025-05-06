package service

import (
	"errors"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"

	"project/internal/dal"
	"project/internal/model"
	"project/pkg/errcode"
)

// DeviceAuth 设备动态认证服务
type DeviceAuth struct{}

// Auth 设备动态认证
func (*DeviceAuth) Auth(req *model.DeviceAuthReq) (*model.DeviceAuthRes, error) {
	// 1. 根据模板密钥查找设备模板
	deviceConfig, err := dal.GetDeviceConfigByTemplateSecret(req.TemplateSecret)
	if err != nil {
		logrus.Error("[DeviceAuth][Auth] GetDeviceConfigByTemplateSecret failed:", err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 2. 验证模板是否存在
	if deviceConfig == nil {
		return nil, errcode.New(200080)
	}

	// 3. 检查自动注册开关是否开启
	if deviceConfig.AutoRegister == 0 {
		return nil, errcode.New(200081)
	}

	// 4. 查询设备是否已存在
	device, err := dal.GetDeviceByDeviceNumber(req.DeviceNumber)
	if err != nil {
		if !errors.Is(err, dal.ErrRecordNotFound) {
			logrus.Error("[DeviceAuth][Auth] GetDeviceByDeviceNumber failed:", err)
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
	} else {
		// 返回错误
		return nil, errcode.New(200082)
	}

	// 5. 获取关联的产品信息（如果提供了ProductKey）
	var productID string
	if req.ProductKey != nil && *req.ProductKey != "" {
		product, err := dal.GetProductByProductKey(*req.ProductKey)
		if err != nil {
			logrus.Error("[DeviceAuth][Auth] GetProductByProductKey failed:", err)
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		if product == nil {
			return nil, errcode.New(200083)
		}
		productID = product.ID
	}

	// 6. 创建设备
	t := time.Now().UTC()

	if device == nil {
		// 创建新设备
		device = &model.Device{
			ID:             uuid.New(),
			DeviceNumber:   req.DeviceNumber,
			CreatedAt:      &t,
			UpdateAt:       &t,
			DeviceConfigID: &deviceConfig.ID,
			ActivateFlag:   "active",
			IsOnline:       0,
			TenantID:       deviceConfig.TenantID,
			IsEnabled:      "enable",
		}

		// 设置设备名称（如果提供）
		if req.DeviceName != nil && *req.DeviceName != "" {
			device.Name = req.DeviceName
		} else {
			defaultName := "Device_" + req.DeviceNumber
			device.Name = &defaultName
		}

		// 设置产品ID（如果有）
		if productID != "" {
			device.ProductID = &productID
		}

		// 生成设备凭证
		// 如果device_config.protocol_type为MQTT
		if deviceConfig.ProtocolType != nil && *deviceConfig.ProtocolType == "MQTT" {
			// 如果voucher_type为ACCESSTOKEN
			if deviceConfig.VoucherType != nil && *deviceConfig.VoucherType == "ACCESSTOKEN" {
				device.Voucher = `{"username":"` + uuid.New() + `"}`
			} else if deviceConfig.VoucherType != nil && *deviceConfig.VoucherType == "BASIC" {
				device.Voucher = `{"username":"` + uuid.New() + `","password":"` + uuid.New()[0:7] + `"}`
			} else {
				device.Voucher = `{"username":"` + uuid.New() + `"}`
			}
		} else {
			device.Voucher = `{"voucher":"` + uuid.New() + `"}`
		}

		// 创建设备
		err = dal.CreateDevice(device)
		if err != nil {
			logrus.Error("[DeviceAuth][Auth] CreateDevice failed:", err)
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
	} else {
		// 返回错误
		return nil, errcode.New(200082)
	}

	// 7. 构建并返回认证响应
	return &model.DeviceAuthRes{
		DeviceID: device.ID,
		Voucher:  device.Voucher,
	}, nil
}
