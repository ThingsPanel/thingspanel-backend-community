package api

import (
	"errors"
	"net/http"

	"project/internal/model"
	"project/internal/repo"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// QueryBundleInstallationOperation observes a lost-reply installation using
// its original client key. The ordinary authenticated device router owns the
// current session; the Encore owner repeats authentication and subject scope.
// @Router /api/v1/device/market/bundles/installation-operations [get]
func (*DeviceApi) QueryBundleInstallationOperation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	keys := c.Request.URL.Query()["operationKey"]
	if len(keys) != 1 {
		c.Error(errcode.NewWithMessage(errcode.CodeParamError, "one original operationKey is required"))
		return
	}
	value, _ := c.Get("claims")
	claims, _ := value.(*utils.UserClaims)
	observation, err := service.GetSelectedMarketInstallationByKey(c.Request.Context(), keys[0], claims, c.GetHeader("x-token"))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", observation)
}

// Registered only inside RegisterMarketInstallProviderRoutes, behind its
// existing deployment service credential and outside request-body logging.
// No browser-supplied tenant/subject is accepted without that trusted boundary.
func handleMarketInstallationReadAuthorization(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var identity model.MarketInstallationReadIdentity
	if err := c.ShouldBindJSON(&identity); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"errorCode": "INVALID_INSTALLATION_READ_AUTHORIZATION"})
		return
	}
	decision, err := service.AuthorizeCurrentMarketInstallationManagement(c.Request.Context(), identity)
	if err != nil {
		status := http.StatusServiceUnavailable
		code := "INSTALLATION_READ_AUTHORIZATION_UNKNOWN"
		if errors.Is(err, repo.ErrMarketInstallUnauthorized) {
			status = http.StatusForbidden
			code = "INSTALLATION_READ_AUTHORIZATION_DENIED"
		}
		c.JSON(status, gin.H{"errorCode": code})
		return
	}
	c.JSON(http.StatusOK, decision)
}
