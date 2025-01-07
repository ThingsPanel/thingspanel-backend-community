package api

import (
	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type UserApi struct{}

// Login
// @Router   /api/v1/login [post]
func (*UserApi) Login(c *gin.Context) {
	var loginReq model.LoginReq
	if !BindAndValidate(c, &loginReq) {
		return
	}

	result := utils.ValidateInput(loginReq.Email)
	if !result.IsValid {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": result.Message,
		}))
		return
	}

	if result.Type == utils.Phone {
		// 通过手机号获取用户邮箱
		email, err := service.GroupApp.User.GetUserEmailByPhoneNumber(loginReq.Email)
		if err != nil {
			c.Error(err)
			return
		}
		loginReq.Email = email
	}

	loginLock := service.NewLoginLock()

	// 检查是否需要锁定账户
	if loginLock.MaxFailedAttempts > 0 {
		if err := loginLock.GetAllowLogin(c, loginReq.Email); err != nil {
			c.Error(err)
			return
		}
	}

	loginRsp, err := service.GroupApp.User.Login(c, &loginReq)
	if err != nil {
		_ = loginLock.LoginFail(c, loginReq.Email)
		c.Error(err)
		return
	}
	_ = loginLock.LoginSuccess(c, loginReq.Email)
	c.Set("data", loginRsp)
}

// GET /api/v1/user/logout
func (*UserApi) Logout(c *gin.Context) {
	token := c.GetHeader("x-token")
	err := service.GroupApp.User.Logout(token)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GET /api/v1/user/refresh
func (*UserApi) RefreshToken(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	loginRsp, err := service.GroupApp.User.RefreshToken(userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", loginRsp)
}

// GET /api/v1/verification/code
func (*UserApi) HandleVerificationCode(c *gin.Context) {
	email := c.Query("email")
	isRegister := c.Query("is_register")
	err := service.GroupApp.User.GetVerificationCode(email, isRegister)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// POST /api/v1/reset/password
func (*UserApi) ResetPassword(c *gin.Context) {
	var resetPasswordReq model.ResetPasswordReq
	if !BindAndValidate(c, &resetPasswordReq) {
		return
	}

	err := service.GroupApp.User.ResetPassword(c, &resetPasswordReq)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// CreateUser 创建用户
// @Router   /api/v1/user [post]
func (*UserApi) CreateUser(c *gin.Context) {
	var createUserReq model.CreateUserReq

	if !BindAndValidate(c, &createUserReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.User.CreateUser(&createUserReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// GetUserListByPage 分页获取用户列表
// @Router   /api/v1/user [get]
func (*UserApi) HandleUserListByPage(c *gin.Context) {
	var userListReq model.UserListReq

	if !BindAndValidate(c, &userListReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	userList, err := service.GroupApp.User.GetUserListByPage(&userListReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", userList)
}

// UpdateUser 修改用户信息
// @Router   /api/v1/user [put]
func (*UserApi) UpdateUser(c *gin.Context) {
	var updateUserReq model.UpdateUserReq

	if !BindAndValidate(c, &updateUserReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.User.UpdateUser(&updateUserReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// DeleteUser 删除用户
// @Router   /api/v1/user/{id} [delete]
func (*UserApi) DeleteUser(c *gin.Context) {
	id := c.Param("id")

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.User.DeleteUser(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// GetUser 获取用户信息
// @Router   /api/v1/user/{id} [get]
func (*UserApi) HandleUser(c *gin.Context) {
	id := c.Param("id")

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	user, err := service.GroupApp.User.GetUser(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	user.Password = ""

	c.Set("data", user)
}

// GetUserDetail 个人信息查看接口
// @Router   /api/v1/user/detail [get]
func (*UserApi) HandleUserDetail(c *gin.Context) {

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	user, err := service.GroupApp.User.GetUserDetail(userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", user)
}

// UpdateUsers 修改用户信息
// @Router   /api/v1/user/update [put]
func (*UserApi) UpdateUsers(c *gin.Context) {
	var updateUserInfoReq model.UpdateUserInfoReq

	if !BindAndValidate(c, &updateUserInfoReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.User.UpdateUserInfo(c, &updateUserInfoReq, userClaims)

	if err != nil {
		c.Error(err)
	}

	c.Set("data", nil)
}

// /api/v1/user/transform
func (*UserApi) TransformUser(c *gin.Context) {
	var transformUserReq model.TransformUserReq

	if !BindAndValidate(c, &transformUserReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	loginRsp, err := service.GroupApp.User.TransformUser(&transformUserReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", loginRsp)
}

// EmailRegister /api/v1/tenant/email/register POST
// @description 租户邮箱注册
func (*UserApi) EmailRegister(c *gin.Context) {
	var req model.EmailRegisterReq
	if !BindAndValidate(c, &req) {
		return
	}
	loginRsp, err := service.GroupApp.EmailRegister(c, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", loginRsp)
}
