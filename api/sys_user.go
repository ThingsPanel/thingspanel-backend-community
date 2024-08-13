package api

import (
	"net/http"

	model "project/model"
	service "project/service"
	utils "project/utils"

	"github.com/gin-gonic/gin"
)

type UserApi struct{}

// Login
// @Tags     base
// @Summary  用户登录
// @Description 该接口用于登录获取token。
// @Produce   application/json
// @Param    data  body      model.LoginReq     true  "email,password"
// @Success  200    {object}  ApiResponse  "登录成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Router   /api/v1/login [post]
func (a *UserApi) Login(c *gin.Context) {
	var loginReq model.LoginReq
	if !BindAndValidate(c, &loginReq) {
		return
	}

	loginLock := service.NewLoginLock()

	// 检查是否需要锁定账户
	if loginLock.MaxFailedAttempts > 0 {
		if err := loginLock.GetAllowLogin(c, loginReq.Email); err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
			return
		}
	}

	loginRsp, err := service.GroupApp.User.Login(c, &loginReq)
	if err != nil {
		_ = loginLock.LoginFail(c, loginReq.Email)
		c.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
		return
	}
	_ = loginLock.LoginSuccess(c, loginReq.Email)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": loginRsp})
}

// GET /api/v1/user/logout
func (a *UserApi) Logout(c *gin.Context) {
	token := c.GetHeader("x-token")
	err := service.GroupApp.User.Logout(token)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Logout successfully", nil)
}

// GET /api/v1/user/refresh
func (a *UserApi) RefreshToken(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	loginRsp, err := service.GroupApp.User.RefreshToken(userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Refresh token successfully", loginRsp)
}

// GET /api/v1/verification/code
func (a *UserApi) GetVerificationCode(c *gin.Context) {
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
func (a *UserApi) ResetPassword(c *gin.Context) {
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
// @Tags     用户管理
// @Summary  创建新用户
// @Description 该接口用于创建租户管理员、租户用户。
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateUserReq     true  "email,password,mobile"
// @Success  200  {object}  ApiResponse  "用户创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/user [post]
func (a *UserApi) CreateUser(c *gin.Context) {
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
// @Tags     用户管理
// @Summary  分页获取用户列表
// @Description 该接口用于分页获取用户列表。
// @accept    application/json
// @Produce   application/json
// @Param     data  query      model.UserListReq     true  "page,page_size"
// @Success  200  {object}  ApiResponse  "获取用户列表成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/user [get]
func (a *UserApi) GetUserListByPage(c *gin.Context) {
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
// @Tags     用户管理
// @Summary  修改用户信息
// @Description 该接口用于修改用户信息。
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateUserReq     true  "id"
// @Success  200  {object}  ApiResponse  "修改用户信息成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/user [put]
func (a *UserApi) UpdateUser(c *gin.Context) {
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
// @Tags     用户管理
// @Summary  删除用户
// @Description 该接口用于删除用户，无法删除系统管理员，只有系统管理员才可删除租户管理员。
// @accept    application/json
// @Produce   application/json
// @Param     id  path      string     true  "用户ID"
// @Success  200  {object}  ApiResponse  "删除用户成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/user/{id} [delete]
func (a *UserApi) DeleteUser(c *gin.Context) {
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
// @Tags     用户管理
// @Summary  获取用户信息
// @Description 该接口用于获取用户信息。
// @accept    application/json
// @Produce   application/json
// @Param     id  path      string     true  "用户ID"
// @Success  200  {object}  ApiResponse  "获取用户信息成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/user/{id} [get]
func (a *UserApi) GetUser(c *gin.Context) {
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
// @Tags     个人信息管理
// @Summary  个人信息查看接口
// @Description 该接口用于查看个人信息。
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse  "查看个人信息成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/user/detail [get]
func (a *UserApi) GetUserDetail(c *gin.Context) {

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	user, err := service.GroupApp.User.GetUserDetail(userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get user successfully", user)
}

// UpdateUsers 修改用户信息
// @Tags     个人信息管理
// @Summary  修改用户信息
// @Description 该接口用于修改用户密码，姓名，备注。
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateUserInfoReq     true  "id"
// @Success  200  {object}  ApiResponse  "修改用户信息成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/user/update [put]
func (a *UserApi) UpdateUsers(c *gin.Context) {
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
func (a *UserApi) TransformUser(c *gin.Context) {
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

// EmailRegister
// @description 租户邮箱注册
func (a *UserApi) EmailRegister(c *gin.Context) {
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
