package api

import (
	"net/http"

	common "project/common"
	model "project/model"
	service "project/service"
	utils "project/utils"

	"github.com/gin-gonic/gin"
)

type BoardApi struct{}

// CreateBoard 创建看板
// @Tags     看板
// @Summary  创建看板
// @Description 创建看板
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateBoardReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "创建看板成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board [post]
func (api *BoardApi) CreateBoard(c *gin.Context) {
	var req model.CreateBoardReq
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID

	boardInfo, err := service.GroupApp.Board.CreateBoard(c, &req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create board successfully", boardInfo)
}

// UpdateBoard 更新看板
// @Tags     看板
// @Summary  更新看板
// @Description 更新看板
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateBoardReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "更新看板成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board [put]
func (api *BoardApi) UpdateBoard(c *gin.Context) {
	var req model.UpdateBoardReq
	if !BindAndValidate(c, &req) {
		return
	}

	// if req.Description == nil && req.Name == "" && req.HomeFlag == "" {
	// 	c.JSON(http.StatusOK, gin.H{"code": 400, "message": "修改内容不能为空"})
	// 	return
	// }

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID

	d, err := service.GroupApp.Board.UpdateBoard(c, &req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update board successfully", d)
}

// DeleteBoard 删除看板
// @Tags     看板
// @Summary  删除看板
// @Description 删除看板
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "ID"
// @Success  200  {object}  ApiResponse  "更新看板成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/{id} [delete]
func (api *BoardApi) DeleteBoard(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.Board.DeleteBoard(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete board successfully", nil)
}

// GetBoardListByPage 看板分页查询
// @Tags     看板
// @Summary  看板分页查询
// @Description 看板分页查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetBoardListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board [get]
func (api *BoardApi) GetBoardListByPage(c *gin.Context) {
	var req model.GetBoardListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	boardList, err := service.GroupApp.Board.GetBoardListByPage(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get board list successfully", boardList)
}

// GetBoard 看板详情查询
// @Tags     看板
// @Summary  看板详情查询
// @Description 看板详情查询
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "ID"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/{id} [get]
func (api *BoardApi) GetBoard(c *gin.Context) {
	id := c.Param("id")
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	board, err := service.GroupApp.Board.GetBoard(id, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get board successfully", board)
}

// GetBoardListByTenantId 首页看板查询
// @Tags     看板
// @Summary  首页看板查询
// @Description 首页看板查询
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/home [get]
func (api *BoardApi) GetBoardListByTenantId(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	boardList, err := service.GroupApp.Board.GetBoardListByTenantId(userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get board list successfully", boardList)
}

// GetDeviceTotal 设备总数
// @Tags     看板
// @Summary  设备总数
// @Description 设备总数查询
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse{data=int}  "获取信息成功"
// @Failure  400  {object}  ApiResponse{data=int}  "无效的请求数据"
// @Failure  500  {object}  ApiResponse{data=int}  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/device/total [get]
func (api *BoardApi) GetDeviceTotal(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	board := service.GroupApp.Board
	total, err := board.GetDeviceTotal(c, userClaims.Authority, userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get board list successfully", total)
}

// GetDevice 设备信息
// @Tags     看板
// @Summary  设备总数/激活数
// @Description 设备总数/激活数
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse{data=model.GetBoardDeviceRes}  "请求成功"
// @Failure  400  {object}  ApiResponse{data=model.GetBoardDeviceRes}   "无效的请求数据"
// @Failure  500  {object}  ApiResponse{data=model.GetBoardDeviceRes}   "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/device [get]
func (api *BoardApi) GetDevice(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	if !common.CheckUserIsAdmin(userClaims.Authority) {
		SuccessHandler(c, "Restricted permissions！", "权限受限！")
		return
	}

	board := service.GroupApp.Board
	data, err := board.GetDevice(c)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get board list successfully", data)
}

// GetTenant 租户信息
// @Tags     看板
// @Summary  租户信息
// @Description 租户总数&昨日新增&本月新增&月历史数据
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse{data=model.GetTenantRes}  "获取信息成功"
// @Failure  400  {object}  ApiResponse{data=model.GetTenantRes}  "无效的请求数据"
// @Failure  500  {object}  ApiResponse{data=model.GetTenantRes}  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/tenant [get]
func (api *BoardApi) GetTenant(c *gin.Context) {
	//TODO::不知道需不需要再次验证用户信息
	users := service.UsersService{}
	data, err := users.GetTenant(c)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get message successfully!", data)
}

// GetTenantUserInfo 租户下用户信息
// @Tags     看板
// @Summary  租户下用户信息
// @Description 租户下用户总数&昨日新增&本月新增&月历史数据
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse{data=model.GetTenantRes}  "获取信息成功"
// @Failure  400  {object}  ApiResponse{data=model.GetTenantRes}  "无效的请求数据"
// @Failure  500  {object}  ApiResponse{data=model.GetTenantRes}  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/tenant/user/info [get]
func (api *BoardApi) GetTenantUserInfo(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	users := service.UsersService{}
	data, err := users.GetTenantUserInfo(c, userClaims.Email)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get message successfully!", data)
}

// GetTenantDeviceInfo 租户下设备信息
// @Tags     看板
// @Summary  租户下设备信息
// @Description 租户下设备总数&on
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse{data=model.GetBoardDeviceRes}  "请求成功"
// @Failure  400  {object}  ApiResponse{data=model.GetBoardDeviceRes}   "无效的请求数据"
// @Failure  500  {object}  ApiResponse{data=model.GetBoardDeviceRes}   "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/tenant/device/info [get]
func (api *BoardApi) GetTenantDeviceInfo(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	board := service.GroupApp.Board
	total, err := board.GetDeviceByTenantID(c, userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get board list successfully", total)
}

// GetUserInfo 个人信息查询
// @Tags     看板
// @Summary  个人信息查询
// @Description 个人信息查询
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse{data=model.UsersRes}  "获取信息成功"
// @Failure  400  {object}  ApiResponse{data=model.UsersRes}  "无效的请求数据"
// @Failure  500  {object}  ApiResponse{data=model.UsersRes}  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/user/info [get]
func (api *BoardApi) GetUserInfo(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	users := service.UsersService{}
	data, err := users.GetTenantInfo(c, userClaims.Email)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get message successfully!", *data)
}

// UpdateUserInfo 更新个人信息
// @Tags     看板
// @Summary  更新个人信息
// @Description 更新个人信息
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UsersUpdateReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "获取卡片信息成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/user/update [post]
func (api *BoardApi) UpdateUserInfo(c *gin.Context) {
	var param model.UsersUpdateReq
	if !BindAndValidate(c, &param) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)

	users := service.UsersService{}
	err := users.UpdateTenantInfo(c, userClaims, &param)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessOK(c)
}

// UpdateUserInfoPassword 更新个人密码
// @Tags     看板
// @Summary  更新个人密码
// @Description 更新个人密码
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UsersUpdatePasswordReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "获取卡片信息成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/board/user/update/password [post]
func (api *BoardApi) UpdateUserInfoPassword(c *gin.Context) {
	var param model.UsersUpdatePasswordReq
	if !BindAndValidate(c, &param) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)

	users := service.UsersService{}
	err := users.UpdateTenantInfoPassword(c, userClaims, &param)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessOK(c)
}
