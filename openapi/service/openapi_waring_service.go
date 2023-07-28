package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"
	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type OpenapiWaringService struct {
}

// 获取列表
func (*OpenapiWaringService) GetTpWarningInformationList(PaginationValidate valid.TpWarningInformationPaginationValidate, tenantId string) ([]models.TpWarningInformation, int64, error) {
	var TpWarningInformations []models.TpWarningInformation
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1 and tenant_id = ?"
	var paramList []interface{}
	paramList = append(paramList, tenantId)
	//if PaginationValidate.Id != "" {
	//	sqlWhere += " and id = ?"
	//	paramList = append(paramList, PaginationValidate.Id)
	//}
	if PaginationValidate.ProcessingResult != "" {
		sqlWhere += " and processing_result = ?"
		paramList = append(paramList, PaginationValidate.ProcessingResult)
	}
	//if PaginationValidate.WarningLevel != "" {
	//	sqlWhere += " and warning_level = ?"
	//	paramList = append(paramList, PaginationValidate.WarningLevel)
	//}
	//if PaginationValidate.StartTime != "" && PaginationValidate.EndTime != "" {
	//	// 字符串转int64
	//	startTime, _ := strconv.ParseInt(PaginationValidate.StartTime, 10, 64)
	//	endTime, _ := strconv.ParseInt(PaginationValidate.EndTime, 10, 64)
	//	// 判断开始时间是否大于结束时间
	//	if startTime > endTime {
	//		return TpWarningInformations, 0, errors.New("开始时间不能大于结束时间")
	//	}
	//	paramList = append(paramList, startTime, endTime)
	//	sqlWhere += " and created_at between ? and ?"
	//}
	var count int64
	psql.Mydb.Model(&models.TpWarningInformation{}).Where(sqlWhere, paramList...).Count(&count)
	result := psql.Mydb.Model(&models.TpWarningInformation{}).Where(sqlWhere, paramList...).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpWarningInformations)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)

		return TpWarningInformations, 0, result.Error
	}
	return TpWarningInformations, count, nil
}
