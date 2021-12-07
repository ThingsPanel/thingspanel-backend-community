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

type UserController struct {
	beego.Controller
}

type PaginateUser struct {
	CurrentPage int                     `json:"current_page"`
	Data        []services.PaginateUser `json:"data"`
	Total       int64                   `json:"total"`
	PerPage     int                     `json:"per_page"`
}

type AddUser struct {
	ID              string `json:"id"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	Enabled         string `json:"enabled"`
	AdditionalInfo  string `json:"additional_info"`
	Authority       string `json:"authority"`
	CustomerID      string `json:"customer_id"`
	Email           string `json:"email"`
	Name            string `json:"name"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	SearchText      string `json:"search_text"`
	EmailVerifiedAt int64  `json:"email_verified_at"`
}

type EditUser struct {
	ID              string `json:"id"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	Enabled         string `json:"enabled"`
	AdditionalInfo  string `json:"additional_info"`
	Authority       string `json:"authority"`
	CustomerID      string `json:"customer_id"`
	Email           string `json:"email"`
	Name            string `json:"name"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	SearchText      string `json:"search_text"`
	EmailVerifiedAt int64  `json:"email_verified_at"`
}

// 列表
func (this *UserController) Index() {
	paginateUserValidate := valid.PaginateUser{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &paginateUserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(paginateUserValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(paginateUserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var UserService services.UserService
	u, c := UserService.Paginate(paginateUserValidate.Search, paginateUserValidate.Page-1, paginateUserValidate.Limit)
	d := PaginateUser{
		CurrentPage: paginateUserValidate.Page,
		Data:        u,
		Total:       c,
		PerPage:     paginateUserValidate.Limit,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return

}

// 添加
func (this *UserController) Add() {
	addUserValidate := valid.AddUser{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &addUserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addUserValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(addUserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var UserService services.UserService
	_, i := UserService.GetUserByName(addUserValidate.Name)
	if i != 0 {
		response.SuccessWithMessage(400, "用户名已存在", (*context2.Context)(this.Ctx))
		return
	}
	_, c := UserService.GetUserByEmail(addUserValidate.Email)
	if c != 0 {
		response.SuccessWithMessage(400, "邮箱已存在", (*context2.Context)(this.Ctx))
		return
	}
	f, id := UserService.Add(
		addUserValidate.Name,
		addUserValidate.Email,
		addUserValidate.Password,
		addUserValidate.Enabled,
		addUserValidate.Mobile,
		addUserValidate.Remark,
	)
	if f {
		u, _ := UserService.GetUserById(id)
		d := AddUser{
			ID:              u.ID,
			CreatedAt:       u.CreatedAt,
			UpdatedAt:       u.UpdatedAt,
			Enabled:         u.Enabled,
			AdditionalInfo:  u.AdditionalInfo,
			Authority:       u.Authority,
			CustomerID:      u.CustomerID,
			Email:           u.Email,
			Name:            u.Name,
			FirstName:       u.FirstName,
			LastName:        u.LastName,
			SearchText:      u.SearchText,
			EmailVerifiedAt: u.EmailVerifiedAt,
		}
		response.SuccessWithDetailed(200, "添加成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "添加失败", (*context2.Context)(this.Ctx))
	return
}

// 编辑
func (this *UserController) Edit() {
	editUserValidate := valid.EditUser{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &editUserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(editUserValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(editUserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var UserService services.UserService
	_, i := UserService.GetSameUserByName(editUserValidate.Name, editUserValidate.ID)
	if i != 0 {
		response.SuccessWithMessage(400, "用户名已存在", (*context2.Context)(this.Ctx))
		return
	}
	_, c := UserService.GetSameUserByEmail(editUserValidate.Email, editUserValidate.ID)
	if c != 0 {
		response.SuccessWithMessage(400, "邮箱已存在", (*context2.Context)(this.Ctx))
		return
	}
	f := UserService.Edit(
		editUserValidate.ID,
		editUserValidate.Name,
		editUserValidate.Email,
		editUserValidate.Mobile,
		editUserValidate.Remark,
	)
	if f {
		u, _ := UserService.GetUserById(editUserValidate.ID)
		d := EditUser{
			ID:              u.ID,
			CreatedAt:       u.CreatedAt,
			UpdatedAt:       u.UpdatedAt,
			Enabled:         u.Enabled,
			AdditionalInfo:  u.AdditionalInfo,
			Authority:       u.Authority,
			CustomerID:      u.CustomerID,
			Email:           u.Email,
			Name:            u.Name,
			FirstName:       u.FirstName,
			LastName:        u.LastName,
			SearchText:      u.SearchText,
			EmailVerifiedAt: u.EmailVerifiedAt,
		}
		response.SuccessWithDetailed(200, "编辑成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	// 编辑失败
	response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
	return
}

// 添加权限
func (this *UserController) Permission() {
	response.SuccessWithMessage(200, "设置成功", (*context2.Context)(this.Ctx))
	return
}

// 删除
func (this *UserController) Delete() {
	deleteUserValidate := valid.DeleteUser{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deleteUserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deleteUserValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(deleteUserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var UserService services.UserService
	f := UserService.Delete(deleteUserValidate.ID)
	if f {
		response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
	return
}

// 修改密码
func (this *UserController) Password() {
	passwordUserValidate := valid.PasswordUser{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &passwordUserValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(passwordUserValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(passwordUserValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var UserService services.UserService
	f := UserService.Password(passwordUserValidate.ID, passwordUserValidate.Password)
	if f {
		response.SuccessWithMessage(200, "修改成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "修改失败", (*context2.Context)(this.Ctx))
	return
}
