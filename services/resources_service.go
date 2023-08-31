package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type ResourcesService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

type CpuData struct {
	CPU       string `json:"cpu"`
	CreatedAt string `json:"created_at"`
}

type MemData struct {
	MEM       string `json:"mem"`
	CreatedAt string `json:"created_at"`
}

type NewResource struct {
	CPU []CpuData `json:"cpu"`
	MEM []MemData `json:"mem"`
}

// 获取全部Resources
func (*ResourcesService) GetNew() *models.Resources {
	var resources models.Resources
	result := psql.Mydb.Order("created_at desc").Limit(1).Find(&resources)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &resources
}

func (*ResourcesService) GetNewResource(field string) NewResource {
	var cpuresources []models.Resources
	var cpuData []CpuData
	var memData []MemData
	result := psql.Mydb.Raw(`select t.cpu,t.mem,t.created_at,t.id from (select ROW_NUMBER() OVER (ORDER BY created_at desc) 
		AS XUHAO,id,cpu,created_at,mem from  resources limit 110) as t where t.XUHAO%11=1 order by t.created_at asc`).Scan(&cpuresources)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	for _, cv := range cpuresources {
		ci := CpuData{
			CPU:       cv.CPU,
			CreatedAt: cv.CreatedAt[11:len(cv.CreatedAt)],
		}
		mi := MemData{
			MEM:       cv.MEM,
			CreatedAt: cv.CreatedAt[11:len(cv.CreatedAt)],
		}
		cpuData = append(cpuData, ci)
		memData = append(memData, mi)
	}
	nr := NewResource{
		CPU: cpuData,
		MEM: memData,
	}
	return nr
}

func (*ResourcesService) Add(cpu string, mem string, created_at string) (bool, string) {
	var uuid = uuid.GetUuid()
	resources := models.Resources{
		ID:        uuid,
		CPU:       cpu,
		MEM:       mem,
		CreatedAt: created_at,
	}
	result := psql.Mydb.Create(&resources)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

// 删除一个小时前的数据
func (*ResourcesService) Delete() bool {
	// 获取一个小时前的时间
	oneHourAgo := time.Now().Add(-time.Hour).Format("2006-01-02 15:04:05")
	result := psql.Mydb.Where("created_at < ?", oneHourAgo).Delete(&models.Resources{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	return true
}
