package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/modules/dataService/mqtt"
	valid "ThingsPanel-Go/validate"
	"crypto/md5"
	"encoding/hex"
	"errors"
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
		db = db.Where("recipe.id = ?", PaginationValidate.Id)
	}
	db = db.Select("recipe.id,recipe.bottom_pot_id,recipe.bottom_pot,recipe.pot_type_id,recipe.materials,recipe.taste,recipe.bottom_properties,recipe.soup_standard,pot_type.name").Joins("left join pot_type on recipe.pot_type_id = pot_type.pot_type_id").Where("recipe.is_del", false)

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
func (*RecipeService) AddRecipe(pot models.Recipe, list1 []models.Materials, list2 []*models.Taste) (error, models.Recipe) {

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
func (*RecipeService) EditRecipe(pot valid.EditRecipeValidator, list1 []models.Materials, list2 []*models.Taste, list3 []string, list4 []string, list5 []models.OriginalMaterials, list6 []models.OriginalTaste) error {

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

	err := psql.Mydb.Transaction(func(tx *gorm.DB) error {

		err := tx.Model(models.Recipe{}).Where("id = ?", pot.Id).Updates(updates).Error
		if err != nil {
			fmt.Println(err)
			return err
		}
		if len(list1) > 0 {
			if err = tx.Create(&list1).Error; err != nil {
				fmt.Println(err)
				return err
			}
		}
		if len(list2) > 0 {
			if err = tx.Create(&list2).Error; err != nil {
				fmt.Println(err)
				return err
			}
		}

		if len(list3) > 0 {

			var taste models.Taste
			if err = tx.Where("id in (?)", list3).Delete(&taste).Error; err != nil {
				return err
			}

			//RecipeTaste := make([]*models.Taste, 0)
			//err = tx.Where("recipe_id = ?", pot.Id).Find(&RecipeTaste).Error
			//if err != nil {
			//	if !errors.Is(err, gorm.ErrRecordNotFound) {
			//		return err
			//	}
			//}
			//tasteIdArr := make([]string, 0)
			//for _, v := range RecipeTaste {
			//	tasteIdArr = append(tasteIdArr, v.OriginalTasteId)
			//}
			//
			//otherRecipeTasteArr := make([]*models.Taste, 0)
			//err = tx.Where("recipe_id <> ?", pot.Id).Where("original_taste_id in (?)", tasteIdArr).Find(&otherRecipeTasteArr).Error
			//if err != nil {
			//	if errors.Is(err, gorm.ErrRecordNotFound) {
			//		var tastes models.OriginalTaste
			//		if err = tx.Where("id in (?)", tasteIdArr).Delete(&tastes).Error; err != nil {
			//			return err
			//		}
			//	} else {
			//		return err
			//	}
			//}
			//else {
			//	existRecipeTasteArr := make([]string, 0)
			//	for _, v := range otherRecipeTasteArr {
			//		existRecipeTasteArr = append(existRecipeTasteArr, v.OriginalTasteId)
			//	}
			//
			//	diffArr := FindDiff(existRecipeTasteArr, tasteIdArr)
			//	var tastes models.OriginalTaste
			//	if err = tx.Where("id in (?)", diffArr).Delete(&tastes).Error; err != nil {
			//		return err
			//	}
			//}

		}

		if len(list4) > 0 {
			var material models.Materials
			if err = tx.Where("id in (?)", list4).Delete(&material).Error; err != nil {
				fmt.Println(err)
				return err
			}
			//
			//RecipeMaterial := make([]*models.Materials, 0)
			//err = tx.Where("recipe_id = ?", pot.Id).Find(&RecipeMaterial).Error
			//if err != nil {
			//	if !errors.Is(err, gorm.ErrRecordNotFound) {
			//		return err
			//	}
			//}
			//materialIdArr := make([]string, 0)
			//for _, v := range RecipeMaterial {
			//	materialIdArr = append(materialIdArr, v.OriginalMaterialId)
			//}
			//
			//otherRecipeMaterialArr := make([]*models.Materials, 0)
			//err = tx.Where("recipe_id <> ?", pot.Id).Where("original_material_id in (?)", materialIdArr).Find(&otherRecipeMaterialArr).Error
			//if err != nil {
			//	if errors.Is(err, gorm.ErrRecordNotFound) {
			//		var taste models.OriginalMaterials
			//		if err = tx.Where("id in (?)", materialIdArr).Delete(&taste).Error; err != nil {
			//			return err
			//		}
			//	} else {
			//		return err
			//	}
			//} else {
			//	existRecipeMaterialArr := make([]string, 0)
			//	for _, v := range otherRecipeMaterialArr {
			//		existRecipeMaterialArr = append(existRecipeMaterialArr, v.OriginalMaterialId)
			//	}
			//
			//	diffArr := FindDiff(existRecipeMaterialArr, materialIdArr)
			//	var materials models.OriginalMaterials
			//	if err = tx.Where("id in (?)", diffArr).Delete(&materials).Error; err != nil {
			//		return err
			//	}
			//}

		}

		if len(list5) > 0 {
			if err = tx.Create(&list5).Error; err != nil {
				fmt.Println(err)
				return err
			}
		}

		if len(list6) > 0 {
			if err = tx.Create(&list6).Error; err != nil {
				fmt.Println(err)
				return err
			}
		}

		return nil
	})
	fmt.Println(err)
	return err

}

// 删除数据
func (*RecipeService) DeleteRecipe(pot models.Recipe) error {
	return psql.Mydb.Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&models.Recipe{}).Where("id = ?", pot.Id).UpdateColumns(map[string]interface{}{"is_del": true, "delete_at": time.Now()}).Error
		if err != nil {
			return err
		}
		//查询其他配方是否含有此物料、没有则删除原始物料
		//list := make([]*models.Materials, 0)
		//err = tx.Where("recipe_id = ?", pot.Id).Find(&list).Error
		//if err != nil {
		//	return err
		//}
		//originalMaterialId := make([]string, 0)
		//for _, v := range list {
		//	originalMaterialId = append(originalMaterialId, v.OriginalMaterialId)
		//}
		//otherRecipeMaterial := make([]*models.Materials, 0)
		//err = tx.Where("recipe_id <> ?", pot.Id).Where("original_material_id in (?)", originalMaterialId).Find(&otherRecipeMaterial).Error

		//if err != nil {
		//	fmt.Println("=====" + err.Error())
		//	if errors.Is(err, gorm.ErrRecordNotFound) {
		//		var originalMaterial models.OriginalMaterials
		//		err = tx.Where("id in (?)", originalMaterialId).Delete(&originalMaterial).Error
		//		if err != nil {
		//			return err
		//		}
		//	} else {
		//		return err
		//	}
		//} else {
		//	otherRecipeExitMaterialIdArr := make([]string, 0)
		//	for _, v := range otherRecipeMaterial {
		//		otherRecipeExitMaterialIdArr = append(otherRecipeExitMaterialIdArr, v.OriginalMaterialId)
		//	}
		//	diff := FindDiff(originalMaterialId, otherRecipeExitMaterialIdArr)
		//	if len(diff) > 0 {
		//		var originalMaterial models.OriginalMaterials
		//		err = tx.Where("id in(?)", diff).Delete(&originalMaterial).Error
		//		if err != nil {
		//			return err
		//		}
		//	}
		//}

		var material models.Materials
		err = tx.Where("recipe_id = ?", pot.Id).Delete(&material).Error
		if err != nil {
			return err
		}

		//查询其他配方是否含有此口味、没有则删除原始口味
		//tasteList := make([]*models.Taste, 0)
		//err = tx.Where("recipe_id = ?", pot.Id).Find(&tasteList).Error
		//if err != nil {
		//	return err
		//}
		//originalTasteId := make([]string, 0)
		//for _, v := range tasteList {
		//	originalTasteId = append(originalTasteId, v.OriginalTasteId)
		//}
		//
		//otherRecipeTaste := make([]*models.Taste, 0)
		//err = tx.Where("recipe_id <> ?", pot.Id).Where("original_taste_id in(?)", originalTasteId).Find(&otherRecipeTaste).Error
		//if err != nil {
		//	if errors.Is(err, gorm.ErrRecordNotFound) {
		//		var originalTaste models.OriginalTaste
		//		err = tx.Where("id in (?)", originalTasteId).Delete(&originalTaste).Error
		//		if err != nil {
		//			return err
		//		}
		//	} else {
		//		return err
		//	}
		//} else {
		//	otherRecipeExitTasteIdArr := make([]string, 0)
		//	for _, v := range otherRecipeTaste {
		//		otherRecipeExitTasteIdArr = append(otherRecipeExitTasteIdArr, v.OriginalTasteId)
		//	}
		//	diff := FindDiff(originalTasteId, otherRecipeExitTasteIdArr)
		//	if len(diff) > 0 {
		//		var originalTaste models.OriginalTaste
		//		err = tx.Where("id in(?)", diff).Delete(&originalTaste).Error
		//		if err != nil {
		//			return err
		//		}
		//	}
		//}

		var taste models.Taste
		err = tx.Where("recipe_id = ?", pot.Id).Delete(&taste).Error
		if err != nil {
			return err
		}
		return nil
	})

}

func (*RecipeService) GetSendToMQTTData(assetId string) (*mqtt.SendConfig, error) {
	var Asset models.Asset
	err2 := psql.Mydb.Where("id = ?", assetId).First(&Asset).Error
	if err2 != nil {
		return nil, err2
	}

	tmpSendConfig := &mqtt.SendConfig{
		Shop: mqtt.ShopContent{
			Name:   Asset.Name,
			Number: Asset.ID,
		},
		PotType:   make([]*mqtt.PotType, 0),
		Taste:     make([]*mqtt.Taste, 0),
		Materials: make([]*mqtt.Materials, 0),
		Recipe:    make([]*mqtt.Recipe, 0),
	}
	var Recipe []*models.Recipe
	err := psql.Mydb.Where("is_del = ?", false).Where("asset_id = ?", Asset.ID).Find(&Recipe).Error
	if err != nil {
		return nil, err
	}

	if len(Recipe) == 0 {
		return nil,errors.New("该店铺下不存在配方")
	}
	for _, v := range Recipe {
		tmpSendConfig.Recipe = append(tmpSendConfig.Recipe, &mqtt.Recipe{
			BottomPotId: v.BottomPotId,
			BottomPot:   v.BottomPot,
			//PotTypeId:        v.PotTypeId,
			BottomProperties: v.BottomProperties,
			//SoupStandard:     v.SoupStandard,
		})
	}

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
	for _, v := range potTypeList {
		tmpSendConfig.PotType = append(tmpSendConfig.PotType, &mqtt.PotType{
			Name:         v.Name,
			SoupStandard: v.SoupStandard,
			PotTypeId:    v.PotTypeId,
		})
	}
	materialList := make([]*models.Materials, 0)
	err = psql.Mydb.Where("recipe_id in (?)", recipeIdArr).Find(&materialList).Error
	if err != nil {
		return nil, err
	}

	tmpMaterialMap := make(map[string]*mqtt.Materials)
	materialIdList := make(map[string][]string, 0)
	for _, v := range materialList {
		if value, ok := tmpMaterialMap[fmt.Sprintf("%s%d%s%d", v.Name, v.Dosage, v.Unit, v.WaterLine)]; ok {
			materialIdList[v.RecipeID] = append(materialIdList[v.RecipeID], value.Id)
		} else {
			materialIdList[v.RecipeID] = append(materialIdList[v.RecipeID], v.Id)
			tmpMaterialMap[fmt.Sprintf("%s%d%s%d", v.Name, v.Dosage, v.Unit, v.WaterLine)] = &mqtt.Materials{
				Id:        v.Id,
				Name:      v.Name,
				Dosage:    v.Dosage,
				Unit:      v.Unit,
				WaterLine: v.WaterLine,
				Station:   v.Station,
			}
		}

	}

	for _, v := range tmpMaterialMap {
		tmpSendConfig.Materials = append(tmpSendConfig.Materials, v)
	}

	tasteList := make([]*models.Taste, 0)

	err = psql.Mydb.Where("recipe_id in (?)", recipeIdArr).Where("is_del", false).Find(&tasteList).Error
	if err != nil {
		return nil, err
	}
	tmpTasteMap := make(map[string]*mqtt.Taste)
	for _, v := range tasteList {
		tmpTasteMap[v.TasteId] = &mqtt.Taste{
			Name:      v.Name,
			TasteId:   v.TasteId,
			Material:  v.Material,
			Dosage:    v.Dosage,
			Unit:      v.Unit,
			WaterLine: v.WaterLine,
			Station:   v.Station,
		}
	}

	for _, v := range tmpTasteMap {
		tmpSendConfig.Taste = append(tmpSendConfig.Taste, v)
	}

	for key, value := range Recipe {
		tmpSendConfig.Recipe[key].MaterialIdList = materialIdList[value.Id]
	}

	return tmpSendConfig, nil
}

func (*RecipeService) FindMaterialByName() ([]*models.Materials, error) {
	list := make([]*models.Materials, 0)
	err := psql.Mydb.Find(&list).Error
	if err != nil {
		return nil, err
	}
	list1 := make(map[string]*models.Materials, 0)
	for _, v := range list {
		list1[MD5(fmt.Sprintf("%s%d%s%d", v.Name, v.Dosage, v.Unit, v.WaterLine))] = v
	}
	list2 := make([]*models.Materials, 0)
	for _, v := range list1 {
		list2 = append(list2, v)
	}

	return list2, nil
}

func (*RecipeService) FindTasteMaterialList() ([]*models.Taste, error) {
	list := make([]*models.Taste, 0)
	err := psql.Mydb.Find(&list).Error
	if err != nil {
		return nil, err
	}
	list1 := make(map[string]*models.Taste, 0)
	for _, v := range list {
		list1[MD5(fmt.Sprintf("%s%d%s%d", v.Material, v.Dosage, v.Unit, v.WaterLine))] = v
	}
	list2 := make([]*models.Taste, 0)
	for _, v := range list1 {
		list2 = append(list2, v)
	}

	return list2, nil
}

func MD5(data string) string {
	h := md5.New()
	h.Write([]byte(data)) // 需要加密的字符串为 123456
	cipherStr := h.Sum(nil)
	return hex.EncodeToString(cipherStr) // 输出加密结果
}

func (*RecipeService) CreateMaterial(material *models.OriginalMaterials) (bool, error) {
	var createModel models.OriginalMaterials
	err := psql.Mydb.Where("name = ?", material.Name).First(&createModel).Error
	if err != nil {
		if strings.Contains(err.Error(), "record not found") {
			if psql.Mydb.Create(&material).Error != nil {
				return false, err
			}
			return false, nil
		}

	}
	return true, nil
}

func (*RecipeService) CreateTaste(taste *models.OriginalTaste, action string) error {
	var createModel models.OriginalTaste
	if action == "CHECK" {
		err := psql.Mydb.Where("name = ?", taste.Name).First(&createModel).Error
		if err != nil {
			fmt.Println(strings.Contains(err.Error(), "record not found"))
			if strings.Contains(err.Error(), "record not found") {
				return nil
			}
			return err
		}
	} else {
		if err := psql.Mydb.Create(&taste).Error; err != nil {
			return err
		}
		return nil
	}

	return nil
}

func (*RecipeService) CheckBottomIdIsRepeat(bottomId, recipeId string, action string) (bool, error) {
	var model models.Recipe
	var err error
	if action == "ADD" {
		err = psql.Mydb.Where("bottom_pot_id = ?", bottomId).Where("is_del", false).First(&model).Error
	} else {
		err = psql.Mydb.Where("bottom_pot_id = ?", bottomId).Where("id <> ?", recipeId).Where("is_del", false).First(&model).Error
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil

}

func (*RecipeService) CheckPosTasteIdIsRepeat(list5 string, action string) (bool, error) {
	if len(list5) > 0 {
		list := make([]*models.Taste, 0)
		if action == "GET" {
			return false, nil
		}
		err := psql.Mydb.Where("taste_id = ?", list5).First(&list).Error
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return false, err
			}
		}
		if len(list) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func FindDiff(arr1, arr2 []string) []string {

	diff := make([]string, 0)

	for _, str1 := range arr1 {
		found := false
		for _, str2 := range arr2 {
			if str1 == str2 {
				found = true
				break
			}
		}
		if !found {
			diff = append(diff, str1)
		}
	}

	for _, str2 := range arr2 {
		found := false
		for _, str1 := range arr1 {
			if str2 == str1 {
				found = true
				break
			}
		}
		if !found {
			diff = append(diff, str2)
		}
	}

	return diff
}
