package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type TasteService struct {
}

func (TasteService) GetTasteList(recipeId []string) (map[string][]*models.Taste, error) {
	var materials []*models.Taste
	result := psql.Mydb.Where("recipe_id in (?)", recipeId).Find(&materials)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, result.Error
		}
	}
	tmpMap := make(map[string][]*models.Taste)
	for _, value := range materials {
		tmpMap[value.RecipeID] = append(tmpMap[value.Id], value)
	}

	return tmpMap, nil
}

func (TasteService) DeleteTaste(id string) error {
	var taste models.Taste
	err := psql.Mydb.Where("id  = ?", id).Delete(&taste).Error
	if err != nil {
		return err
	}
	return nil
}

func (TasteService) SearchTasteList(keyword string) ([]*models.OriginalTaste, error) {
	taste := make([]*models.OriginalTaste, 0)
	db := psql.Mydb
	if keyword != "" {
		db = db.Where("name = ?", keyword)
	}
	result := db.Find(&taste)
	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, result.Error
		}
	}

	return taste, nil
}



