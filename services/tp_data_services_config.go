package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpDataServicesConfig struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpDataServicesConfig) GetTpDataServicesConfigDetail(id string) models.TpDataServicesConfig {
	var TpDataServicesConfig models.TpDataServicesConfig
	psql.Mydb.First(&TpDataServicesConfig, "id = ? ", id)
	return TpDataServicesConfig
}

// 获取列表
func (*TpDataServicesConfig) GetTpDataServicesConfigList(PaginationValidate valid.TpDataServicesConfigPaginationValidate) (bool, []models.TpDataServicesConfig, int64) {
	var TpDataServicesConfigs []models.TpDataServicesConfig
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpDataServicesConfig{})
	if PaginationValidate.Name != "" {
		db.Where("name like ?", "%"+PaginationValidate.Name+"%")
	}
	// if PaginationValidate.AppKey != "" {
	// 	db.Where("app_key like ?", "%"+PaginationValidate.AppKey+"%")
	// }
	// if PaginationValidate.SignatureMode != "" {
	// 	db.Where("SignatureMode = ?", PaginationValidate.SignatureMode)
	// }
	// if PaginationValidate.IpWhitelist != "" {
	// 	db.Where("ip_whitelist like ?", "%"+PaginationValidate.IpWhitelist+"%")
	// }
	// if PaginationValidate.EnableFlag != "" {
	// 	db.Where("name = ?", PaginationValidate.EnableFlag)
	// }
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpDataServicesConfigs)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpDataServicesConfigs, 0
	}
	return true, TpDataServicesConfigs, count
}

// 新增数据
func (*TpDataServicesConfig) AddTpDataServicesConfig(reqdata valid.AddTpDataServicesConfigValidate, appkey, secretKey string) (models.TpDataServicesConfig, error) {
	var TpDataServicesConfigModel models.TpDataServicesConfig = models.TpDataServicesConfig{
		Id:            utils.GetUuid(),
		Name:          reqdata.Name,
		AppKey:        appkey,
		SecretKey:     secretKey,
		SignatureMode: reqdata.SignatureMode,
		IpWhitelist:   reqdata.IpWhitelist,
		DataSql:       reqdata.DataSql,
		ApiFlag:       reqdata.ApiFlag,
		TimeInterval:  reqdata.TimeInterval,
		EnableFlag:    reqdata.EnableFlag,
		Description:   reqdata.Description,
		CreatedAt:     time.Now().Unix(),
		Remark:        reqdata.Remark,
	}
	result := psql.Mydb.Create(&TpDataServicesConfigModel)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return TpDataServicesConfigModel, result.Error
	}
	return TpDataServicesConfigModel, nil
}

// 修改数据
func (*TpDataServicesConfig) EditTpDataServicesConfig(reqdata valid.EditTpDataServicesConfigValidate) error {

	var tpdataservicesconfigmodel = models.TpDataServicesConfig{
		Name:          reqdata.Name,
		SignatureMode: reqdata.SignatureMode,
		IpWhitelist:   reqdata.IpWhitelist,
		DataSql:       reqdata.DataSql,
		ApiFlag:       reqdata.ApiFlag,
		TimeInterval:  reqdata.TimeInterval,
		EnableFlag:    reqdata.EnableFlag,
		Description:   reqdata.Description,
		Remark:        reqdata.Remark,
	}
	result := psql.Mydb.Model(&models.TpDataServicesConfig{}).Where("id = ? ", reqdata.Id).Updates(&tpdataservicesconfigmodel)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return result.Error
	}
	return nil
}

// 删除数据
func (*TpDataServicesConfig) DeleteTpDataServicesConfig(TpDataServicesConfig models.TpDataServicesConfig) error {
	result := psql.Mydb.Delete(&TpDataServicesConfig)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
}

func (*TpDataServicesConfig) QuizeTpDataServicesConfig(reqsql string) ([]map[string]interface{}, error) {

	if !strings.Contains(reqsql, "select") || !strings.Contains(reqsql, "SELECT") {
		return nil, errors.New("无效sql语句")
	}

	var not_allow_key = [8]string{}
	not_allow_key[0] = "delete"
	not_allow_key[1] = "drop"
	not_allow_key[2] = "truncate"
	not_allow_key[3] = "update"
	not_allow_key[4] = "insert"
	not_allow_key[5] = "alter"
	not_allow_key[6] = "create"
	not_allow_key[7] = "replace"

	for _, v := range not_allow_key {
		if strings.Contains(reqsql, v) || strings.Contains(reqsql, strings.ToUpper(v)) {
			return nil, errors.New("sql语句只能查询")
		}
	}

	var result []map[string]interface{}
	db := psql.Mydb.Raw(reqsql + " limit 10").Scan(&result)
	if db.Error != nil {
		logs.Error(db.Error, gorm.ErrRecordNotFound)
		return result, db.Error

	}
	return result, nil
}
