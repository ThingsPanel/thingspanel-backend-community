package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"project/internal/query"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

var factoryID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
var factoryGrantID = regexp.MustCompile(`^[1-9][0-9]*$`)

func factoryAdmin(c *gin.Context) bool {
	claims, ok := c.Get("claims")
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
		return false
	}
	user, ok := claims.(*utils.UserClaims)
	if !ok || (user.Authority != "SYS_ADMIN" && user.TenantID == "") {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "无出厂预制管理权限"})
		return false
	}
	return true
}

func factoryOwnedProduct(c *gin.Context, base, token string, payload interface{}) bool {
	user := c.MustGet("claims").(*utils.UserClaims)
	if user.Authority == "SYS_ADMIN" {
		return true
	}
	productKey := c.Query("productKey")
	if body, ok := payload.(map[string]interface{}); ok {
		productKey, _ = body["productKey"].(string)
	}
	if batchID := c.Param("batchId"); batchID != "" {
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet,
			base+"/api/v1/internal/factory-batches/"+url.PathEscape(batchID), nil)
		if err != nil {
			c.AbortWithStatus(http.StatusBadGateway)
			return false
		}
		req.Header.Set("X-Yomi-Internal-Token", token)
		resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if err != nil {
			c.AbortWithStatus(http.StatusBadGateway)
			return false
		}
		defer resp.Body.Close()
		var result struct {
			Code int `json:"code"`
			Data struct {
				ProductKey string `json:"productKey"`
			} `json:"data"`
		}
		if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result) != nil || result.Code != 0 {
			c.AbortWithStatus(http.StatusBadGateway)
			return false
		}
		productKey = result.Data.ProductKey
	}
	if !factoryID.MatchString(productKey) {
		c.AbortWithStatus(http.StatusForbidden)
		return false
	}
	q := query.Product
	_, err := q.WithContext(c.Request.Context()).Where(q.ProductKey.Eq(productKey), q.TenantID.Eq(user.TenantID)).First()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "无此产品的出厂预制管理权限"})
		return false
	}
	return true
}

func factoryBase() (string, string, error) {
	base := strings.TrimRight(os.Getenv("YOMI_FACTORY_API_BASE_URL"), "/")
	token := os.Getenv("YOMI_INTERNAL_TOKEN")
	if token == "" {
		token = os.Getenv("YOMI_INTERNAL_EVENT_TOKEN")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || token == "" {
		return "", "", errors.New("出厂预制内部服务未配置")
	}
	privateHTTP := parsed.Scheme == "http" && (parsed.Hostname() == "yomi-server" || parsed.Hostname() == "host.docker.internal" || parsed.Hostname() == "127.0.0.1")
	if parsed.Scheme != "https" && !privateHTTP {
		return "", "", errors.New("出厂预制内部服务地址不安全")
	}
	return base, token, nil
}

func proxyFactory(c *gin.Context, method, path string, payload interface{}) {
	if !factoryAdmin(c) {
		return
	}
	base, token, err := factoryBase()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"message": err.Error()})
		return
	}
	if !factoryOwnedProduct(c, base, token, payload) {
		return
	}
	var body io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": "请求无效"})
			return
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), method, base+"/api/v1/internal/factory-batches"+path, body)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"message": "出厂预制服务不可用"})
		return
	}
	req.Header.Set("X-Yomi-Internal-Token", token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"message": "出厂预制服务不可用"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		status, message := http.StatusBadGateway, "出厂预制操作失败"
		switch resp.StatusCode {
		case http.StatusConflict:
			status, message = http.StatusConflict, "批次编号或设备编号已被占用"
		case http.StatusUnprocessableEntity:
			status, message = http.StatusUnprocessableEntity, "出厂预制参数无效"
		case http.StatusNotFound:
			status, message = http.StatusNotFound, "出厂预制记录不存在"
		case http.StatusServiceUnavailable:
			status, message = http.StatusServiceUnavailable, "出厂预制服务暂不可用"
		}
		c.AbortWithStatusJSON(status, gin.H{"message": message})
		return
	}
	var result struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil || result.Code != 0 {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"message": "出厂预制操作失败"})
		return
	}
	var data interface{}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"message": "出厂预制响应无效"})
		return
	}
	c.Set("data", data)
}

func factoryBatchPath(c *gin.Context) (string, bool) {
	id := c.Param("batchId")
	if !factoryID.MatchString(id) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": "批次编号无效"})
		return "", false
	}
	return "/" + url.PathEscape(id), true
}

func readFactoryBody(c *gin.Context) (map[string]interface{}, bool) {
	var body map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 16<<10)).Decode(&body); err != nil || body == nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": "请求无效"})
		return nil, false
	}
	return body, true
}

func (*ProductApi) ListFactoryBatches(c *gin.Context) {
	product := c.Query("productKey")
	if !factoryID.MatchString(product) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": "产品标识无效"})
		return
	}
	query := url.Values{"productKey": {product}, "page": {c.DefaultQuery("page", "1")}}
	proxyFactory(c, http.MethodGet, "?"+query.Encode(), nil)
}

func (*ProductApi) CreateFactoryBatch(c *gin.Context) {
	body, ok := readFactoryBody(c)
	if !ok || !factoryAdmin(c) {
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	body["createdBy"] = claims.ID
	proxyFactory(c, http.MethodPost, "", body)
}

func (*ProductApi) GetFactoryBatch(c *gin.Context) {
	path, ok := factoryBatchPath(c)
	if !ok {
		return
	}
	query := url.Values{"page": {c.DefaultQuery("page", "1")}}
	if status := c.Query("status"); status != "" {
		query.Set("status", status)
	}
	proxyFactory(c, http.MethodGet, path+"?"+query.Encode(), nil)
}

func (*ProductApi) SetFactoryBatchStatus(c *gin.Context) {
	path, ok := factoryBatchPath(c)
	if !ok {
		return
	}
	body, ok := readFactoryBody(c)
	if ok {
		proxyFactory(c, http.MethodPost, path+"/status", body)
	}
}

func (*ProductApi) IssueFactoryStationGrant(c *gin.Context) {
	path, ok := factoryBatchPath(c)
	if !ok {
		return
	}
	body, ok := readFactoryBody(c)
	if ok {
		proxyFactory(c, http.MethodPost, path+"/stations", body)
	}
}

func (*ProductApi) RevokeFactoryStationGrant(c *gin.Context) {
	path, ok := factoryBatchPath(c)
	if !ok {
		return
	}
	grantID := c.Param("grantId")
	if !factoryGrantID.MatchString(grantID) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": "工位授权编号无效"})
		return
	}
	proxyFactory(c, http.MethodPost, path+"/stations/"+grantID+"/revoke", nil)
}

func (*ProductApi) EnableFactoryUnit(c *gin.Context) {
	path, ok := factoryBatchPath(c)
	if !ok || !factoryID.MatchString(c.Param("deviceId")) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": "设备编号无效"})
		return
	}
	proxyFactory(c, http.MethodPost, path+"/units/"+url.PathEscape(c.Param("deviceId"))+"/enable", nil)
}
