package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"strconv"
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

//表面结构体
type TableInfo struct {
	TableName string `json:"table_name" alias:"名称" gorm:"column:table_name"`
	Comment   string `json:"comment" alias:"描述" gorm:"column:table_description"`
}

//获取一条数详情
func (*TpDataServicesConfig) GetTpDataServicesConfigDetail(id string) models.TpDataServicesConfig {
	var TpDataServicesConfig models.TpDataServicesConfig
	psql.Mydb.First(&TpDataServicesConfig, "id = ? ", id)
	return TpDataServicesConfig
}

// 获取列表
func (*TpDataServicesConfig) GetTpDataServicesConfigList(PaginationValidate valid.TpDataServicesConfigPaginationValidate) (bool, []models.TpDataServicesConfig, int64) {
	var tpDataServicesConfigs []models.TpDataServicesConfig
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpDataServicesConfig{})
	if PaginationValidate.Name != "" {
		db.Where("name like ?", "%"+PaginationValidate.Name+"%")
	}
	if PaginationValidate.EnableFlag != "" {
		db.Where("enable_flag = ?", PaginationValidate.EnableFlag)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&tpDataServicesConfigs)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, tpDataServicesConfigs, 0
	}
	return true, tpDataServicesConfigs, count
}

// 新增数据
func (*TpDataServicesConfig) AddTpDataServicesConfig(reqdata valid.AddTpDataServicesConfigValidate) (models.TpDataServicesConfig, error) {

	var TpDataServicesConfigModel models.TpDataServicesConfig

	if illegalDataSql(reqdata.DataSql) {
		return TpDataServicesConfigModel, errors.New("sql语句不合法")
	}

	appkey := utils.GenerateAppKey(models.Appkey_Length)
	secretKey := utils.GenerateAppKey(models.SecretKey_Length)

	TpDataServicesConfigModel = models.TpDataServicesConfig{
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

	TpDataServicesConfigModel.SecretKey = ""
	return TpDataServicesConfigModel, nil
}

// 修改数据
func (*TpDataServicesConfig) EditTpDataServicesConfig(reqdata valid.EditTpDataServicesConfigValidate) error {

	if illegalDataSql(reqdata.DataSql) {
		return errors.New("sql语句不合法")
	}

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
	if illegalDataSql(reqsql) {
		return nil, errors.New("sql语句不合法")
	}
	var result []map[string]interface{}
	db := psql.Mydb.Raw(reqsql + " limit 10").Scan(&result)
	if db.Error != nil {
		logs.Error(db.Error, gorm.ErrRecordNotFound)
		return result, db.Error

	}
	return result, nil
}

// 检查sql语句是否合法
func illegalDataSql(reqsql string) bool {

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
			return true
		}
	}
	return false
}

// 通过appkey获取数据服务配置
func (*TpDataServicesConfig) GetTpDataServicesConfigByAppKey(appkey string) (models.TpDataServicesConfig, error) {
	var TpDataServicesConfig models.TpDataServicesConfig
	result := psql.Mydb.Where("app_key = ? and enable_flag = '1'", appkey).First(&TpDataServicesConfig)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return TpDataServicesConfig, result.Error
	}
	return TpDataServicesConfig, nil
}

// 通过appkey获取数据
func (*TpDataServicesConfig) GetDataByAppkey(reqData valid.GetDataPaginationValidate, appKey string) ([]map[string]interface{}, error) {
	var tpDataServicesConfig TpDataServicesConfig
	// 根据appKey获取数据服务配置
	dataServicesConfig, err := tpDataServicesConfig.GetTpDataServicesConfigByAppKey(appKey)
	if err != nil {
		return nil, err
	}
	var reqsql string = dataServicesConfig.DataSql
	if illegalDataSql(reqsql) {
		return nil, errors.New("sql语句不合法")
	}
	// 判断是否有分页
	if reqData.CurrentPage > 0 && reqData.PerPage > 0 {
		offset := (reqData.CurrentPage - 1) * reqData.PerPage
		// 整数转字符串
		reqsql = reqsql + " limit " + strconv.Itoa(reqData.PerPage) + " offset " + strconv.Itoa(offset)
	} else {
		reqsql = reqsql + " limit 1000"
	}
	var result []map[string]interface{}
	db := psql.Mydb.Raw(reqsql + " limit 10").Scan(&result)
	if db.Error != nil {
		logs.Error(db.Error, gorm.ErrRecordNotFound)
		return result, db.Error

	}
	return result, nil
}

func (*TpDataServicesConfig) GetTableNames() ([]TableInfo, error) {
	var tableNames []TableInfo
	sql := `SELECT table_name, obj_description (c.oid) AS table_description
	FROM information_schema.tables t
	LEFT JOIN pg_class c ON t.table_name = c.relname
	WHERE table_type = 'BASE TABLE' AND table_schema = 'public'`
	result := psql.Mydb.Raw(sql).Scan(&tableNames)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tableNames, result.Error
	}
	return tableNames, nil

}
func (*TpDataServicesConfig) GetTableField(table string) ([]map[string]interface{}, error) {
	sql := `SELECT 
    a.attname AS field,
    format_type ( a.atttypid,a.atttypmod ) AS TYPE,
    col_description(a.attrelid, a.attnum) AS comment
FROM 
    pg_class as c,
    pg_attribute as a 
WHERE 
    c.relname = 'asset' 
    AND a.attnum > 0 
    AND a.attrelid = c.oid`
	var result []map[string]interface{}
	db := psql.Mydb.Raw(sql).Scan(&result)
	if db.Error != nil {
		logs.Error(db.Error, gorm.ErrRecordNotFound)
		return result, db.Error

	}
	return result, nil
}
