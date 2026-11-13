package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const automationDeviceLookupLimit = 200

// automationDeviceLookup is a bounded, exact-ID read using the authenticated
// service tenant. It does not accept a tenant selector or fall back to listing.
func automationDeviceLookup(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			IDs []string `json:"ids"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil || len(body.IDs) == 0 || len(body.IDs) > automationDeviceLookupLimit {
			c.JSON(400, gin.H{"code": 400, "message": "invalid device IDs"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			c.JSON(400, gin.H{"code": 400, "message": "invalid request body"})
			return
		}
		ids := make([]string, 0, len(body.IDs))
		seen := map[string]bool{}
		for _, id := range body.IDs {
			if id == "" || len(id) > 36 || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
				c.JSON(400, gin.H{"code": 400, "message": "invalid device IDs"})
				return
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if db == nil {
			c.JSON(503, gin.H{"code": 503})
			return
		}
		tenant := c.GetString("automation_owner_tenant")
		if tenant == "" {
			c.JSON(401, gin.H{"code": 401})
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
		query := db.WithContext(c.Request.Context()).Table("devices AS d").
			Where("d.tenant_id=? AND d.is_enabled='enabled' AND d.id IN ?", tenant, ids).
			Joins("LEFT JOIN device_configs c ON c.id=d.device_config_id AND c.tenant_id=d.tenant_id").
			Select("d.id,d.tenant_id,coalesce(d.name,'') AS name,coalesce(d.label,'') AS label,coalesce(d.device_config_id,'') AS device_config_id,coalesce(c.device_type,'1') AS device_type,d.is_online").
			Order("d.id").Limit(len(ids))
		if err := query.Scan(&list).Error; err != nil {
			c.JSON(500, gin.H{"code": 500, "message": "device lookup unavailable"})
			return
		}
		c.JSON(200, gin.H{"code": 200, "data": gin.H{"list": list, "total": len(list)}})
	}
}
