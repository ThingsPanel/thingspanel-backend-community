package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
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
		fmt.Println("参数解析失败", err.Error())
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
	dd := valid.RspRecipePaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(pot.Ctx))

}

/**
创建
*/
func (pot *RecipeController) Add() {
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

	id := uuid.GetUuid()
	Recipe := models.Recipe{
		Id:               id,
		BottomPotId:      addRecipeValidate.BottomPotId,
		BottomPot:        addRecipeValidate.BottomPot,
		PotTypeId:        addRecipeValidate.PotTypeId,
		PotTypeName:      addRecipeValidate.PotTypeName,
		Materials:        addRecipeValidate.Materials,
		Taste:            addRecipeValidate.Tastes,
		BottomProperties: addRecipeValidate.BottomProperties,
		SoupStandard:     addRecipeValidate.SoupStandard,
		CreateAt:         time.Now().Unix(),
	}

	MaterialArr := make([]models.Materials, 0)
	MaterialIdArr := make([]string, 0)
	TasteArr := make([]models.Taste, 0)
	TasteIdArr := make([]string, 0)
	for _, v := range addRecipeValidate.MaterialsArr {
		materialUuid := uuid.GetUuid()
		MaterialIdArr = append(MaterialIdArr, materialUuid)
		MaterialArr = append(MaterialArr, models.Materials{
			Id:        materialUuid,
			Name:      v.Name,
			Dosage:    v.Dosage,
			Unit:      v.Unit,
			WaterLine: v.WaterLine,
			Station:   v.Station,
		})
	}

	for _, v := range addRecipeValidate.TastesArr {
		tasteUuid := uuid.GetUuid()
		TasteIdArr = append(TasteIdArr, tasteUuid)
		TasteArr = append(TasteArr, models.Taste{
			Id:            tasteUuid,
			Name:          v.Name,
			TasteId:       v.TasteId,
			MaterialsName: v.MaterialsName,
			Dosage:        v.Dosage,
			Unit:          v.Unit,
			CreateAt:      time.Now().Unix(),
			WaterLine:     v.WaterLine,
			Station:       v.Station,
		})
	}
	Recipe.MaterialsId = strings.Join(MaterialIdArr, ",")
	Recipe.TasteId = strings.Join(TasteIdArr, ",")
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
	RecipeValidate := valid.AddRecipeValidator{}
	err := json.Unmarshal(pot.Ctx.Input.RequestBody, &RecipeValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
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
	isSucess := Recipe.EditRecipe(RecipeValidate)
	if isSucess {
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
		Id: DelRecipeValidator.Id,
	}
	rsp_err := RecipeService.DeleteRecipe(TpProduct)
	if rsp_err == nil {
		response.SuccessWithMessage(200, "success", (*context2.Context)(pot.Ctx))
	} else {
		response.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(pot.Ctx))
	}
}
