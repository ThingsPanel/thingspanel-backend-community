package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"project/internal/northbound"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// NorthboundSourceApi exposes the Community-side view of the northbound
// handoff. Configuration, authorization and delivery accounting live in the
// northbound service; this handler only reports what the local outbox knows and
// forwards management calls unchanged.
type NorthboundSourceApi struct{}

// northboundRoles are the roles that may read the source-side status. It mirrors
// the northbound service's view permission; it is not a second permission
// system, only a guard on a local read-only endpoint.
func northboundRoleAllowed(authority string) bool {
	switch strings.ToUpper(authority) {
	case "TENANT_ADMIN", "TENANT_OPERATOR", "TENANT_VIEWER", "SYS_ADMIN", "PLATFORM_ADMIN":
		return true
	}
	return false
}

// HandleSourceStatus 返回北向源端（社区运行侧）的登记与交接状态
// @Router   /api/v1/northbound-source/status [get]
func (*NorthboundSourceApi) HandleSourceStatus(c *gin.Context) {
	claims, ok := c.Get("claims")
	if !ok {
		c.Error(errcode.New(errcode.CodeUnauthorized))
		return
	}
	userClaims, ok := claims.(*utils.UserClaims)
	if !ok || userClaims == nil || userClaims.TenantID == "" {
		c.Error(errcode.New(errcode.CodeUnauthorized))
		return
	}
	if !northboundRoleAllowed(userClaims.Authority) {
		c.JSON(http.StatusForbidden, gin.H{"error": "当前角色不能查看数据转发"})
		return
	}
	c.Set("data", northbound.LoadSourceStatus(c.Request.Context(), global.DB))
}

// HandleProxy 把 /api/v1/northbound/* 转发给北向服务。
//
// 这里不注入身份、不做本地授权判定：调用方的社区令牌原样交给北向服务，由它的
// 认证与授权引擎做唯一判定。代理只做两件事：解决浏览器同源问题，以及把社区习惯的
// x-token 头翻译成北向服务认证处理器读取的 Authorization 头。
// @Router   /api/v1/northbound/{path} [any]
func (*NorthboundSourceApi) HandleProxy(c *gin.Context) {
	cfg := northbound.Current()
	if cfg.EncoreBaseURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "数据转发服务未配置：northbound.encore_base_url 为空",
		})
		return
	}
	target, err := url.Parse(strings.TrimRight(cfg.EncoreBaseURL, "/"))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据转发服务地址无效"})
		return
	}

	// The Community frontend authenticates with x-token, while the northbound
	// service's auth handler reads the standard Authorization header. Resolve
	// that difference once, here, instead of asking every page to send both.
	// The value is the caller's own token: nothing is minted or elevated.
	callerToken := strings.TrimSpace(c.GetHeader("x-token"))
	if callerToken == "" {
		callerToken = strings.TrimSpace(c.GetHeader("Authorization"))
	}
	if callerToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少访问令牌"})
		return
	}
	if !strings.HasPrefix(strings.ToLower(callerToken), "bearer ") {
		callerToken = "Bearer " + callerToken
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// /api/v1/northbound/connections -> {encore}/v1/connections
		// The northbound service owns the /v1 prefix; the browser-facing path
		// keeps the community API shape.
		req.URL.Path = "/v1" + strings.TrimPrefix(c.Request.URL.Path, "/api/v1/northbound")
		req.URL.RawQuery = c.Request.URL.RawQuery
		req.Host = target.Host
		req.Header.Set("Authorization", callerToken)
		req.Header.Del("x-token")
		req.Header.Del("Cookie")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"数据转发服务不可达，请检查 northbound 服务是否已启动"}`))
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}
