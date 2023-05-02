package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"fmt"
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
		tmpMap[value.RecipeID] = append(tmpMap[value.RecipeID], value)
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

func (TasteService) SearchTasteList() ([]*models.Taste, error) {
	taste := make([]*models.Taste, 0)
	result := psql.Mydb.Find(&taste)
	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, result.Error
		}
	}
	list1 := make(map[string]*models.Taste, 0)
	for _, v := range taste {
		list1[MD5(fmt.Sprintf("%s%s", v.Name, v.TasteId))] = v
	}
	list2 := make([]*models.Taste, 0)
	for _, v := range list1 {
		list2 = append(list2, v)
	}

	return list2, nil
}
