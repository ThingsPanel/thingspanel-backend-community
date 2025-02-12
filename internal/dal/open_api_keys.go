// internal/dal/open_api_keys.go
package dal

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"

	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"
)

// CreateOpenAPIKey 创建新的OpenAPI密钥
func CreateOpenAPIKey(key *model.OpenAPIKey) error {
	return query.OpenAPIKey.Create(key)
}

// GetOpenAPIKeyByID 根据ID获取OpenAPI密钥信息
func GetOpenAPIKeyByID(id string) (*model.OpenAPIKey, error) {
	return query.OpenAPIKey.Where(query.OpenAPIKey.ID.Eq(id)).First()
}

// GetOpenAPIKeyByAppKey 根据AppKey获取OpenAPI密钥信息
func GetOpenAPIKeyByAppKey(appKey string) (*model.OpenAPIKey, error) {
	return query.OpenAPIKey.Where(query.OpenAPIKey.APIKey.Eq(appKey)).First()
}

// GetOpenAPIKeyListByPage 分页获取OpenAPI密钥列表
// @param listReq 查询参数
// @param tenantID 租户ID,用于权限过滤
// @return 总数,数据列表,错误信息
func GetOpenAPIKeyListByPage(listReq *model.OpenAPIKeyListReq, tenantID string) (int64, interface{}, error) {
	q := query.OpenAPIKey
	queryBuilder := q.WithContext(context.Background())

	// 添加租户过滤
	if tenantID != "" {
		queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))
	}

	// 添加查询条件
	if listReq.Status != nil {
		queryBuilder = queryBuilder.Where(q.Status.Eq(*listReq.Status))
	}

	// 获取总数
	count, err := queryBuilder.Count()
	if err != nil {
		return 0, nil, err
	}

	// 分页处理
	if listReq.Page != 0 && listReq.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(listReq.PageSize)
		queryBuilder = queryBuilder.Offset((listReq.Page - 1) * listReq.PageSize)
	}

	// 执行查询
	keys, err := queryBuilder.Order(q.CreatedAt.Desc()).Find()
	if err != nil {
		return 0, nil, err
	}

	return count, keys, nil
}

// UpdateOpenAPIKey 更新OpenAPI密钥信息
// @param id 密钥ID
// @param updates 需要更新的字段
func UpdateOpenAPIKey(id string, updates map[string]interface{}) error {
	q := query.OpenAPIKey
	updates["updated_at"] = time.Now()
	_, err := q.Where(q.ID.Eq(id)).Updates(updates)
	return err
}

// DeleteOpenAPIKey 删除OpenAPI密钥
// @param id 密钥ID
// @note 删除时会同时清理Redis缓存
func DeleteOpenAPIKey(id string) error {
	// 删除数据库记录
	_, err := query.OpenAPIKey.Where(query.OpenAPIKey.ID.Eq(id)).Delete()
	if err != nil {
		return err
	}

	// 清理缓存
	cacheKey := "openapi:key:" + id
	err = global.REDIS.Del(context.Background(), cacheKey).Err()
	if err != nil {
		logrus.Warnf("删除OpenAPI密钥缓存失败: %v", err)
	}

	return nil
}

// OpenAPIKeyQuery OpenAPI密钥查询结构体
type OpenAPIKeyQuery struct{}

// Count 获取OpenAPI密钥总数
func (OpenAPIKeyQuery) Count(ctx context.Context, option ...gen.Condition) (count int64, err error) {
	count, err = query.OpenAPIKey.WithContext(ctx).Where(option...).Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

// Select 按条件查询OpenAPI密钥列表
func (OpenAPIKeyQuery) Select(ctx context.Context, option ...gen.Condition) (list []*model.OpenAPIKey, err error) {
	list, err = query.OpenAPIKey.WithContext(ctx).Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}
