package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpWarningInformationService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取列表
func (*TpWarningInformationService) GetTpWarningInformationList(PaginationValidate valid.TpWarningInformationPaginationValidate) ([]models.TpWarningInformation, int64, error) {
	var TpWarningInformations []models.TpWarningInformation
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"
	var paramList []interface{}
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = ?"
		paramList = append(paramList, PaginationValidate.Id)
	}
	if PaginationValidate.ProcessingResult != "" {
		sqlWhere += " and processing_result = ?"
		paramList = append(paramList, PaginationValidate.ProcessingResult)
	}
	var count int64
	psql.Mydb.Model(&models.TpWarningInformation{}).Where(sqlWhere, paramList...).Count(&count)
	result := psql.Mydb.Model(&models.TpWarningInformation{}).Where(sqlWhere, paramList...).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpWarningInformations)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return TpWarningInformations, 0, result.Error
	}
	return TpWarningInformations, count, nil
}

// 新增数据
func (*TpWarningInformationService) AddTpWarningInformation(tp_warning_information models.TpWarningInformation) (models.TpWarningInformation, error) {
	var uuid = uuid.GetUuid()
	tp_warning_information.Id = uuid
	tp_warning_information.CreatedAt = time.Now().Unix()
	result := psql.Mydb.Create(&tp_warning_information)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_warning_information, result.Error
	}
	return tp_warning_information, nil
}

// 修改数据
func (*TpWarningInformationService) EditTpWarningInformation(tp_warning_information valid.TpWarningInformationValidate) (valid.TpWarningInformationValidate, error) {
	var warningInformationMap = map[string]interface{}{
		"ProcessingResult":       tp_warning_information.ProcessingResult,
		"ProcessingInstructions": tp_warning_information.ProcessingInstructions,
		"ProcessingTime":         time.Now().Format("2006/01/02 15:04:05"),
	}
	result := psql.Mydb.Model(&models.TpWarningInformation{}).Where("id = ?", tp_warning_information.Id).Updates(&warningInformationMap)
	if result.Error != nil {
		return tp_warning_information, result.Error
	}
	return tp_warning_information, nil
}

// 批量处理
func (*TpWarningInformationService) BatchProcessing(batchProcessing valid.BatchProcessingValidate) error {
	tx := psql.Mydb.Begin()
	for _, id := range batchProcessing.Id {
		var warningInformationMap = make(map[string]interface{})
		if batchProcessing.ProcessingResult == "1" { //处理
			warningInformationMap["ProcessingResult"] = batchProcessing.ProcessingResult
			warningInformationMap["ProcessingInstructions"] = batchProcessing.ProcessingInstructions
		} else if batchProcessing.ProcessingResult == "2" {
			warningInformationMap["ProcessingResult"] = batchProcessing.ProcessingResult
		} else {
			tx.Rollback()
			return errors.New("处理状态不正确")
		}
		warningInformationMap["ProcessingTime"] = time.Now().Format("2006/01/02 15:04:05")
		err := tx.Model(&models.TpWarningInformation{}).Where("id = ?", id).Updates(&warningInformationMap).Error
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	tx.Commit()
	return nil
}
