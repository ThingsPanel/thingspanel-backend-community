package api

import (
	"project/internal/model"
	"project/internal/service"
	"project/pkg/constant"
	"project/pkg/utils"
	"strconv"

	"github.com/gin-gonic/gin"
)

type AttributeDataApi struct{}

// GetDataList 设备属性列表查询
// @Router   /api/v1/attribute/datas/{id} [get]
func (*AttributeDataApi) HandleDataList(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.AttributeData.GetAttributeDataList(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// 根据key查询设备属性
// /api/v1/attribute/datas/key [get]
func (*AttributeDataApi) HandleAttributeDataByKey(c *gin.Context) {
	var req model.GetDataListByKeyReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.AttributeData.GetAttributeDataByKey(req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// DeleteData 删除数据
// @Router   /api/v1/attribute/datas/{id} [delete]
func (*AttributeDataApi) DeleteData(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.AttributeData.DeleteAttributeData(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetAttributeSetLogsDataListByPage 属性下发记录查询（分页）
// @Router   /api/v1/attribute/datas/set/logs [get]
func (*AttributeDataApi) HandleAttributeSetLogsDataListByPage(c *gin.Context) {
	var req model.GetAttributeSetLogsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.AttributeData.GetAttributeSetLogsDataListByPage(req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/attribute/datas/pub [post]
func (*AttributeDataApi) AttributePutMessage(c *gin.Context) {
	var req model.AttributePutMessage
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.AttributeData.AttributePutMessage(c, userClaims.ID, &req, strconv.Itoa(constant.Manual))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// 发送获取属性请求
// /api/v1/attribute/datas/get
func (*AttributeDataApi) AttributeGetMessage(c *gin.Context) {
	var req model.AttributeGetMessageReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.AttributeData.AttributeGetMessage(userClaims, &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}
