package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

	"gorm.io/gorm"
)

type NavigationService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 根据id获取100条Navigation数据
func (*NavigationService) List() ([]models.Navigation, int64) {
	var navigations []models.Navigation
	result := psql.Mydb.Order("count desc").Find(&navigations)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(navigations) == 0 {
		navigations = []models.Navigation{}
	}
	return navigations, result.RowsAffected
}

// 根据ID删除一条Navigation数据
func (*NavigationService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.Navigation{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据id获取一条Navigation数据
func (*NavigationService) GetNavigationByCondition(t int64, name string, data string) (*models.Navigation, int64) {
	var navigation models.Navigation
	result := psql.Mydb.Where("type = ? AND name = ? AND data = ?", t, name, data).First(&navigation)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &navigation, result.RowsAffected
}

func (*NavigationService) Increment(id string, count int64, step int64) bool {
	result := psql.Mydb.Model(&models.Navigation{}).Where("id = ?", id).Updates(map[string]interface{}{
		"count": count + step,
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

func (*NavigationService) Add(name string, t int64, data string) (bool, string) {
	var uuid = uuid.GetUuid()
	navigation := models.Navigation{
		ID:   uuid,
		Type: t,
		Name: name,
		Data: data,
	}
	result := psql.Mydb.Create(&navigation)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}
