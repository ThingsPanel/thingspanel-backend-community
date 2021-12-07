// 业务
package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type BusinessController struct {
	beego.Controller
}

type PaginateBusiness struct {
	CurrentPage int                         `json:"current_page"`
	Data        []services.PaginateBusiness `json:"data"`
	Total       int64                       `json:"total"`
	PerPage     int                         `json:"per_page"`
}

type AddBusiness struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

// 获取列表
func (this *BusinessController) Index() {
	paginateBusinessValidate := valid.PaginateBusiness{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &paginateBusinessValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(paginateBusinessValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(paginateBusinessValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var BusinessService services.BusinessService
	u, c := BusinessService.Paginate(paginateBusinessValidate.Name, paginateBusinessValidate.Page-1, paginateBusinessValidate.Limit)
	var ResBusinessData []services.PaginateBusiness
	if c != 0 {
		var AssetService services.AssetService
		var is_device int
		for _, bv := range u {
			_, ac := AssetService.GetAssetDataByBusinessId(bv.ID)
			if ac == 0 {
				is_device = 0
			} else {
				is_device = 1
			}
			item := services.PaginateBusiness{
				ID:        bv.ID,
				Name:      bv.Name,
				CreatedAt: bv.CreatedAt,
				IsDevice:  is_device,
			}
			ResBusinessData = append(ResBusinessData, item)
		}
	}
	d := PaginateBusiness{
		CurrentPage: paginateBusinessValidate.Page,
		Data:        ResBusinessData,
		Total:       c,
		PerPage:     paginateBusinessValidate.Limit,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 新增
func (this *BusinessController) Add() {
	addBusinessValidate := valid.AddBusiness{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &addBusinessValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addBusinessValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(addBusinessValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var BusinessService services.BusinessService
	f, id := BusinessService.Add(addBusinessValidate.Name)
	if f {
		b, _ := BusinessService.GetBusinessById(id)
		u := AddBusiness{
			ID:        b.ID,
			Name:      b.Name,
			CreatedAt: b.CreatedAt,
		}
		response.SuccessWithDetailed(200, "新增成功", u, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "新增失败", (*context2.Context)(this.Ctx))
	return
}

// 编辑
func (this *BusinessController) Edit() {
	editBusinessValidate := valid.EditBusiness{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &editBusinessValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(editBusinessValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(editBusinessValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var BusinessService services.BusinessService
	f := BusinessService.Edit(editBusinessValidate.ID, editBusinessValidate.Name)
	if f {
		response.SuccessWithMessage(200, "编辑成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
	return
}

// 删除
func (this *BusinessController) Delete() {
	deleteBusinessValidate := valid.DeleteBusiness{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deleteBusinessValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deleteBusinessValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(deleteBusinessValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var BusinessService services.BusinessService
	f := BusinessService.Delete(deleteBusinessValidate.ID)
	if f {
		response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
	return
}
