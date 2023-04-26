package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"errors"
	"gorm.io/gorm"
)

type MaterialService struct {
}

func (*MaterialService) GetMaterialList(id []string) (map[string][]*models.Materials, error) {
	var materials []*models.Materials
	result := psql.Mydb.Where("recipe_id in (?)", id).Find(&materials)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, result.Error
		}
	}
	tmpMap := make(map[string][]*models.Materials)
	for _, value := range materials {
		tmpMap[value.RecipeID] = append(tmpMap[value.RecipeID], value)
	}

	return tmpMap, nil
}

func (*MaterialService) DeleteMaterial(id string) error {
	var materials models.Materials
	err := psql.Mydb.Where("id = ?", id).Delete(&materials).Error
	if err != nil {
		return err
	}
	return nil
}

func (*MaterialService) GetMaterialByName(name string) bool {
	var materials models.Materials
	err := psql.Mydb.Where("name = ?", name).First(&materials).Error
	if err != nil {
		return false
	}
	return true
}
