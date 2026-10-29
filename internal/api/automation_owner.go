package api

import (
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"project/internal/automationbridge"
	"project/internal/brokercommands"
	"project/internal/service"
	"strconv"
)

// RegisterAutomationOwnerRoutes deliberately runs service authentication before
// business processing. It grants no browser session or general community API access.
func RegisterAutomationOwnerRoutes(router *gin.Engine, db *gorm.DB) {
	group := router.Group("/internal/automation", func(c *gin.Context) {
		tenant, err := automationbridge.Authenticate(c.Request)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid automation service identity"})
			return
		}
		c.Set("automation_owner_tenant", tenant)
		c.Next()
	})
	group.GET("/devices", func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		size, _ := strconv.Atoi(c.DefaultQuery("page_size", "200"))
		if page < 1 || size < 1 || size > 200 {
			c.JSON(400, gin.H{"code": 400})
			return
		}
		if db == nil {
			c.JSON(503, gin.H{"code": 503})
			return
		}
		tenant := c.GetString("automation_owner_tenant")
		query := db.WithContext(c.Request.Context()).Table("devices AS d").Where("d.tenant_id=? AND d.is_enabled='enabled'", tenant)
		var total int64
		if query.Count(&total).Error != nil {
			c.JSON(500, gin.H{"code": 500})
			return
		}
		var list []struct {
			ID             string `json:"id"`
			TenantID       string `json:"tenant_id"`
			Name           string `json:"name"`
			Label          string `json:"label"`
			DeviceConfigID string `json:"device_config_id"`
			DeviceType     string `json:"device_type"`
			IsOnline       int    `json:"is_online"`
		}
		err := query.Joins("LEFT JOIN device_configs c ON c.id=d.device_config_id AND c.tenant_id=d.tenant_id").Select("d.id,d.tenant_id,coalesce(d.name,'') AS name,coalesce(d.label,'') AS label,coalesce(d.device_config_id,'') AS device_config_id,coalesce(c.device_type,'1') AS device_type,d.is_online").Order("d.id").Offset((page - 1) * size).Limit(size).Scan(&list).Error
		if err != nil {
			c.JSON(500, gin.H{"code": 500})
			return
		}
		c.JSON(200, gin.H{"code": 200, "data": gin.H{"list": list, "total": total}})
	})
	group.POST("/api/v1/:kind/datas/pub", func(c *gin.Context) {
		var body struct {
			DeviceID string `json:"device_id"`
			Value    string `json:"value"`
			Identify string `json:"identify"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		if c.ShouldBindJSON(&body) != nil {
			c.JSON(400, gin.H{"code": 400})
			return
		}
		generation, _ := strconv.ParseInt(c.GetHeader("X-Automation-Generation"), 10, 64)
		id, err := service.SubmitNativeAutomationCommand(c.Request.Context(), service.NativeCommand{TenantID: c.GetString("automation_owner_tenant"), DeploymentID: automationbridge.Deployment(), Kind: c.Param("kind"), DeviceID: body.DeviceID, Identifier: body.Identify, Value: body.Value, Key: c.GetHeader("X-Idempotency-Key"), Scope: c.GetHeader("X-Automation-Scope"), Generation: generation})
		if errors.Is(err, service.ErrNativeSuperseded) {
			c.Header("X-Automation-Superseded", "true")
			c.JSON(409, gin.H{"code": 4090, "message": "generation superseded"})
			return
		}
		if errors.Is(err, brokercommands.ErrConflict) {
			c.JSON(409, gin.H{"code": 4091, "message": "idempotency conflict"})
			return
		}
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, brokercommands.ErrInvalid) || errors.Is(err, brokercommands.ErrRejected) || errors.Is(err, brokercommands.ErrFenced) || errors.Is(err, gorm.ErrRecordNotFound) {
				status = http.StatusBadRequest
			}
			_ = c.Error(err)
			c.JSON(status, gin.H{"code": status, "message": "native command not confirmed"})
			return
		}
		c.JSON(200, gin.H{"code": 200, "data": gin.H{"command_id": id}})
	})
}
