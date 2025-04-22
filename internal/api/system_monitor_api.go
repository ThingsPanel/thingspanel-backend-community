package api

import (
	"fmt"
	"time"

	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// SystemMonitorApi 系统监控API
type SystemMonitorApi struct{}

// GetCurrentSystemMetrics 获取当前系统指标
// @Summary 获取当前系统指标
// @Description 获取系统CPU、内存、磁盘使用率的当前值
// @Tags 系统监控
// @Accept  json
// @Produce  json
// @Success 200 {object} response.Response
// @Router /api/v1/system/metrics/current [get]
func (api *SystemMonitorApi) GetCurrentSystemMetrics(c *gin.Context) {
	// 判断是否超管
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	if userClaims.Authority != "SYS_ADMIN" {
		c.Error(errcode.New(errcode.CodeNoPermission))
		return
	}

	metrics, err := service.GroupApp.SystemMonitor.GetCurrentMetrics()
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", metrics)
}

// GetHistorySystemMetrics 获取系统指标历史数据
// @Summary 获取系统指标历史数据
// @Description 获取系统CPU、内存、磁盘使用率的历史数据
// @Tags 系统监控
// @Accept  json
// @Produce  json
// @Param hours query int false "查询小时数，默认24小时" default(24)
// @Success 200 {object} response.Response
// @Router /api/v1/system/metrics/history [get]
func (api *SystemMonitorApi) GetHistorySystemMetrics(c *gin.Context) {
	// 判断是否超管
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	if userClaims.Authority != "SYS_ADMIN" {
		c.Error(errcode.New(errcode.CodeNoPermission))
		return
	}

	hours := 24
	if hoursStr := c.Query("hours"); hoursStr != "" {
		if _, err := fmt.Sscanf(hoursStr, "%d", &hours); err != nil {
			hours = 24
		}
	}

	// 限制查询范围
	if hours <= 0 {
		hours = 1
	} else if hours > 72 {
		hours = 72
	}

	duration := time.Duration(hours) * time.Hour
	data, err := service.GroupApp.SystemMonitor.GetCombinedHistoryData(duration)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}
