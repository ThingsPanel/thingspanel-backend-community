package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/modules/dataService/mqtt"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
	"strings"
	"time"
)

type RecipeService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*RecipeService) GetRecipeDetail(recipeId string) []models.Recipe {
	var recipe []models.Recipe
	psql.Mydb.First(&recipe, "recipe.id = ?", recipeId)
	return recipe
}

// 获取列表
func (*RecipeService) GetRecipeList(PaginationValidate valid.RecipePaginationValidate) (bool, []models.RecipeValue, int64) {
	var Recipe []models.RecipeValue
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.Recipe{})
	if PaginationValidate.Id != "" {
		db = db.Where("id = ?", PaginationValidate.Id)
	}
	db = db.Select("recipe.id,recipe.bottom_pot_id,recipe.bottom_pot,recipe.pot_type_id,recipe.materials,recipe.taste,recipe.bottom_properties,recipe.soup_standard,recipe.current_water_line,pot_type.name").Joins("left join pot_type on recipe.pot_type_id = pot_type.pot_type_id").Where("recipe.is_del", false)

	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("recipe.create_at desc").Find(&Recipe)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, Recipe, 0
	}
	return true, Recipe, count
}

// 新增数据
func (*RecipeService) AddRecipe(pot models.Recipe, list1 []models.Materials, list2 []models.Taste) (error, models.Recipe) {

	err := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		result := tx.Create(&pot)
		if result.Error != nil {
			logs.Error(result.Error, gorm.ErrRecordNotFound)
			return result.Error
		}
		if err := tx.Create(list1).Error; err != nil {
			logs.Error(err)
			return err
		}
		if err := tx.Create(list2).Error; err != nil {
			logs.Error(err)
			return err
		}
		return nil

	})
	if err != nil {
		logs.Error(err)
		return err, pot
	}

	return nil, pot
}

// 修改数据
func (*RecipeService) EditRecipe(pot valid.EditRecipeValidator, list1 []models.Materials, list2 []models.Taste) error {

	updates := &models.EditRecipeValue{
		BottomPotId:      pot.BottomPotId,
		BottomPot:        pot.BottomPot,
		PotTypeId:        pot.PotTypeId,
		Materials:        strings.Join(pot.Materials, ","),
		Taste:            strings.Join(pot.Tastes, ","),
		BottomProperties: pot.BottomProperties,
		SoupStandard:     pot.SoupStandard,
		UpdateAt:         time.Now(),
	}
	by, _ := json.Marshal(updates)
	fmt.Println(string(by))
	err := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		err := tx.Model(models.Recipe{}).Where("id = ?", pot.Id).Updates(updates).Error
		if err != nil {
			return err
		}
		if len(list1) > 0 {
			if err := tx.Create(&list1).Error; err != nil {
				return err
			}
		}
		if len(list2) > 0 {
			if err := tx.Create(&list2).Error; err != nil {
				return err
			}
		}
		return nil
	})

	return err

}

// 删除数据
func (*RecipeService) DeleteRecipe(pot models.Recipe) error {
	result := psql.Mydb.Model(&models.Recipe{}).Where("id = ?", pot.Id).UpdateColumns(map[string]interface{}{"is_del": true, "delete_at": time.Now()})
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}

func (*RecipeService) GetSendToMQTTData(assetId string) ([]*mqtt.SendConfig, error) {
	AssetArr := make([]*models.Asset, 0)
	err2 := psql.Mydb.Where("id = ?", assetId).Find(&AssetArr).Error
	if err2 != nil {
		return nil, err2
	}
	list := make([]*mqtt.SendConfig, 0)
	for _, v := range AssetArr {
		tmpSendConfig := &mqtt.SendConfig{
			Shop: mqtt.ShopContent{
				Name:   v.Name,
				Number: v.ID,
			},
			PotType:   make([]*models.PotType, 0),
			Taste:     make([]*models.Taste, 0),
			Materials: make([]*models.Materials, 0),
			Recipe:    make([]*models.Recipe, 0),
		}
		var Recipe []*models.Recipe
		err := psql.Mydb.Where("is_del = ?", false).Where("asset_id = ?", v.ID).Find(&Recipe).Error
		if err != nil {
			return nil, err
		}
		tmpSendConfig.Recipe = Recipe
		recipeIdArr := make([]string, 0)
		potTypeArr := make([]string, 0)
		for _, v := range Recipe {
			recipeIdArr = append(recipeIdArr, v.Id)
			potTypeArr = append(potTypeArr, v.PotTypeId)
		}
		potTypeList := make([]*models.PotType, 0)
		err = psql.Mydb.Where("pot_type_id in (?)", potTypeArr).Find(&potTypeList).Error
		if err != nil {
			return nil, err
		}
		tmpSendConfig.PotType = potTypeList
		materialList := make([]*models.Materials, 0)
		err = psql.Mydb.Where("recipe_id in (?)", recipeIdArr).Find(&materialList).Error
		if err != nil {
			return nil, err
		}
		tmpSendConfig.Materials = materialList
		tasteList := make([]*models.Taste, 0)
		err = psql.Mydb.Where("recipe_id in (?)", recipeIdArr).Where("is_del", false).Find(&tasteList).Error
		if err != nil {
			return nil, err
		}
		tmpSendConfig.Taste = tasteList
		list = append(list, tmpSendConfig)

	}

	return list, nil
}

func (*RecipeService) FindMaterialByName(keyword string) ([]*models.Materials, error) {
	list := make([]*models.Materials, 0)
	db := psql.Mydb
	if keyword != "" {
		db = db.Where("name  like ?", "%"+keyword+"%")
	}
	err := db.Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}
