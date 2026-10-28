package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"project/internal/dal"
)

func (r *NotificationMemberReaderApi) appScope(c *gin.Context) (string, bool) {
	if r == nil || !r.enabled {
		readerStatus(c, 404)
		return "", false
	}
	if !r.authenticate(c.Request) {
		readerStatus(c, 401)
		return "", false
	}
	if !r.authorizeDeployment(c.Request) {
		readerStatus(c, 403)
		return "", false
	}
	q := c.Request.URL.Query()
	tenant := q.Get("tenantId")
	if c.Request.Method != "GET" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) > 0 || len(q) != 1 || len(q["tenantId"]) != 1 || !validReaderScopeID(tenant) || !validReaderScopeID(c.Param("id")) {
		readerStatus(c, 400)
		return "", false
	}
	if _, ok := r.allowedTenants[tenant]; !ok {
		readerStatus(c, 403)
		return "", false
	}
	c.Header("Cache-Control", "no-store")
	return tenant, true
}

func (r *NotificationMemberReaderApi) GetAppMemberDevices(c *gin.Context) {
	tenant, ok := r.appScope(c)
	if !ok {
		return
	}
	rows, err := dal.ReadAppPushDevices(c.Request.Context(), tenant, c.Param("id"), "")
	if err != nil {
		readerStatus(c, 503)
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	c.JSON(http.StatusOK, gin.H{"tenantId": tenant, "userId": c.Param("id"), "deviceIds": ids})
}
func (r *NotificationMemberReaderApi) GetAppDevice(c *gin.Context) {
	tenant, ok := r.appScope(c)
	if !ok {
		return
	}
	rows, err := dal.ReadAppPushDevices(c.Request.Context(), tenant, "", c.Param("id"))
	if err != nil {
		readerStatus(c, 503)
		return
	}
	if len(rows) != 1 {
		readerStatus(c, 404)
		return
	}
	c.JSON(http.StatusOK, rows[0])
}
