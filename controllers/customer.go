package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
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

type CustomerController struct {
	beego.Controller
}

type PaginateCustomer struct {
	CurrentPage int               `json:"current_page"`
	Data        []models.Customer `json:"data"`
	Total       int64             `json:"total"`
	PerPage     int               `json:"per_page"`
}

type AddCustomer struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Email string `json:"email"`
}

type EditCustomer struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Email string `json:"email"`
}

// 列表
func (this *CustomerController) Index() {
	paginateCustomerValidate := valid.PaginateCustomer{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &paginateCustomerValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(paginateCustomerValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(paginateCustomerValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var CustomerService services.CustomerService
	u, c := CustomerService.Paginate(paginateCustomerValidate.Search, paginateCustomerValidate.Page-1, paginateCustomerValidate.Limit)
	d := PaginateCustomer{
		CurrentPage: paginateCustomerValidate.Page,
		Data:        u,
		Total:       c,
		PerPage:     paginateCustomerValidate.Limit,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return

}

// 添加
func (this *CustomerController) Add() {
	addCustomerValidate := valid.AddCustomer{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &addCustomerValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addCustomerValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(addCustomerValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var CustomerService services.CustomerService
	f, id := CustomerService.Add(
		addCustomerValidate.Title,
		addCustomerValidate.Email,
	)
	if f {
		u, i := CustomerService.GetCustomerById(id)
		if i == 0 {
			response.SuccessWithMessage(400, "添加失败", (*context2.Context)(this.Ctx))
			return
		}
		d := AddCustomer{
			ID:    u.ID,
			Title: u.Title,
			Email: u.Email,
		}
		response.SuccessWithDetailed(200, "添加成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "添加失败", (*context2.Context)(this.Ctx))
	return
}

// 编辑
func (this *CustomerController) Edit() {
	editCustomerValidate := valid.EditCustomer{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &editCustomerValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(editCustomerValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(editCustomerValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var CustomerService services.CustomerService
	f := CustomerService.Edit(
		editCustomerValidate.ID,
		editCustomerValidate.Title,
		editCustomerValidate.Email,
		editCustomerValidate.AdditionalInfo,
		editCustomerValidate.Address,
		editCustomerValidate.Address2,
		editCustomerValidate.City,
		editCustomerValidate.Country,
		editCustomerValidate.Phone,
		editCustomerValidate.Zip,
	)
	if f {
		u, i := CustomerService.GetCustomerById(editCustomerValidate.ID)
		if i == 0 {
			response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
			return
		}
		d := EditCustomer{
			ID:    u.ID,
			Title: u.Title,
			Email: u.Email,
		}
		response.SuccessWithDetailed(200, "编辑成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
	return
}

// 删除
func (this *CustomerController) Delete() {
	deleteCustomerValidate := valid.DeleteCustomer{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deleteCustomerValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deleteCustomerValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(deleteCustomerValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var CustomerService services.CustomerService
	f := CustomerService.Delete(deleteCustomerValidate.ID)
	if f {
		response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
	return
}
