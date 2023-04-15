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

type TreeBusinessData struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Children []TreeBusiness `json:"children"`
}

type TreeBusiness struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Children []TreeBusiness2 `json:"children"`
}

type TreeBusiness2 struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Children []TreeBusiness3 `json:"children"`
}

type TreeBusiness3 struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
	offset := (paginateBusinessValidate.Page - 1) * paginateBusinessValidate.Limit
	u, c, err := BusinessService.Paginate(paginateBusinessValidate.Name, offset, paginateBusinessValidate.Limit)
	if err != nil {
		response.SuccessWithMessage(400, "查询失败", (*context2.Context)(this.Ctx))
		return
	}
	var ResBusinessData []services.PaginateBusiness
	if c != 0 {
		var AssetService services.AssetService
		var is_device int
		for _, bv := range u {
			_, err := AssetService.GetAssetDataByBusinessId(bv.ID)
			if err != nil {
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
	if len(ResBusinessData) == 0 {
		ResBusinessData = []services.PaginateBusiness{}
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
		b, i, err := BusinessService.GetBusinessById(id)
		if err != nil && i == 0 {
			response.SuccessWithMessage(400, "新增失败", (*context2.Context)(this.Ctx))
			return
		}
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
	var NavigationService services.NavigationService
	f := BusinessService.Delete(deleteBusinessValidate.ID)
	if f {
		NavigationService.DeleteByBusinessID(deleteBusinessValidate.ID)
		response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
	return
}

// 业务资产树
func (this *BusinessController) Tree() {
	var BusinessService services.BusinessService
	var ResTreeBusinessData []TreeBusinessData
	var AssetService services.AssetService
	bl, bc := BusinessService.All()
	if bc > 0 {
		for _, v := range bl {
			l, c := AssetService.GetAssetByBusinessId(v.ID)
			var ResTreeBusiness []TreeBusiness
			if c != 0 {
				for _, s := range l {
					l2, c2, err := AssetService.GetAssetsByParentID(s.ID)
					var ResTreeBusiness2 []TreeBusiness2
					if c2 != 0 && err == nil {
						for _, s2 := range l2 {
							l3, c3, err := AssetService.GetAssetsByParentID(s2.ID)
							var ResTreeBusiness3 []TreeBusiness3
							if c3 != 0 && err == nil {
								for _, s3 := range l3 {
									td3 := TreeBusiness3{
										ID:   s3.ID,
										Name: s3.Name,
									}
									ResTreeBusiness3 = append(ResTreeBusiness3, td3)
								}
							} else if err != nil {
								fmt.Println(err)
							}
							if len(ResTreeBusiness3) == 0 {
								ResTreeBusiness3 = []TreeBusiness3{}
							}
							td2 := TreeBusiness2{
								ID:       s2.ID,
								Name:     s2.Name,
								Children: ResTreeBusiness3,
							}
							ResTreeBusiness2 = append(ResTreeBusiness2, td2)
						}
					} else if err != nil {
						fmt.Println(err)
					}
					if len(ResTreeBusiness2) == 0 {
						ResTreeBusiness2 = []TreeBusiness2{}
					}
					td := TreeBusiness{
						ID:       s.ID,
						Name:     s.Name,
						Children: ResTreeBusiness2,
					}
					ResTreeBusiness = append(ResTreeBusiness, td)
				}
			}
			tb := TreeBusinessData{
				ID:       v.ID,
				Name:     v.Name,
				Children: ResTreeBusiness,
			}
			ResTreeBusinessData = append(ResTreeBusinessData, tb)
		}
		response.SuccessWithDetailed(200, "success", ResTreeBusinessData, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithDetailed(200, "success", "", map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
