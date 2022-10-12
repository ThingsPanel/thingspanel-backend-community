package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"strconv"

	"gorm.io/gorm"
)

type ChartService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*ChartService) GetChartDetail(chart_id string) []models.Chart {
	var chart []models.Chart
	psql.Mydb.First(&chart, "id = ?", chart_id)
	return chart
}

// 获取列表
func (*ChartService) GetChartList(PaginationValidate valid.ChartPaginationValidate) (bool, []models.Chart, int64) {
	var Charts []models.Chart
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"
	if PaginationValidate.Flag != 0 {
		sqlWhere += " and flag = " + strconv.Itoa(PaginationValidate.Flag)
	}
	if PaginationValidate.Issued != 0 {
		sqlWhere += " and issued = " + strconv.Itoa(PaginationValidate.Issued)
	}
	if PaginationValidate.ChartType != "" {
		sqlWhere += " and chart_type = '" + PaginationValidate.ChartType + "'"
	}
	var count int64
	psql.Mydb.Model(&models.Chart{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.Chart{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&Charts)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, Charts, 0
	}
	return true, Charts, count
}

// 新增数据
func (*ChartService) AddChart(chart models.Chart) (bool, models.Chart) {
	var uuid = uuid.GetUuid()
	chart.ID = uuid
	result := psql.Mydb.Create(&chart)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, chart
	}
	return true, chart
}

// 修改数据
func (*ChartService) EditChart(chart valid.ChartValidate) bool {
	result := psql.Mydb.Model(&models.Chart{}).Where("id = ?", chart.Id).Updates(&chart)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*ChartService) DeleteChart(chart models.Chart) bool {
	result := psql.Mydb.Delete(&chart)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}
