// internal/service/open_api_keys.go
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"

	"project/internal/dal"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

type OpenAPIKey struct{}

// CreateOpenAPIKey 创建OpenAPI密钥
func (o *OpenAPIKey) CreateOpenAPIKey(req *model.CreateOpenAPIKeyReq, claims *utils.UserClaims) error {
	// 校验用户权限
	if claims.Authority != "SYS_ADMIN" && claims.Authority != "TENANT_ADMIN" {
		return errcode.WithVars(errcode.CodeNoPermission, map[string]interface{}{
			"required_role": "SYS_ADMIN or TENANT_ADMIN",
			"current_role":  claims.Authority,
		})
	}

	// 租户管理员只能创建自己租户的密钥
	if claims.Authority != "SYS_ADMIN" && claims.TenantID != req.TenantID {
		return errcode.WithVars(errcode.CodeNoPermission, map[string]interface{}{
			"required_tenant": req.TenantID,
			"current_tenant":  claims.TenantID,
		})
	}

	// 生成APIKey
	apikey, err := utils.GenerateAPIKey()
	if err != nil {
		logrus.Errorf("生成AppSecret失败: %v", err)
		return errcode.New(errcode.CodeSystemError)
	}

	status := int16(1) // 默认启用
	// 创建OpenAPI密钥记录
	key := &model.OpenAPIKey{
		ID:        uuid.New(),
		TenantID:  req.TenantID,
		APIKey:    apikey,
		Status:    &status,
		Name:      req.Name,
		CreatedID: &claims.ID,
	}

	t := time.Now().UTC()
	key.CreatedAt = &t
	key.UpdatedAt = &t

	if err := dal.CreateOpenAPIKey(key); err != nil {
		logrus.Error("创建OpenAPI密钥失败")
		return errcode.New(errcode.CodeDBError)
	}

	return nil
}

// GetOpenAPIKeyList 获取OpenAPI密钥列表
func (o *OpenAPIKey) GetOpenAPIKeyList(req *model.OpenAPIKeyListReq, claims *utils.UserClaims) (map[string]interface{}, error) {
	var tenantID string
	// 租户管理员只能查看自己租户的密钥
	if claims.Authority == "TENANT_ADMIN" || claims.Authority == "TENANT_USER" {
		tenantID = claims.TenantID
	}

	total, list, err := dal.GetOpenAPIKeyListByPage(req, tenantID)
	if err != nil {
		logrus.Errorf("查询OpenAPI密钥列表失败: %v", err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	result := make(map[string]interface{})
	result["total"] = total
	result["list"] = list
	return result, nil
}

func apiKeyWriteScope(claims *utils.UserClaims) (string, bool, error) {
	if claims == nil || strings.TrimSpace(claims.ID) == "" {
		return "", false, errcode.New(errcode.CodeNoPermission)
	}
	if claims.Authority == "SYS_ADMIN" {
		return "", true, nil
	}
	if claims.Authority == "TENANT_ADMIN" && strings.TrimSpace(claims.TenantID) != "" {
		return claims.TenantID, false, nil
	}
	return "", false, errcode.New(errcode.CodeNoPermission)
}

func apiKeyWriteError(err error) error {
	switch {
	case errors.Is(err, dal.ErrAPIKeyWriteForbidden):
		return errcode.New(errcode.CodeNoPermission)
	case errors.Is(err, dal.ErrAPIKeyWriteUnknown):
		return errcode.NewWithMessage(errcode.CodeDBError, "api key change outcome unknown; reconcile before retry")
	case errors.Is(err, dal.ErrAPIKeyWriteNotExecuted):
		return errcode.NewWithMessage(errcode.CodeDBError, "api key change not executed")
	default:
		return errcode.New(errcode.CodeDBError)
	}
}

// UpdateOpenAPIKey 更新OpenAPI密钥
func (o *OpenAPIKey) UpdateOpenAPIKey(ctx context.Context, req *model.UpdateOpenAPIKeyReq, claims *utils.UserClaims) error {
	tenant, allTenants, err := apiKeyWriteScope(claims)
	if err != nil {
		return err
	}
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errcode.New(errcode.CodeNoPermission)
	}
	if req.Status != nil && *req.Status != 0 && *req.Status != 1 {
		return errcode.New(errcode.CodeParamError)
	}

	// 构建更新内容
	updates := make(map[string]interface{})
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}

	// 执行更新
	if ctx == nil {
		return apiKeyWriteError(dal.ErrAPIKeyWriteNotExecuted)
	}
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := dal.MutateOpenAPIKey(dbCtx, global.DB, req.ID, tenant, allTenants, false, updates); err != nil {
		return apiKeyWriteError(err)
	}
	return nil
}

// DeleteOpenAPIKey 删除OpenAPI密钥
func (o *OpenAPIKey) DeleteOpenAPIKey(ctx context.Context, id string, claims *utils.UserClaims) error {
	tenant, allTenants, err := apiKeyWriteScope(claims)
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errcode.New(errcode.CodeNoPermission)
	}
	if ctx == nil {
		return apiKeyWriteError(dal.ErrAPIKeyWriteNotExecuted)
	}
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := dal.MutateOpenAPIKey(dbCtx, global.DB, id, tenant, allTenants, true, nil); err != nil {
		return apiKeyWriteError(err)
	}
	if global.REDIS != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		if err := global.REDIS.Del(cleanupCtx, "openapi:key:"+id).Err(); err != nil {
			logrus.Warn("API key cache cleanup failed")
		}
	}
	return nil
}
