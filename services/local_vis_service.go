package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpLocalVis struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpLocalVis) GetTpLocalVisPluginDetail(tp_local_vis_plugin_id string) []models.TpLocalVisPlugin {
	var tplocalvisplugin []models.TpLocalVisPlugin
	psql.Mydb.First(&tplocalvisplugin, "id = ?", tp_local_vis_plugin_id)
	return tplocalvisplugin
}

// 获取列表
func (*TpLocalVis) GetTpLocalVisPluginList(PaginationValidate valid.TpLocalVisPluginPaginationValidate) (bool, []models.TpLocalVisPlugin, int64) {
	var TpLocalVisPlugins []models.TpLocalVisPlugin
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpLocalVisPlugin{})
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("create_at asc").Find(&TpLocalVisPlugins)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpLocalVisPlugins, 0
	}
	return true, TpLocalVisPlugins, count
}

// 新增数据
func (*TpLocalVis) AddTpLocalVisPlugin(TpLocalVisPlugin valid.AddTpLocalVisPluginValidate, tenantId string) (models.TpLocalVisPlugin, error) {
	var TpLocalVisPluginModel models.TpLocalVisPlugin = models.TpLocalVisPlugin{
		Id:        TpLocalVisPlugin.Id,
		TenantId:  tenantId,
		PluginUrl: TpLocalVisPlugin.PluginUrl,
		CreateAt:  time.Now().Unix(),
		Remark:    TpLocalVisPlugin.Remark,
	}
	result := psql.Mydb.Create(&TpLocalVisPluginModel)
	//不返回tenantId
	TpLocalVisPluginModel.TenantId = ""
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return TpLocalVisPluginModel, result.Error
	}
	return TpLocalVisPluginModel, nil
}

// 修改数据
func (*TpLocalVis) EditTpLocalVisPlugin(TpLocalVisPlugin valid.EditTpLocalVisPluginValidate) error {
	// 验证id是否存在
	var TpLocalVisPluginModel models.TpLocalVisPlugin
	result := psql.Mydb.First(&TpLocalVisPluginModel, "id = ?", TpLocalVisPlugin.Id)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return result.Error
	}
	// 将需要修改的数据组合到结构体中
	TpLocalVisPluginModel = models.TpLocalVisPlugin{
		Id:        TpLocalVisPlugin.Id,
		PluginUrl: TpLocalVisPlugin.PluginUrl,
	}
	result = psql.Mydb.Model(&models.TpLocalVisPlugin{}).Where("id = ?", TpLocalVisPlugin.Id).Updates(&TpLocalVisPluginModel)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return result.Error
	}
	return nil
}

// 删除数据
func (*TpLocalVis) DeleteTpLocalVisPlugin(TpLocalVisPlugin models.TpLocalVisPlugin) error {
	result := psql.Mydb.Delete(&TpLocalVisPlugin)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
}
