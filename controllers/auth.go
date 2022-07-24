package controllers

import (
	"ThingsPanel-Go/initialize/redis"
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	bcrypt "ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"

	jwt "ThingsPanel-Go/utils"

	gjwt "github.com/dgrijalva/jwt-go"
)

type AuthController struct {
	beego.Controller
}

type TokenData struct {
	AccessToken string   `json:"access_token"`
	TokenType   string   `json:"token_type"`
	ExpiresIn   int      `json:"expires_in"`
	Menus       []string `json:"menus"`
}

type MeData struct {
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

type RegisterData struct {
	ID         string `json:"id"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	CustomerID string `json:"customer_id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
}

// 登录
func (this *AuthController) Login() {
	loginValidate := valid.LoginValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &loginValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(loginValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(loginValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var UserService services.UserService
	user, i := UserService.GetUserByEmail(loginValidate.Email)
	if i == 0 {
		response.SuccessWithMessage(400, "该账户不存在", (*context2.Context)(this.Ctx))
		return
	}
	if !bcrypt.ComparePasswords(user.Password, []byte(loginValidate.Password)) {
		response.SuccessWithMessage(400, "密码错误", (*context2.Context)(this.Ctx))
		return
	}

	// 生成jwt
	tokenCliams := jwt.UserClaims{
		ID:         user.ID,
		Name:       user.Email,
		CreateTime: time.Now(),
		StandardClaims: gjwt.StandardClaims{
			ExpiresAt: time.Now().Unix() + 3600,
		},
	}
	token, err := jwt.MakeCliamsToken(tokenCliams)
	if err != nil {
		// jwt失败
		response.SuccessWithMessage(400, "token不正确", (*context2.Context)(this.Ctx))
		return
	}
	d := TokenData{
		AccessToken: token,
		TokenType:   "bearer",
		ExpiresIn:   3600,
	}
	redis.SetStr(token, "1", 3000*time.Second)
	// cache.Bm.Put(c.TODO(), token, 1, 3000*time.Second)
	// 登录成功
	response.SuccessWithDetailed(200, "登录成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 退出登录
func (this *AuthController) Logout() {
	authorization := this.Ctx.Request.Header["Authorization"][0]
	userToken := authorization[7:len(authorization)]
	_, err := jwt.ParseCliamsToken(userToken)
	if err != nil {
		response.SuccessWithMessage(400, "token异常", (*context2.Context)(this.Ctx))
		return
	}
	redis.GetStr(userToken)
	if redis.GetStr(userToken) == "1" {
		redis.DelKey(userToken)
	}
	// s, _ := cache.Bm.IsExist(c.TODO(), userToken)
	// if s {
	// 	cache.Bm.Delete(c.TODO(), userToken)
	// }
	response.SuccessWithMessage(200, "退出成功", (*context2.Context)(this.Ctx))
	return
}

// 刷新token
func (this *AuthController) Refresh() {
	authorization := this.Ctx.Request.Header["Authorization"][0]
	userToken := authorization[7:]
	user, err := jwt.ParseCliamsToken(userToken)
	if err != nil {
		response.SuccessWithMessage(400, "token异常", (*context2.Context)(this.Ctx))
		return
	}
	if redis.GetStr(userToken) == "1" {
		redis.DelKey(userToken)
	}
	// s, _ := cache.Bm.IsExist(c.TODO(), userToken)
	// if s {
	// 	cache.Bm.Delete(c.TODO(), userToken)
	// }
	var UserService services.UserService
	_, i := UserService.GetUserById(user.ID)
	if i == 0 {
		response.SuccessWithMessage(400, "该账户不存在", (*context2.Context)(this.Ctx))
		return
	}
	// 生成jwt
	tokenCliams := jwt.UserClaims{
		ID:         user.ID,
		Name:       user.Name,
		CreateTime: time.Now(),
		StandardClaims: gjwt.StandardClaims{
			ExpiresAt: time.Now().Unix() + 3600,
		},
	}
	token, err := jwt.MakeCliamsToken(tokenCliams)
	if err != nil {
		response.SuccessWithMessage(400, "jwt失败", (*context2.Context)(this.Ctx))
		return
	}
	d := TokenData{
		AccessToken: token,
		TokenType:   "bearer",
		ExpiresIn:   3600,
	}
	redis.SetStr(token, "1", 3000*time.Second)
	// cache.Bm.Put(c.TODO(), token, 1, 3000*time.Second)
	response.SuccessWithDetailed(200, "刷新token成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 个人信息
func (this *AuthController) Me() {
	authorization := this.Ctx.Request.Header["Authorization"][0]
	userToken := authorization[7:len(authorization)]
	user, err := jwt.ParseCliamsToken(userToken)
	if err != nil {
		response.SuccessWithMessage(400, "token异常", (*context2.Context)(this.Ctx))
		return
	}
	var UserService services.UserService
	me, i := UserService.GetUserById(user.ID)
	if i == 0 {
		response.SuccessWithMessage(400, "该账户不存在", (*context2.Context)(this.Ctx))
		return
	}
	d := MeData{
		ID:              me.ID,
		CreatedAt:       me.CreatedAt,
		UpdatedAt:       me.UpdatedAt,
		Enabled:         me.Enabled,
		AdditionalInfo:  me.AdditionalInfo,
		Authority:       me.Authority,
		CustomerID:      me.CustomerID,
		Email:           me.Email,
		Name:            me.Name,
		FirstName:       me.FirstName,
		LastName:        me.LastName,
		SearchText:      me.SearchText,
		EmailVerifiedAt: me.EmailVerifiedAt,
	}
	response.SuccessWithDetailed(200, "获取成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 注册 register
func (this *AuthController) Register() {
	registerValidate := valid.RegisterValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &registerValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(registerValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(registerValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var UserService services.UserService
	_, i := UserService.GetUserByName(registerValidate.Name)
	if i != 0 {
		response.SuccessWithMessage(400, "用户名已存在", (*context2.Context)(this.Ctx))
		return
	}
	_, c := UserService.GetUserByEmail(registerValidate.Email)
	if c != 0 {
		response.SuccessWithMessage(400, "邮箱已存在", (*context2.Context)(this.Ctx))
		return
	}
	s, id := UserService.Register(registerValidate.Email, registerValidate.Name, registerValidate.Password, registerValidate.CustomerID)
	if s {
		u, _ := UserService.GetUserById(id)
		d := RegisterData{
			ID:         u.ID,
			CreatedAt:  u.CreatedAt,
			UpdatedAt:  u.UpdatedAt,
			CustomerID: u.CustomerID,
			Email:      u.Email,
			Name:       u.Name,
		}
		response.SuccessWithDetailed(200, "注册成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "注册失败", (*context2.Context)(this.Ctx))
	return
}
