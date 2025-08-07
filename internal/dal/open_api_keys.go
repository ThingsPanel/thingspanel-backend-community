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
// param listReq 查询参数
// param tenantID 租户ID,用于权限过滤
// return 总数,数据列表,错误信息
func GetOpenAPIKeyListByPage(listReq *model.OpenAPIKeyListReq, tenantID string) (int64, interface{}, error) {
	q := query.OpenAPIKey
	u := query.User
	var keysList []model.OpenAPIKeyListRsp

	queryBuilder := q.WithContext(context.Background())

	// 添加租户过滤
	if tenantID != "" {
		queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))
	}

	// 添加查询条件
	if listReq.Status != nil {
		queryBuilder = queryBuilder.Where(q.Status.Eq(*listReq.Status))
	}

	// 左关联用户表
	queryBuilder = queryBuilder.LeftJoin(u, u.ID.EqCol(q.CreatedID))

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

	// 执行查询，选择所需字段
	err = queryBuilder.Select(
		q.ALL,
		u.ID.As("user_id"),
		u.Email.As("email"),
		u.Name.As("user_name"),
	).Order(q.CreatedAt.Desc()).Scan(&keysList)

	if err != nil {
		return 0, nil, err
	}

	return count, keysList, nil
}

// UpdateOpenAPIKey 更新OpenAPI密钥信息
// param id 密钥ID
// param updates 需要更新的字段
func UpdateOpenAPIKey(id string, updates map[string]interface{}) error {
	q := query.OpenAPIKey
	updates["updated_at"] = time.Now()
	_, err := q.Where(q.ID.Eq(id)).Updates(updates)
	return err
}

// DeleteOpenAPIKey 删除OpenAPI密钥
// param id 密钥ID
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

// 验证OpenAPI密钥是否有效并返回租户ID
// Redis缓存结构 key: "apikey:{api_key}" value: tenantID
// 有效期为1小时
func VerifyOpenAPIKey(ctx context.Context, appKey string) (string, string, error) {
	// 从Redis缓存中获取租户ID
	cacheKey := "apikey:" + appKey
	cacheKeyCreatedID := "apikey:createdid:" + appKey
	tenantID, err := global.REDIS.Get(ctx, cacheKey).Result()
	createdID, err1 := global.REDIS.Get(ctx, cacheKeyCreatedID).Result()
	if err != nil || err1 != nil {
		// 如果缓存中不存在，则从数据库中查询
		apiKey, err := query.OpenAPIKey.WithContext(ctx).Where(query.OpenAPIKey.APIKey.Eq(appKey), query.OpenAPIKey.Status.Eq(1)).First()
		if err != nil {
			return "", "", err
		}
		// 将查询结果存入Redis缓存，有效期为1小时
		tenantID = apiKey.TenantID
		createdID = *apiKey.CreatedID
		err = global.REDIS.Set(ctx, cacheKey, tenantID, time.Hour).Err()
		if err != nil {
			logrus.Warnf("设置OpenAPI密钥缓存失败: %v", err)
		}
		err = global.REDIS.Set(ctx, cacheKeyCreatedID, createdID, time.Hour).Err()
		if err != nil {
			logrus.Warnf("设置OpenAPI密钥创建者ID缓存失败: %v", err)
		}
	}
	return tenantID, createdID, nil
}
