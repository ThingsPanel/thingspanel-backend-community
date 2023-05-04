package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpScriptService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpScriptService) GetTpScriptDetail(tp_script_id string) []models.TpScript {
	var tp_script []models.TpScript
	psql.Mydb.First(&tp_script, "id = ?", tp_script_id)
	return tp_script
}

// 获取列表
func (*TpScriptService) GetTpScriptList(PaginationValidate valid.TpScriptPaginationValidate, tenantId string) (bool, []models.TpScript, int64) {
	var TpScripts []models.TpScript
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpScript{})
	db.Where("tenant_id = ?", tenantId)
	if PaginationValidate.ProtocolType != "" {
		db.Where("protocol_type = ?", PaginationValidate.ProtocolType)
	}
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	if PaginationValidate.DeviceType != "" {
		db.Where("device_type = ?", PaginationValidate.DeviceType)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpScripts)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpScripts, 0
	}
	return true, TpScripts, count
}

// 新增数据
func (*TpScriptService) AddTpScript(tp_script models.TpScript) (models.TpScript, error) {
	var uuid = uuid.GetUuid()
	tp_script.Id = uuid
	result := psql.Mydb.Create(&tp_script)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_script, result.Error
	}
	return tp_script, nil
}

// 修改数据
func (*TpScriptService) EditTpScript(tp_script valid.TpScriptValidate) bool {
	result := psql.Mydb.Model(&models.TpScript{}).Where("id = ?", tp_script.Id).Updates(&tp_script)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpScriptService) DeleteTpScript(tp_script models.TpScript) error {
	result := psql.Mydb.Delete(&tp_script)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
}

// 调试脚本
func (*TpScriptService) QuizTpScript(code, msgcotent, topic string) (res string, err error) {
	var msg []byte
	if strings.HasPrefix(msgcotent, "0x") {
		msg, err = hex.DecodeString(strings.ReplaceAll(msgcotent, "0x", ""))
		if err != nil {
			return "", errors.New("报文存在错误")
		}
	} else {
		msg, err = json.Marshal(msgcotent)
		if err != nil {
			return "", errors.New("报文存在错误")
		}
	}
	res, err = utils.ScriptDeal(code, msg, topic)
	return

}
