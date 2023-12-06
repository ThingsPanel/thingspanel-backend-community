package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	bcrypt "ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
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
func (c *UserController) Index() {
	var reqData valid.PaginateUser
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	var UserService services.UserService
	// 获取用户权限
	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	// SYS_ADMIN只能查询TENANT_ADMIN，TENANT_ADMIN和TENANT_USER只能查询TENANT_USER
	if authority == "SYS_ADMIN" {
		reqData.Authority = "TENANT_ADMIN"
	} else if authority == "TENANT_ADMIN" || authority == "TENANT_USER" {
		reqData.Authority = "TENANT_USER"
	}

	offset := (reqData.Page - 1) * reqData.Limit
	u, i, err := UserService.Paginate(reqData.Search, offset, reqData.Limit, reqData.Authority, tenantId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	d := PaginateUser{
		CurrentPage: reqData.Page,
		Data:        u,
		Total:       i,
		PerPage:     reqData.Limit,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 添加
func (c *UserController) Add() {
	var reqData valid.AddUser
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	var UserService services.UserService
	// 判断是否有添加用户权限
	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if ok {
		if !UserService.HasAddAuthority(authority, reqData.Authority) {
			response.SuccessWithMessage(400, "没有添加该类型用户的权限", (*context2.Context)(c.Ctx))
			return
		}
	} else {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	// 判断用户名和邮箱是否存在
	_, i := UserService.GetUserByName(reqData.Name)
	if i != 0 {
		response.SuccessWithMessage(400, "用户名已存在", (*context2.Context)(c.Ctx))
		return
	}
	_, s, _ := UserService.GetUserByEmail(reqData.Email)
	if s != 0 {
		response.SuccessWithMessage(400, "邮箱已存在", (*context2.Context)(c.Ctx))
		return
	}
	// 如果是系统管理员添加租户管理员，需要生成租户ID
	if authority == "SYS_ADMIN" {
		var uuid = uuid.GetUuid()
		reqData.TenantID = uuid[9:13] + uuid[14:18]
	} else if authority == "TENANT_ADMIN" { // 如果是租户管理员或者租户用户添加租户用户，需要设置租户ID
		// 获取用户租户id
		tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
		if !ok {
			response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
			return
		}
		reqData.TenantID = tenantId
	}
	// 添加用户
	if d, err := UserService.AddUser(reqData); err != nil {
		response.SuccessWithMessage(400, "添加失败", (*context2.Context)(c.Ctx))
		return
	} else {
		response.SuccessWithDetailed(200, "添加成功", d, map[string]string{}, (*context2.Context)(c.Ctx))
	}
}

// 编辑
func (c *UserController) Edit() {
	var reqData valid.EditUser
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 判断是否有编辑用户权限
	var UserService services.UserService
	// 获取用户权限
	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户id
	userId, ok := c.Ctx.Input.GetData("user_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	// 如果修改的不是自己的信息需要判断是否有编辑用户权限
	if userId != reqData.ID {
		// 根据用户id获取被编辑用户权限
		e_user, count := UserService.GetUserById(reqData.ID)
		if count == 0 {
			response.SuccessWithMessage(400, "获取被编辑用户权限失败", (*context2.Context)(c.Ctx))
			return
		}
		// 如果不是系统管理员，要判断是否同一个租户
		if authority != "SYS_ADMIN" {
			if e_user.TenantID != tenantId {
				response.SuccessWithMessage(400, "没有编辑该用户的权限", (*context2.Context)(c.Ctx))
				return
			}
		}
		// 判断是否有编辑用户权限
		if !UserService.HasEditAuthority(authority, e_user.Authority) {
			response.SuccessWithMessage(400, "没有编辑该用户的权限", (*context2.Context)(c.Ctx))
			return
		}
	}

	_, i := UserService.GetSameUserByName(reqData.Name, reqData.ID)
	if i != 0 {
		response.SuccessWithMessage(400, "用户名已存在", (*context2.Context)(c.Ctx))
		return
	}
	_, n := UserService.GetSameUserByEmail(reqData.Email, reqData.ID)
	if n != 0 {
		response.SuccessWithMessage(400, "邮箱已存在", (*context2.Context)(c.Ctx))
		return
	}
	f := UserService.Edit(
		reqData.ID,
		reqData.Name,
		reqData.Email,
		reqData.Mobile,
		reqData.Remark,
		reqData.Enabled,
	)
	if f {
		u, _ := UserService.GetUserById(reqData.ID)
		d := EditUser{
			ID:             u.ID,
			CreatedAt:      u.CreatedAt,
			UpdatedAt:      u.UpdatedAt,
			Enabled:        u.Enabled,
			AdditionalInfo: u.AdditionalInfo,
			Authority:      u.Authority,
			CustomerID:     u.CustomerID,
			Email:          u.Email,
			Name:           u.Name,
		}
		response.SuccessWithDetailed(200, "编辑成功", d, map[string]string{}, (*context2.Context)(c.Ctx))
		return
	}
	// 编辑失败
	response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
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

	// 获取请求用户权限
	authority, ok := this.Ctx.Input.GetData("authority").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	// 获取请求用户租户id
	tenantId, ok := this.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	// 根据请求id及租户id获取待删除用户信息
	eAuthority, eTenantID, err := UserService.GetUserAuthorityById(deleteUserValidate.ID)
	if err != nil {
		response.SuccessWithMessage(400, "获取用户信息失败", (*context2.Context)(this.Ctx))
		return
	}
	if authority != "SYS_ADMIN" {
		// 如果不是系统管理员，要判断是否同一个租户
		if eTenantID != tenantId {
			response.SuccessWithMessage(400, "没有删除该用户的权限", (*context2.Context)(this.Ctx))
			return
		}

	}
	if !UserService.HasEditAuthority(authority, eAuthority) {
		response.SuccessWithMessage(400, "没有删除该用户的权限", (*context2.Context)(this.Ctx))
		return
	}

	f := UserService.Delete(deleteUserValidate.ID)
	if f {
		response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
}

// 修改密码
func (this *UserController) Password() {
	reqData := valid.PasswordUser{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var UserService services.UserService
	// 获取请求用户id
	userId, ok := this.Ctx.Input.GetData("user_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	// 获取请求用户权限
	authority, ok := this.Ctx.Input.GetData("authority").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	// 获取请求用户租户id
	tenantId, ok := this.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	//修改其它用户密码
	if userId != reqData.ID {
		// 根据用户id获取被编辑用户信息
		eAuthority, eTenantID, err := UserService.GetUserAuthorityById(reqData.ID)
		if err != nil {
			response.SuccessWithMessage(400, "获取用户信息失败", (*context2.Context)(this.Ctx))
			return
		}
		//系统管理员不能修改租户用户密码
		if authority != "SYS_ADMIN" {
			// 如果不是系统管理员，要判断是否同一个租户
			if eTenantID != tenantId {
				response.SuccessWithMessage(400, "没有修改该用户密码的权限", (*context2.Context)(this.Ctx))
				return
			}

		}
		//判断是否有编辑该用户权限
		if !UserService.HasEditAuthority(authority, eAuthority) {
			response.SuccessWithMessage(400, "没有修改该用户密码的权限", (*context2.Context)(this.Ctx))
			return
		}
	}

	// 获取原密码
	userInfo, userRow := UserService.GetUserById(reqData.ID)
	if userRow != 1 {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	// 校验原密码是否正确
	if !bcrypt.ComparePasswords(userInfo.Password, []byte(reqData.OldPassword)) {
		response.SuccessWithMessage(400, "原密码错误", (*context2.Context)(this.Ctx))
		return
	}

	f := UserService.Password(reqData.ID, reqData.Password)
	if f {
		response.SuccessWithMessage(200, "修改成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "修改失败", (*context2.Context)(this.Ctx))
	return
}

func (c *UserController) Count() {
	// 获取请求用户权限
	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if !ok {
		response.SuccessWithMessage(400, "用户权限获取失败", (*context2.Context)(c.Ctx))
		return
	}

	if authority != "SYS_ADMIN" && authority != "TENANT_ADMIN" {
		response.SuccessWithMessage(400, "无权限访问该接口", (*context2.Context)(c.Ctx))
		return
	}

	// 获取请求用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "租户ID获取失败", (*context2.Context)(c.Ctx))
		return
	}

	var UserService services.UserService
	userCount, err := UserService.CountUsers(authority, tenantId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	}
	d := make(map[string]int64)
	d["count"] = userCount
	response.SuccessWithDetailed(200, "获取成功", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

func (c *UserController) TenantConfigIndex() {
	// 获取请求用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "租户ID获取失败", (*context2.Context)(c.Ctx))
		return
	}

	var UserService services.UserService
	config, err := UserService.GetTenantConfigByTenantId(tenantId)
	if err != nil {
		response.SuccessWithMessage(400, "查询失败", (*context2.Context)(c.Ctx))
		return
	}

	// 转换
	d := make(map[string]interface{})
	var cc map[string]models.TpTenantAIConfig
	err = json.Unmarshal([]byte(config.CustomConfig), &cc)
	if err != nil {
		response.SuccessWithMessage(400, "CustomConfig解析失败", (*context2.Context)(c.Ctx))
		return
	}
	d["custom_config"] = cc
	response.SuccessWithDetailed(200, "获取成功", d, map[string]string{}, (*context2.Context)(c.Ctx))

}

func (c *UserController) TenantConfigSave() {
	// 获取请求用户租户id
	// tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	// if !ok {
	// 	response.SuccessWithMessage(400, "租户ID获取失败", (*context2.Context)(c.Ctx))
	// 	return
	// }

}
