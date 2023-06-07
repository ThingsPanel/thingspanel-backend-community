package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpVis struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

//获取列表
func (*TpVis) GetTpVisPluginList(PaginationValidate valid.TpVisPluginPaginationValidate, tenantId string) (bool, []map[string]interface{}, int64) {

	var visplugins []models.TpVisPlugin
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpVisPlugin{})
	db.Where("tenant_id = ?", tenantId)

	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at").Find(&visplugins)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, nil, 0
	}

	var data []map[string]interface{}
	for _, v := range visplugins {
		data = append(data, map[string]interface{}{
			"plugin_name": v.PluginName,
			"files":       []map[string]string{},
		})
		visfiles, err := GetTpVisFilesListByPluginId(v.Id)
		if err != nil {
			logs.Error(err.Error())
			continue
		}
		for _, f := range visfiles {
			data[len(data)-1]["files"] = append(data[len(data)-1]["files"].([]map[string]string), map[string]string{
				"file_name": f.FileName,
				"file_url":  f.FileUrl,
			})
		}
	}

	return true, data, count

}

func (*TpVis) UploadTpVisPlugin(plugin_name, tenantId string, files []map[string]string) bool {

	var visplugin models.TpVisPlugin
	visplugin.Id = utils.GetUuid()
	visplugin.PluginName = plugin_name
	visplugin.TenantId = tenantId
	visplugin.CreatedAt = time.Now().Unix()

	tx := psql.Mydb.Model(&models.TpVisPlugin{})
	err := tx.Create(&visplugin).Error
	if err != nil {
		logs.Error(err.Error())
		return false
	}

	for _, f := range files {
		var visfile models.TpVisFiles
		visfile.Id = utils.GetUuid()
		visfile.VisPluginId = visplugin.Id
		visfile.FileName = f["file_name"]
		visfile.FileUrl = f["file_url"]
		visfile.FileSize = f["file_size"]
		visfile.CreatedAt = time.Now().Unix()
		tx := psql.Mydb.Model(&models.TpVisFiles{})
		err := tx.Create(&visfile).Error
		if err != nil {
			logs.Error(err.Error())
			return false
		}
	}

	return true

}

//根据插件id获取文件列表
func GetTpVisFilesListByPluginId(vispluginid string) ([]models.TpVisFiles, error) {

	tx := psql.Mydb.Model(&models.TpVisFiles{})
	tx.Where("vis_plugin_id=?", vispluginid)

	var visfilesList []models.TpVisFiles
	err := tx.Find(&visfilesList).Error
	if err != nil {
		logs.Error(err.Error())
		return nil, err
	}
	return visfilesList, nil

}
