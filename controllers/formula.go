package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/modules/dataService/mqtt"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"strings"
	"time"
)

type RecipeController struct {
	beego.Controller
}

func (pot *RecipeController) Index() {
	PaginationValidate := valid.RecipePaginationValidate{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &PaginationValidate)
	if err != nil {
		fmt.Println("参数解析Delete失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(PaginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(PaginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}
	var RecipeService services.RecipeService
	isSuccess, d, t := RecipeService.GetRecipeList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(pot.Ctx))
		return
	}

	recipeIdArr := make([]string, 0)
	for _, value := range d {
		recipeIdArr = append(recipeIdArr, value.Id)
	}

	var materialService services.MaterialService
	list, err := materialService.GetMaterialList(recipeIdArr, "material")
	if err != nil {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(pot.Ctx))
		return
	}

	tasteMaterialList, err := materialService.GetMaterialList(recipeIdArr, "taste")
	if err != nil {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(pot.Ctx))
		return
	}

	var tasteService services.TasteService

	tasteList, err := tasteService.GetTasteList(recipeIdArr)
	if err != nil {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(pot.Ctx))
		return
	}

	for key, value := range d {
		d[key].MaterialArr = list[value.Id]
		d[key].TasteArr = tasteList[value.Id]
		for _, vs := range d[key].TasteArr {
			vs.TasteMaterialArr, _ = materialService.GetMaterialListByMaterialID(recipeIdArr, strings.Split(vs.MaterialIdList, ","), "taste")
		}
		d[key].TasteMaterialArr = tasteMaterialList[value.Id]
		d[key].Materials = strings.ReplaceAll(d[key].Materials, ",", "\n")
		d[key].Taste = strings.ReplaceAll(d[key].Taste, ",", "\n")
	}

	dd := valid.RspRecipePaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(pot.Ctx))

}

/**创建*/
func (pot *RecipeController) Add() {
	AssetId := "10000"
	addRecipeValidate := valid.AddRecipeValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &addRecipeValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addRecipeValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(addRecipeValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}

	var RecipeService services.RecipeService

	isExit, err := RecipeService.CheckBottomIdIsRepeat(addRecipeValidate.BottomPotId, "", "ADD", addRecipeValidate.PotTypeId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}
	if isExit {
		response.SuccessWithMessage(400, "同一店铺下，锅底不能重复", (*context2.Context)(pot.Ctx))
		return
	}

	id := uuid.GetUuid()
	Recipe := models.Recipe{
		Id:               id,
		BottomPotId:      addRecipeValidate.BottomPotId,
		BottomPot:        addRecipeValidate.BottomPot,
		PotTypeId:        addRecipeValidate.PotTypeId,
		Materials:        strings.Join(addRecipeValidate.Materials, ","),
		Taste:            strings.Join(addRecipeValidate.Tastes, ","),
		BottomProperties: addRecipeValidate.BottomProperties,
		SoupStandard:     addRecipeValidate.SoupStandard,
		CreateAt:         time.Now().Unix(),
		AssetId:          AssetId,
		TasteMaterials:   strings.Join(addRecipeValidate.TasteMaterials, ","),
	}

	MaterialArr := make([]models.Materials, 0)
	TasteArr := make([]*models.Taste, 0)
	var recipeId = uuid.GetUuid()
	Recipe.Id = recipeId
	for _, v := range addRecipeValidate.MaterialsArr {

		materialUuid := uuid.GetUuid()
		MaterialArr = append(MaterialArr, models.Materials{
			Id:        materialUuid,
			Name:      v.Name,
			Dosage:    v.Dosage,
			Unit:      v.Unit,
			WaterLine: v.WaterLine,
			Station:   v.Station,
			RecipeID:  recipeId,
			PotTypeId: v.PotTypeId,
			Resource:  v.Resource,
		})

	}

	for _, v := range addRecipeValidate.TastesArr {
		tasteUuid := uuid.GetUuid()
		arr := make([]string, 0)
		for _, vs := range v.TasteMaterialArr {
			materialUuid := uuid.GetUuid()
			arr = append(arr, materialUuid)
			MaterialArr = append(MaterialArr, models.Materials{
				Id:        materialUuid,
				Name:      vs.Name,
				Dosage:    vs.Dosage,
				Unit:      vs.Unit,
				WaterLine: vs.WaterLine,
				Station:   vs.Station,
				RecipeID:  recipeId,
				PotTypeId: vs.PotTypeId,
				Resource:  vs.Resource,
			})

		}

		taste := &models.Taste{
			Id:             tasteUuid,
			Name:           v.Taste,
			TasteId:        v.TasteId,
			CreateAt:       time.Now().Unix(),
			RecipeID:       recipeId,
			PotTypeId:      v.PotTypeId,
			MaterialIdList: strings.Join(arr, ","),
		}
		switch v.Action {
		case "GET":
			TasteArr = append(TasteArr, taste)
			break
		default:

			TasteArr = append(TasteArr, taste)
		}

	}

	rsp_err, d := RecipeService.AddRecipe(Recipe, MaterialArr, TasteArr)
	if rsp_err == nil {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(pot.Ctx))
	} else {
		var err string
		err = rsp_err.Error()
		response.SuccessWithMessage(400, err, (*context2.Context)(pot.Ctx))
	}
	response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
}

// 编辑
func (pot *RecipeController) Edit() {
	RecipeValidate := valid.EditRecipeValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &RecipeValidate)
	if err != nil {
		response.SuccessWithMessage(1000, "参数解析失败", (*context2.Context)(pot.Ctx))
		return
	}
	v := validation.Validation{}
	status, _ := v.Valid(RecipeValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(RecipeValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}
	if RecipeValidate.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(pot.Ctx))
	}
	var Recipe services.RecipeService

	isExit, err := Recipe.CheckBottomIdIsRepeat(RecipeValidate.BottomPotId, RecipeValidate.Id, "EDIT", RecipeValidate.PotTypeId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}
	if isExit {
		response.SuccessWithMessage(400, "同一店铺下，锅底不能重复", (*context2.Context)(pot.Ctx))
		return
	}

	MaterialArr := make([]models.Materials, 0)
	DeleteMaterialArr := make([]string, 0)
	TasteArr := make([]*models.Taste, 0)
	DeleteTasteArr := make([]string, 0)

	for _, v := range RecipeValidate.MaterialsArr {
		materialUuid := ""
		if v.Id == "" {
			materialUuid = uuid.GetUuid()
			MaterialArr = append(MaterialArr, models.Materials{
				Id:        materialUuid,
				Name:      v.Name,
				Dosage:    v.Dosage,
				Unit:      v.Unit,
				WaterLine: v.WaterLine,
				Station:   v.Station,
				RecipeID:  RecipeValidate.Id,
				PotTypeId: v.PotTypeId,
				Resource:  v.Resource,
			})

		}
		if v.Operate == "delete" {
			DeleteMaterialArr = append(DeleteMaterialArr, v.Id)
		}
	}

	for _, v := range RecipeValidate.TastesArr {
		arr := make([]string, 0)

		for _, vs := range v.TasteMaterialArr {
			materialUuid := uuid.GetUuid()
			if vs.Id == "" {
				arr = append(arr, materialUuid)
				MaterialArr = append(MaterialArr, models.Materials{
					Id:        materialUuid,
					Name:      vs.Name,
					Dosage:    vs.Dosage,
					Unit:      vs.Unit,
					WaterLine: vs.WaterLine,
					Station:   vs.Station,
					RecipeID:  RecipeValidate.Id,
					PotTypeId: vs.PotTypeId,
					Resource:  vs.Resource,
				})
			} else {
				arr = append(arr, vs.Id)
			}

			if vs.Operate == "delete" {
				DeleteTasteArr = append(DeleteTasteArr, vs.Id)
			}

		}

		if v.Id == "" {
			tasteUuid := uuid.GetUuid()
			TasteArr = append(TasteArr, &models.Taste{
				Id:             tasteUuid,
				Name:           v.Taste,
				TasteId:        v.TasteId,
				CreateAt:       time.Now().Unix(),
				RecipeID:       RecipeValidate.Id,
				PotTypeId:      v.PotTypeId,
				MaterialIdList: strings.Join(arr, ","),
			})

		}

		if v.Operate == "delete" {
			DeleteMaterialArr = append(DeleteMaterialArr, arr...)
			DeleteTasteArr = append(DeleteTasteArr, v.Id)
		}

	}

	isSucess := Recipe.EditRecipe(RecipeValidate, MaterialArr, TasteArr, DeleteTasteArr, DeleteMaterialArr)
	if isSucess == nil {
		d := Recipe.GetRecipeDetail(RecipeValidate.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(pot.Ctx))
	}
}

// 删除
func (pot *RecipeController) Delete() {
	DelRecipeValidator := valid.DelRecipeValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &DelRecipeValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DelRecipeValidator)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DelRecipeValidator, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}
	if DelRecipeValidator.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(pot.Ctx))
	}
	var RecipeService services.RecipeService
	TpProduct := models.Recipe{
		Id:        DelRecipeValidator.Id,
		PotTypeId: DelRecipeValidator.PotTypeId,
	}
	rsp_err := RecipeService.DeleteRecipe(TpProduct)
	if rsp_err == nil {
		response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(pot.Ctx))
	}
}

func (pot *RecipeController) SendToHDL() {
	SendToMQTTValidator := valid.SendToMQTTValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &SendToMQTTValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(SendToMQTTValidator)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(SendToMQTTValidator, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(pot.Ctx))
			break
		}
		return
	}
	Recipe := services.RecipeService{}
	list, err := Recipe.GetSendToMQTTData(SendToMQTTValidator.AssetId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}
	bytes, err := json.Marshal(list)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}
	fmt.Println(string(bytes))
	err = mqtt.SendToHDL(bytes, SendToMQTTValidator.AccessToken)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}
	response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
}

func (pot *RecipeController) GetMaterialList() {
	searchValidator := valid.SearchMaterialNameValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &searchValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var RecipeService services.RecipeService

	list, err := RecipeService.FindMaterialByName(searchValidator.PotTypeId, searchValidator.Resource)
	if err == nil {
		response.SuccessWithDetailed(200, "success", list, map[string]string{}, (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
	}
}

//func (pot *RecipeController) GetTasteMaterialList() {
//	searchValidator := valid.SearchMaterialNameValidator{}
//	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &searchValidator)
//	if err != nil {
//		fmt.Println("参数解析失败", err.Error())
//	}
//	var RecipeService services.RecipeService
//
//	list, err := RecipeService.FindTasteMaterialList(searchValidator.PotTypeId)
//	if err == nil {
//		response.SuccessWithDetailed(200, "success", list, map[string]string{}, (*context2.Context)(pot.Ctx))
//	} else {
//		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
//	}
//}

func (pot *RecipeController) CreateMaterial() {
	createValidator := valid.CreateMaterialValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &createValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}

	var recipeService services.RecipeService
	isExit, err := recipeService.CreateMaterial(&models.OriginalMaterials{
		Id:        uuid.GetUuid(),
		Name:      createValidator.Name,
		Dosage:    createValidator.Dosage,
		Unit:      createValidator.Unit,
		WaterLine: createValidator.WaterLine,
		Station:   createValidator.Station,
	})
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithDetailed(200, "success", map[string]interface{}{"status": isExit}, map[string]string{}, (*context2.Context)(pot.Ctx))
	}
}

func (pot *RecipeController) CreateTaste() {
	createValidator := valid.CreateTasteValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &createValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}

	var recipeService services.RecipeService
	err = recipeService.CreateTaste(&models.OriginalTaste{
		Id:         uuid.GetUuid(),
		Name:       createValidator.Taste,
		TasteId:    createValidator.TasteId,
		MaterialId: createValidator.MaterialId,
	}, createValidator.Action)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
	}
}

func (pot *RecipeController) DeleteMaterial() {
	searchValidator := valid.DelMaterialValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &searchValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var materialService services.MaterialService

	err = materialService.DeleteMaterial(searchValidator.Id)
	if err == nil {
		response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
		return
	} else {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}
}

func (pot *RecipeController) DeleteTaste() {
	searchValidator := valid.DelTasteValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &searchValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var tasteService services.TasteService

	err = tasteService.DeleteTaste(searchValidator.Id)
	if err == nil {
		response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
		return
	} else {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}
}

func (pot *RecipeController) GetTasteList() {
	searchValidator := valid.SearchTasteValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &searchValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var RecipeService services.TasteService

	list, err := RecipeService.SearchTasteList(searchValidator.PotTypeId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithDetailed(200, "success", list, map[string]string{}, (*context2.Context)(pot.Ctx))
	}
}

func (pot *RecipeController) GetMaterialByName() {
	searchValidator := valid.GetMaterialValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &searchValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var RecipeService services.MaterialService

	bools := RecipeService.GetMaterialByName(searchValidator.Name)
	if bools {
		response.SuccessWithDetailed(200, "success", map[string]string{"status": "EXIST"}, map[string]string{}, (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithDetailed(200, "success", map[string]string{"status": "NOTEXIST"}, map[string]string{}, (*context2.Context)(pot.Ctx))
	}
}

func (pot *RecipeController) CheckPosTasteIdIsRepeat() {
	checkValidator := valid.CheckValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &checkValidator)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}

	var recipeService services.RecipeService
	isExist, err := recipeService.CheckPosTasteIdIsRepeat(checkValidator.TasteId, checkValidator.Action, checkValidator.PotTypeId)

	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(pot.Ctx))
		return
	}
	if isExist {
		response.SuccessWithDetailed(200, "success", map[string]string{"status": "EXIST"}, map[string]string{}, (*context2.Context)(pot.Ctx))
		return
	}
	response.SuccessWithDetailed(200, "success", map[string]string{"status": "NOTEXIST"}, map[string]string{}, (*context2.Context)(pot.Ctx))
	return

}
