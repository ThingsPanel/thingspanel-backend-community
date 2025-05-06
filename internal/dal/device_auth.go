package dal

import (
	"errors"
	"project/internal/model"
	"project/internal/query"

	"gorm.io/gorm"
)

// ErrRecordNotFound 记录未找到错误
var ErrRecordNotFound = gorm.ErrRecordNotFound

// GetDeviceConfigByTemplateSecret 通过模板密钥获取设备配置
func GetDeviceConfigByTemplateSecret(templateSecret string) (*model.DeviceConfig, error) {
	deviceConfig, err := query.DeviceConfig.Where(query.DeviceConfig.TemplateSecret.Eq(templateSecret)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return deviceConfig, nil
}

// GetProductByProductKey 通过产品密钥获取产品信息
func GetProductByProductKey(productKey string) (*model.Product, error) {
	product, err := query.Product.Where(query.Product.ProductKey.Eq(productKey)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return product, nil
}
