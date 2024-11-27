package api

import (
	"net/http"

	model "project/internal/model"
	service "project/internal/service"
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

	loginLock := service.NewLoginLock()

	// 检查是否需要锁定账户
	if loginLock.MaxFailedAttempts > 0 {
		if err := loginLock.GetAllowLogin(c, loginReq.Email); err != nil {
			//c.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
			c.Error(err)
			return
		}
	}

	loginRsp, err := service.GroupApp.User.Login(c, &loginReq)
	if err != nil {
		_ = loginLock.LoginFail(c, loginReq.Email)
		//c.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
		c.Error(err)
		return
	}
	_ = loginLock.LoginSuccess(c, loginReq.Email)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": loginRsp})
}

// GET /api/v1/user/logout
func (*UserApi) Logout(c *gin.Context) {
	token := c.GetHeader("x-token")
	err := service.GroupApp.User.Logout(token)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Logout successfully", nil)
}

// GET /api/v1/user/refresh
func (*UserApi) RefreshToken(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	loginRsp, err := service.GroupApp.User.RefreshToken(userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Refresh token successfully", loginRsp)
}

// GET /api/v1/verification/code
func (*UserApi) GetVerificationCode(c *gin.Context) {
	email := c.Query("email")
	isRegister := c.Query("is_register")
	err := service.GroupApp.User.GetVerificationCode(email, isRegister)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get verification code successfully", nil)
}

// POST /api/v1/reset/password
func (*UserApi) ResetPassword(c *gin.Context) {
	var resetPasswordReq model.ResetPasswordReq
	if !BindAndValidate(c, &resetPasswordReq) {
		return
	}

	err := service.GroupApp.User.ResetPassword(c, &resetPasswordReq)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Reset password successfully", nil)
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
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "User created successfully", nil)
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
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get user list successfully", userList)
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
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update user successfully", nil)
}

// DeleteUser 删除用户
// @Router   /api/v1/user/{id} [delete]
func (*UserApi) DeleteUser(c *gin.Context) {
	id := c.Param("id")

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.User.DeleteUser(id, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Delete user successfully", nil)
}

// GetUser 获取用户信息
// @Router   /api/v1/user/{id} [get]
func (*UserApi) HandleUser(c *gin.Context) {
	id := c.Param("id")

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	user, err := service.GroupApp.User.GetUser(id, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get user successfully", user)
}

// GetUserDetail 个人信息查看接口
// @Router   /api/v1/user/detail [get]
func (*UserApi) HandleUserDetail(c *gin.Context) {

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	user, err := service.GroupApp.User.GetUserDetail(userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get user successfully", user)
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
		ErrorHandler(c, 400, err)
	}

	SuccessHandler(c, "Update user successfully", nil)
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
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Transform successfully", loginRsp)
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
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "EmailRegister successfully", loginRsp)
}
