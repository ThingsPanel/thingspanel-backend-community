package api

import (
	"net/http"
	"net/url"

	"project/internal/service"

	"github.com/gin-gonic/gin"
)

// GetAutomationPolicy uses the source reader's deployment and tenant boundaries.
// Empty alias means the tenant default; an explicit unavailable alias never falls back.
func (reader *NotificationMemberReaderApi) GetAutomationPolicy(c *gin.Context) {
	if reader == nil || !reader.enabled {
		readerStatus(c, http.StatusNotFound)
		return
	}
	if !reader.authenticate(c.Request) {
		readerStatus(c, http.StatusUnauthorized)
		return
	}
	if !reader.authorizeDeployment(c.Request) {
		readerStatus(c, http.StatusForbidden)
		return
	}
	tenant := c.Param("id")
	if !validReaderScopeID(tenant) {
		readerStatus(c, http.StatusBadRequest)
		return
	}
	if _, allowed := reader.allowedTenants[tenant]; !allowed {
		readerStatus(c, http.StatusForbidden)
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(query) > 1 || len(query["alias"]) > 1 || (len(query) == 1 && query["alias"] == nil) || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) > 0 {
		readerStatus(c, http.StatusBadRequest)
		return
	}
	alias := query.Get("alias")
	if alias != "" && !validReaderScopeID(alias) {
		readerStatus(c, http.StatusBadRequest)
		return
	}
	view, err := reader.policyView(c.Request.Context(), c.GetHeader("X-Notification-Deployment"), tenant)
	if err != nil {
		readerStatus(c, http.StatusServiceUnavailable)
		return
	}
	policy, ok := selectAutomationPolicy(view, alias)
	if !ok {
		readerStatus(c, http.StatusConflict)
		return
	}
	c.Header("Cache-Control", "no-store")
	if policy == nil {
		c.JSON(http.StatusOK, gin.H{"configured": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"configured": true, "policy_id": policy.NativeGroupID, "group_revision": policy.GroupRevision})
}

func selectAutomationPolicy(view service.TenantDefaultPolicyView, alias string) (*service.DefaultPolicySummary, bool) {
	if alias == "" {
		if view.Selected == nil {
			return nil, true
		}
		return view.Selected, view.Selected.Ready && view.Selected.NativeGroupID != "" && view.Selected.GroupRevision > 0
	}
	for _, p := range view.AvailablePolicies {
		if p.AliasGroupID == alias && p.Ready && p.NativeGroupID != "" && p.GroupRevision > 0 {
			return &p, true
		}
	}
	return nil, false
}
