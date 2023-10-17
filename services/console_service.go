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

func (*ConsoleService) EditConsole() int {
	return 0
}

func (*ConsoleService) DeleteConsoleById() int {
	return 0
}

func (*ConsoleService) GetConsoleList() int {
	return 0
}

func (*ConsoleService) GetConsoleDetail() int {
	return 0
}
