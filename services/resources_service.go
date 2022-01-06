package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

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
	var memresources []models.Resources
	var memData []MemData
	cpuResult := psql.Mydb.Select([]string{"created_at", "cpu"}).Order("created_at desc").Limit(10).Find(&cpuresources)
	if cpuResult.Error != nil {
		errors.Is(cpuResult.Error, gorm.ErrRecordNotFound)
	}
	clength := len(cpuresources)
	for i := 0; i < clength/2; i++ {
		temp := cpuresources[clength-1-i]
		cpuresources[clength-1-i] = cpuresources[i]
		cpuresources[i] = temp
	}
	for _, cv := range cpuresources {
		ci := CpuData{
			CPU:       cv.CPU,
			CreatedAt: cv.CreatedAt[11 : len(cv.CreatedAt)-3],
		}
		cpuData = append(cpuData, ci)
	}
	memResult := psql.Mydb.Select([]string{"created_at", "mem"}).Order("created_at desc").Limit(10).Find(&memresources)
	if memResult.Error != nil {
		errors.Is(memResult.Error, gorm.ErrRecordNotFound)
	}
	mlength := len(memresources)
	for i := 0; i < mlength/2; i++ {
		temp := memresources[mlength-1-i]
		memresources[mlength-1-i] = memresources[i]
		memresources[i] = temp
	}
	for _, mv := range memresources {
		mi := MemData{
			MEM:       mv.MEM,
			CreatedAt: mv.CreatedAt[11 : len(mv.CreatedAt)-3],
		}
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
