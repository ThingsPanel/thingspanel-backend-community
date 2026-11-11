package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"project/internal/model"
	"project/internal/repo"
	"project/internal/service"
)

// RegisterMarketInstallProviderRoutes is mounted before body/operation logging.
// These purpose-limited machine routes require a dedicated deployment service
// credential. Plan/execution routes also require a currently valid immutable
// installation grant. read-authorize instead checks the current read permission
// of a native-authenticated principal; it neither renews nor issues that grant.
func RegisterMarketInstallProviderRoutes(router *gin.Engine) {
	authenticated := func(c *gin.Context) bool {
		configured := os.Getenv("TP_MARKETINSTALL_PROVIDER_TOKEN")
		presented := c.GetHeader("X-TP-Service-Token")
		if os.Getenv("TP_MARKETINSTALL_OWNER") != "encore" || len(configured) < 32 {
			c.AbortWithStatusJSON(503, gin.H{"errorCode": "INSTALLATION_PROVIDER_DISABLED"})
			return false
		}
		if len(configured) != len(presented) || subtle.ConstantTimeCompare([]byte(configured), []byte(presented)) != 1 {
			c.AbortWithStatusJSON(403, gin.H{"errorCode": "INSTALLATION_PROVIDER_DENIED"})
			return false
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10*1024*1024)
		c.Header("Cache-Control", "no-store")
		return true
	}
	failure := func(c *gin.Context, err error) {
		status := 503
		code := "INSTALLATION_PROVIDER_UNKNOWN"
		if errors.Is(err, repo.ErrMarketInstallUnauthorized) {
			status = 403
			code = "INSTALLATION_AUTHORIZATION_DENIED"
		}
		c.JSON(status, gin.H{"errorCode": code})
	}
	router.POST("/internal/market-install/read-authorize", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !authenticated(c) {
			return
		}
		handleMarketInstallationReadAuthorization(c)
	})
	router.POST("/internal/market-install/prepare", func(c *gin.Context) {
		if !authenticated(c) {
			return
		}
		var req model.MarketInstallPrepareRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"errorCode": "INVALID_INSTALLATION_REQUEST"})
			return
		}
		plan, err := service.ReadMarketInstallPreparedPlan(c.Request.Context(), req)
		if err != nil {
			failure(c, err)
			return
		}
		c.JSON(200, plan)
	})
	router.POST("/internal/market-install/authorize", func(c *gin.Context) {
		if !authenticated(c) {
			return
		}
		var req model.MarketInstallStepRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"errorCode": "INVALID_INSTALLATION_REQUEST"})
			return
		}
		if _, err := service.AuthorizeMarketInstallStep(c.Request.Context(), req); err != nil {
			failure(c, err)
			return
		}
		c.JSON(200, gin.H{"authorized": true, "tenantId": req.TenantID, "subjectId": req.SubjectID, "payloadHash": req.PayloadHash})
	})
	for _, mode := range []string{"query", "execute"} {
		execute := mode == "execute"
		router.POST("/internal/market-install/"+mode, func(c *gin.Context) {
			if !authenticated(c) {
				return
			}
			var req model.MarketInstallStepRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"errorCode": "INVALID_INSTALLATION_REQUEST"})
				return
			}
			result, err := service.NewMarketInstallProvider().Step(c.Request.Context(), req, execute)
			if err != nil {
				failure(c, err)
				return
			}
			c.JSON(200, result)
		})
	}
}
