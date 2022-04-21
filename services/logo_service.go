package services

import (
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

	"ThingsPanel-Go/initialize/psql"

	"gorm.io/gorm"
)

type LogoService struct {
}

// 获取logo配置
func (*LogoService) GetLogo() models.Logo {
	var Logos []models.Logo
	var Logo models.Logo
	result := psql.Mydb.Model(&models.Logo{}).Limit(1).Find(&Logos)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(Logos) == 0 {
		Logo = models.Logo{}
	} else {
		Logo = Logos[0]
	}
	return Logo
}

// Add新增一条Logo数据
func (*LogoService) Add(logo models.Logo) (bool, string) {
	var uuid = uuid.GetUuid()
	logo.Id = uuid
	result := psql.Mydb.Create(&logo)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

// 根据ID编辑一条Logo数据
func (*LogoService) Edit(logo models.Logo) bool {
	result := psql.Mydb.Model(&models.Customer{}).Where("id = ?", logo.Id).Updates(logo)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}
