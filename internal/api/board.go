package api

import (
	model "project/internal/model"
	service "project/internal/service"
	common "project/pkg/common"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type BoardApi struct{}

// CreateBoard 创建看板
// @Router   /api/v1/board [post]
func (*BoardApi) CreateBoard(c *gin.Context) {
	var req model.CreateBoardReq
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID

	boardInfo, err := service.GroupApp.Board.CreateBoard(c, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", boardInfo)
}

// UpdateBoard 更新看板
// @Router   /api/v1/board [put]
func (*BoardApi) UpdateBoard(c *gin.Context) {
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
		c.Error(err)
		return
	}

	c.Set("data", d)
}

// DeleteBoard 删除看板
// @Router   /api/v1/board/{id} [delete]
func (*BoardApi) DeleteBoard(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.Board.DeleteBoard(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetBoardListByPage 看板分页查询
// @Router   /api/v1/board [get]
func (*BoardApi) HandleBoardListByPage(c *gin.Context) {
	var req model.GetBoardListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	boardList, err := service.GroupApp.Board.GetBoardListByPage(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", boardList)
}

// GetBoard 看板详情查询
// @Router   /api/v1/board/{id} [get]
func (*BoardApi) HandleBoard(c *gin.Context) {
	id := c.Param("id")
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	board, err := service.GroupApp.Board.GetBoard(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", board)
}

// GetBoardListByTenantId 首页看板查询
// @Router   /api/v1/board/home [get]
func (*BoardApi) HandleBoardListByTenantId(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	boardList, err := service.GroupApp.Board.GetBoardListByTenantId(userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", boardList)
}

// GetDeviceTotal 设备总数
// @Router   /api/v1/board/device/total [get]
func (*BoardApi) HandleDeviceTotal(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	board := service.GroupApp.Board
	total, err := board.GetDeviceTotal(c, userClaims.Authority, userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", total)
}

// GetDevice 设备信息
// @Router   /api/v1/board/device [get]
func (*BoardApi) HandleDevice(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	if !common.CheckUserIsAdmin(userClaims.Authority) {
		c.Error(errcode.New(201001)) // "无访问权限" / "Access Denied"
		return
	}

	board := service.GroupApp.Board
	data, err := board.GetDevice(c)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetTenant 租户信息
// @Router   /api/v1/board/tenant [get]
func (*BoardApi) HandleTenant(c *gin.Context) {
	//TODO::不知道需不需要再次验证用户信息
	users := service.UsersService{}
	data, err := users.GetTenant(c)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetTenantUserInfo 租户下用户信息
// @Router   /api/v1/board/tenant/user/info [get]
func (*BoardApi) HandleTenantUserInfo(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	tenantID := userClaims.TenantID
	// 根据租户ID查询租户信息
	tenantInfo, err := service.GroupApp.User.GetTenantInfo(tenantID)
	if err != nil {
		c.Error(err)
		return
	}
	users := service.UsersService{}
	data, err := users.GetTenantUserInfo(c, tenantInfo.Email)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// GetTenantDeviceInfo 租户下设备信息
// @Router   /api/v1/board/tenant/device/info [get]
func (*BoardApi) HandleTenantDeviceInfo(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)

	board := service.GroupApp.Board
	total, err := board.GetDeviceByTenantID(c, userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", total)
}

// GetUserInfo 个人信息查询
// @Router   /api/v1/board/user/info [get]
func (*BoardApi) HandleUserInfo(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	tenantID := userClaims.TenantID
	// 根据租户ID查询租户信息
	tenantInfo, err := service.GroupApp.User.GetTenantInfo(tenantID)
	if err != nil {
		c.Error(err)
		return
	}

	users := service.UsersService{}
	data, err := users.GetTenantInfo(c, tenantInfo.Email)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// UpdateUserInfo 更新个人信息
// @Router   /api/v1/board/user/update [post]
func (*BoardApi) UpdateUserInfo(c *gin.Context) {
	var param model.UsersUpdateReq
	if !BindAndValidate(c, &param) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)

	users := service.UsersService{}
	err := users.UpdateTenantInfo(c, userClaims, &param)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// UpdateUserInfoPassword 更新个人密码
// @Router   /api/v1/board/user/update/password [post]
func (*BoardApi) UpdateUserInfoPassword(c *gin.Context) {
	var param model.UsersUpdatePasswordReq
	if !BindAndValidate(c, &param) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)

	users := service.UsersService{}
	err := users.UpdateTenantInfoPassword(c, userClaims, &param)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetDeviceTrend 获取设备在线趋势
// @Router   /api/v1/board/trend [get]
func (*BoardApi) GetDeviceTrend(c *gin.Context) {
	var deviceTrendReq model.DeviceTrendReq
	if !BindAndValidate(c, &deviceTrendReq) {
		return
	}

	// 获取用户claims
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	// 如果请求中没有指定tenantID,则使用当前用户的tenantID
	if deviceTrendReq.TenantID == nil || *deviceTrendReq.TenantID == "" {
		deviceTrendReq.TenantID = &userClaims.TenantID
	}

	// 权限检查 - 只有系统管理员可以查看其他租户的数据
	if *deviceTrendReq.TenantID != userClaims.TenantID && userClaims.Authority != "SYS_ADMIN" {
		c.Error(errcode.New(errcode.CodeNoPermission))
		return
	}

	// 调用service层获取趋势数据
	trend, err := service.GroupApp.Device.GetDeviceTrend(c, *deviceTrendReq.TenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", trend)
}
