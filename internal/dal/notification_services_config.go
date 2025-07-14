package dal

import (
	"errors"

	model "project/internal/model"
	query "project/internal/query"

	"gorm.io/gorm"
)

// 根据类型获取配置（邮件/短信）
func GetNotificationServicesConfigByType(noticeType string) (*model.NotificationServicesConfig, error) {
	data, err := query.NotificationServicesConfig.Where(query.NotificationServicesConfig.NoticeType.Eq(noticeType)).Take()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return data, nil
}

// 创建/保存配置
func SaveNotificationServicesConfig(data *model.NotificationServicesConfig) (*model.NotificationServicesConfig, error) {
	err := query.NotificationServicesConfig.Save(data)
	if err != nil {

		return nil, err
	}
	return data, nil
}
