package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"time"

	"gorm.io/gorm"
)

type BusinessService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

type PaginateBusiness struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	IsDevice  int    `json:"is_device"`
}

type AllBusiness struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

// Paginate 分页获取business数据
func (*BusinessService) Paginate(name string, offset int, pageSize int) ([]models.Business, int64) {
	var businesses []models.Business
	var count int64
	if name != "" {
		result := psql.Mydb.Model(&models.Business{}).Where("name LIKE ?", "%"+name+"%").Order("created_at desc").Limit(pageSize).Offset(offset).Find(&businesses)
		psql.Mydb.Model(&models.Business{}).Where("name LIKE ?", "%"+name+"%").Count(&count)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
		}
		if len(businesses) == 0 {
			businesses = []models.Business{}
		}
		return businesses, count
	} else {
		result := psql.Mydb.Model(&models.Business{}).Order("created_at desc").Limit(pageSize).Offset(offset).Find(&businesses)
		psql.Mydb.Model(&models.Business{}).Count(&count)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
		}
		if len(businesses) == 0 {
			businesses = []models.Business{}
		}
		return businesses, count
	}
}

// 根据id获取一条business数据
func (*BusinessService) GetBusinessById(id string) (*models.Business, int64) {
	var business models.Business
	result := psql.Mydb.Where("id = ?", id).First(&business)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &business, result.RowsAffected
}

// Add新增一条business数据
func (*BusinessService) Add(name, tenantId string) (bool, string) {
	bussiness_id := uuid.GetUuid()
	business := models.Business{ID: bussiness_id, Name: name, TenantId: tenantId, CreatedAt: time.Now().Unix()}
	result := psql.Mydb.Create(&business)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	//新增根分组
	asset_id := uuid.GetUuid()
	asset := models.Asset{
		ID:         asset_id,
		Name:       name,
		Tier:       1,
		ParentID:   "0",
		BusinessID: bussiness_id,
		TenantId:   tenantId,
	}
	psql.Mydb.Create(asset)
	return true, bussiness_id
}

// 根据ID编辑一条business数据
func (*BusinessService) Edit(id string, name string, tenantId string) bool {
	result := psql.Mydb.Model(&models.Business{}).Where("id = ? and tenant_id = ?", id, tenantId).Update("name", name)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据ID删除一条business数据
func (*BusinessService) Delete(id, tenantId string) bool {
	result := psql.Mydb.Where("id = ? and tenantid = ?", id, tenantId).Delete(&models.Business{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 获取全部
func (*BusinessService) All() ([]AllBusiness, int64) {
	var businesses []AllBusiness
	var count int64
	result := psql.Mydb.Model(&models.Business{}).Find(&businesses)
	psql.Mydb.Model(&models.Business{}).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(businesses) == 0 {
		businesses = []AllBusiness{}
	}
	return businesses, count
}
