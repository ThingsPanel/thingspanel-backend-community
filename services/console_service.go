package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"time"
)

type ConsoleService struct {
}

func (*ConsoleService) AddConsole(name, createdBy, data, config, template, code, tenantId string) error {
	id := uuid.GetUuid()
	save := models.Console{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().Unix(),
		CreatedBy: createdBy,
		UpdateAt:  time.Now().Unix(),
		Data:      data,
		Config:    config,
		Template:  template,
		Code:      code,
		TenantId:  tenantId,
	}
	result := psql.Mydb.Create(&save)
	return result.Error
}

func (*ConsoleService) EditConsole(id, name, data, config, template, code string) error {

	update := make(map[string]interface{})

	update["update_at"] = time.Now().Unix()
	// 只修改传过来的字段
	if name != "" {
		update["name"] = name
	}

	if data != "" {
		update["data"] = data
	}

	if config != "" {
		update["config"] = config
	}

	if template != "" {
		update["template"] = template
	}

	if code != "" {
		update["code"] = code
	}

	err := psql.Mydb.Model(&models.Console{}).Where("id = ?", id).Updates(update).Error
	return err
}

func (*ConsoleService) DeleteConsoleById(id string) error {
	err := psql.Mydb.Where("id = ?", id).Delete(&models.Console{}).Error
	return err
}

func (*ConsoleService) GetConsoleList(name string) ([]models.Console, error) {
	var data []models.Console
	searchTerm := "%" + name + "%"
	err := psql.Mydb.Where("name LIKE ?", searchTerm).Find(&data).Error
	return data, err

}

func (*ConsoleService) GetConsoleDetail(id string) (models.Console, error) {
	var data models.Console
	err := psql.Mydb.Select("name", "created_at", "created_by", "code").First(&data, "id = ?", id).Error
	return data, err
}
