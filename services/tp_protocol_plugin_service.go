package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpProtocolPluginService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpProtocolPluginService) GetTpProtocolPluginDetail(tp_protocol_plugin_id string) []models.TpProtocolPlugin {
	var tp_protocol_plugin []models.TpProtocolPlugin
	psql.Mydb.First(&tp_protocol_plugin, "id = ?", tp_protocol_plugin_id)
	return tp_protocol_plugin
}

// 获取列表
func (*TpProtocolPluginService) GetTpProtocolPluginList(PaginationValidate valid.TpProtocolPluginPaginationValidate) (bool, []models.TpProtocolPlugin, int64) {
	var TpProtocolPlugins []models.TpProtocolPlugin
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"
	if PaginationValidate.ProtocolType != "" {
		sqlWhere += " and protocol_type = '" + PaginationValidate.ProtocolType + "'"
	}
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = '" + PaginationValidate.Id + "'"
	}
	var count int64
	psql.Mydb.Model(&models.TpProtocolPlugin{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.TpProtocolPlugin{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpProtocolPlugins)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpProtocolPlugins, 0
	}
	return true, TpProtocolPlugins, count
}

// 新增数据
func (*TpProtocolPluginService) AddTpProtocolPlugin(tp_protocol_plugin models.TpProtocolPlugin) (models.TpProtocolPlugin, error) {
	var uuid = uuid.GetUuid()
	tp_protocol_plugin.Id = uuid
	result := psql.Mydb.Create(&tp_protocol_plugin)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_protocol_plugin, result.Error
	}
	var TpDictService TpDictService
	var TpDict = models.TpDict{
		DictCode:  "GATEWAY_PROTOCOL",
		DictValue: tp_protocol_plugin.ProtocolType,
		Describe:  tp_protocol_plugin.Name,
		CreatedAt: tp_protocol_plugin.CreatedAt,
	}
	TpDictService.AddTpDict(TpDict)
	return tp_protocol_plugin, nil
}

// 修改数据
func (*TpProtocolPluginService) EditTpProtocolPlugin(tp_protocol_plugin valid.TpProtocolPluginValidate) bool {
	result := psql.Mydb.Model(&models.TpProtocolPlugin{}).Where("id = ?", tp_protocol_plugin.Id).Updates(&tp_protocol_plugin)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpProtocolPluginService) DeleteTpProtocolPlugin(tp_protocol_plugin models.TpProtocolPlugin) error {
	var TpProtocolPluginService TpProtocolPluginService
	TpProtocolPlugin := TpProtocolPluginService.GetTpProtocolPluginDetail(tp_protocol_plugin.Id)
	result := psql.Mydb.Delete(&tp_protocol_plugin)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	var TpDictService TpDictService
	var TpDict = models.TpDict{
		DictCode:  "GATEWAY_PROTOCOL",
		DictValue: TpProtocolPlugin[0].ProtocolType,
	}
	TpDictService.DeleteRowTpDict(TpDict)
	return nil
}
