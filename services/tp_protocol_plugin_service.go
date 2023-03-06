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

func (*TpProtocolPluginService) GetByProtocolType(protocol_type string, device_type string) models.TpProtocolPlugin {
	var tp_protocol_plugin models.TpProtocolPlugin
	psql.Mydb.First(&tp_protocol_plugin, "protocol_type = ? and device_type = ?", protocol_type, device_type)
	return tp_protocol_plugin

}

// 获取列表
func (*TpProtocolPluginService) GetTpProtocolPluginList(PaginationValidate valid.TpProtocolPluginPaginationValidate) (bool, []models.TpProtocolPlugin, int64) {
	var TpProtocolPlugins []models.TpProtocolPlugin
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpProtocolPlugin{})
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
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpProtocolPlugins)
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
	var dict_code string
	if tp_protocol_plugin.DeviceType == "1" { //直连设备
		dict_code = "DRIECT_ATTACHED_PROTOCOL"
	} else {
		dict_code = "GATEWAY_PROTOCOL"
	}
	var TpDict = models.TpDict{
		DictCode:  dict_code,
		DictValue: tp_protocol_plugin.ProtocolType,
		Describe:  tp_protocol_plugin.Name,
		CreatedAt: tp_protocol_plugin.CreatedAt,
	}
	TpDictService.AddTpDict(TpDict)
	return tp_protocol_plugin, nil
}

// 修改数据
func (*TpProtocolPluginService) EditTpProtocolPlugin(tp_protocol_plugin valid.TpProtocolPluginValidate) error {
	result := psql.Mydb.Model(&models.TpProtocolPlugin{}).Where("id = ?", tp_protocol_plugin.Id).Updates(&tp_protocol_plugin)
	if result.Error != nil {
		logs.Error(result.Error.Error(), gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
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
	//删除字典中的协议插件
	var TpDictService TpDictService
	var dict_code = ""
	if TpProtocolPlugin[0].DeviceType == "1" {
		dict_code = "DRIECT_ATTACHED_PROTOCOL"
	} else if TpProtocolPlugin[0].DeviceType == "2" {
		dict_code = "GATEWAY_PROTOCOL"
	}
	var TpDict = models.TpDict{
		DictCode:  dict_code,
		DictValue: TpProtocolPlugin[0].ProtocolType,
	}
	err := TpDictService.DeleteRowTpDict(TpDict)
	if err != nil {
		logs.Error(err.Error())
	} else {
		logs.Info("协议插件对应的字典数据删除成功")
	}
	return nil
}
