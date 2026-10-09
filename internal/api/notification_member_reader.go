package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"project/internal/dal"

	"github.com/gin-gonic/gin"
)

type NotificationMemberReaderConfig struct {
	Token                string
	AllowedDeploymentIDs []string
	AllowedTenantIDs     []string
}

type NotificationMemberReaderApi struct {
	tokenDigest        [sha256.Size]byte
	enabled            bool
	allowedDeployments map[string]struct{}
	allowedTenants     map[string]struct{}
}

func NewNotificationMemberReader(config NotificationMemberReaderConfig) *NotificationMemberReaderApi {
	reader := &NotificationMemberReaderApi{
		allowedDeployments: make(map[string]struct{}, len(config.AllowedDeploymentIDs)),
		allowedTenants:     make(map[string]struct{}, len(config.AllowedTenantIDs)),
	}
	if len(config.Token) < 32 || strings.TrimSpace(config.Token) != config.Token {
		return reader
	}
	for _, id := range config.AllowedDeploymentIDs {
		if !validReaderScopeID(id) {
			return reader
		}
		reader.allowedDeployments[id] = struct{}{}
	}
	for _, id := range config.AllowedTenantIDs {
		if !validReaderScopeID(id) {
			return reader
		}
		reader.allowedTenants[id] = struct{}{}
	}
	if len(reader.allowedDeployments) == 0 || len(reader.allowedTenants) == 0 {
		return reader
	}
	reader.tokenDigest = sha256.Sum256([]byte(config.Token))
	reader.enabled = true
	return reader
}

func validReaderScopeID(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:-", char) {
			continue
		}
		return false
	}
	return true
}

// GetMemberContact is mounted only at the private notification source-member
// route before the legacy operation-log middleware.
func (reader *NotificationMemberReaderApi) GetMemberContact(c *gin.Context) {
	if reader == nil || !reader.enabled {
		readerStatus(c, http.StatusNotFound)
		return
	}
	if c.Request.Method != http.MethodGet {
		readerStatus(c, http.StatusMethodNotAllowed)
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
	if c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) > 0 {
		readerStatus(c, http.StatusBadRequest)
		return
	}
	query, ok := parseMemberReaderQuery(c.Request.URL.RawQuery)
	if !ok {
		readerStatus(c, http.StatusBadRequest)
		return
	}
	if _, allowed := reader.allowedTenants[query.tenantID]; !allowed {
		readerStatus(c, http.StatusForbidden)
		return
	}
	userID := c.Param("id")
	if !validReaderScopeID(userID) || (query.contactField != "email" && query.contactField != "phone") {
		readerStatus(c, http.StatusBadRequest)
		return
	}
	contact, err := dal.ResolveNotificationMemberContact(c.Request.Context(), userID, query.tenantID, query.contactField)
	if err != nil {
		if err == dal.ErrNotificationMemberNotFound {
			readerStatus(c, http.StatusNotFound)
			return
		}
		readerStatus(c, http.StatusServiceUnavailable)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"userId":       contact.UserID,
		"tenantId":     contact.TenantID,
		"contactField": contact.ContactField,
		"address":      contact.Address,
	})
}

// GetTenantPresence exposes only active-tenant existence to the same private
// source deployment identity. It intentionally accepts no query parameters.
func (reader *NotificationMemberReaderApi) GetTenantPresence(c *gin.Context) {
	if reader == nil || !reader.enabled {
		readerStatus(c, http.StatusNotFound)
		return
	}
	if c.Request.Method != http.MethodGet {
		readerStatus(c, http.StatusMethodNotAllowed)
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
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) > 0 {
		readerStatus(c, http.StatusBadRequest)
		return
	}
	tenantID := c.Param("id")
	if !validReaderScopeID(tenantID) {
		readerStatus(c, http.StatusNotFound)
		return
	}
	if _, allowed := reader.allowedTenants[tenantID]; !allowed {
		readerStatus(c, http.StatusForbidden)
		return
	}
	presence, err := dal.ResolveNotificationTenant(c.Request.Context(), tenantID)
	if err != nil {
		if err == dal.ErrNotificationMemberNotFound {
			readerStatus(c, http.StatusNotFound)
			return
		}
		readerStatus(c, http.StatusServiceUnavailable)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"tenantId": presence.TenantID, "exists": true})
}

func (reader *NotificationMemberReaderApi) authenticate(request *http.Request) bool {
	values := request.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > 8192 || !strings.HasPrefix(values[0], "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	if token == "" || strings.TrimSpace(token) != token {
		return false
	}
	digest := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(reader.tokenDigest[:], digest[:]) == 1
}

func (reader *NotificationMemberReaderApi) authorizeDeployment(request *http.Request) bool {
	values := request.Header.Values("X-Notification-Deployment")
	if len(values) != 1 || !validReaderScopeID(values[0]) {
		return false
	}
	_, allowed := reader.allowedDeployments[values[0]]
	return allowed
}

type memberReaderQuery struct {
	tenantID     string
	contactField string
}

func parseMemberReaderQuery(rawQuery string) (memberReaderQuery, bool) {
	var query memberReaderQuery
	if rawQuery == "" || len(rawQuery) > 1024 || strings.HasSuffix(rawQuery, "&") || strings.Contains(rawQuery, "&&") {
		return query, false
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil || len(values) != 2 || len(values["tenantId"]) != 1 || len(values["contactField"]) != 1 {
		return query, false
	}
	query.tenantID = values.Get("tenantId")
	query.contactField = values.Get("contactField")
	if !validReaderScopeID(query.tenantID) || (query.contactField != "email" && query.contactField != "phone") {
		return memberReaderQuery{}, false
	}
	return query, true
}

func readerStatus(c *gin.Context, status int) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{"error": http.StatusText(status)})
}
