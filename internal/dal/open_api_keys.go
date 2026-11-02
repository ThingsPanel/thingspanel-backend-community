// internal/dal/open_api_keys.go
package dal

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"

	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"
)

// CreateOpenAPIKey 创建新的OpenAPI密钥
func CreateOpenAPIKey(key *model.OpenAPIKey) error {
	return query.OpenAPIKey.Session(&gorm.Session{Logger: logger.Discard}).Create(key)
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
	keysList := make([]model.OpenAPIKeyListRsp, 0)

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

var (
	ErrAPIKeyWriteForbidden   = errors.New("api key write forbidden")
	ErrAPIKeyWriteUnknown     = errors.New("api key write outcome unknown")
	ErrAPIKeyWriteNotExecuted = errors.New("api key change not executed")
)

// MutateOpenAPIKey locks and changes a key in the same authorized scope.
// A failed commit or unconfirmed rollback must be reconciled before retry.
func MutateOpenAPIKey(ctx context.Context, db *gorm.DB, id, tenant string, allTenants, remove bool, updates map[string]interface{}) error {
	if db == nil {
		return ErrAPIKeyStore
	}
	tx := db.Session(&gorm.Session{Logger: logger.Discard}).WithContext(ctx).Clauses(dbresolver.Write).Begin()
	if tx.Error != nil {
		if ctx.Err() != nil {
			return ErrAPIKeyWriteNotExecuted
		}
		return ErrAPIKeyStore
	}
	rollback := func(err error) error {
		if tx.Rollback().Error != nil {
			return ErrAPIKeyWriteUnknown
		}
		return err
	}
	rollbackBeforeWrite := func(err error) error {
		if tx.Rollback().Error != nil {
			logrus.Warn("API key transaction rollback or lock cleanup unconfirmed before write")
		}
		return err
	}
	scoped := func() *gorm.DB {
		q := tx.Model(&model.OpenAPIKey{}).Where("id = ?", id)
		if !allTenants {
			q = q.Where("tenant_id = ?", tenant)
		}
		return q
	}
	var key model.OpenAPIKey
	err := scoped().Select("id", "tenant_id", "status", "name").Clauses(clause.Locking{Strength: "UPDATE"}).Take(&key).Error
	if err != nil && ctx.Err() != nil {
		// No DML or commit has been attempted; database/sql may already have
		// rolled back this context-bound transaction when cancellation arrived.
		return rollbackBeforeWrite(ErrAPIKeyWriteNotExecuted)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return rollbackBeforeWrite(ErrAPIKeyWriteForbidden)
	}
	if err != nil {
		return rollbackBeforeWrite(ErrAPIKeyStore)
	}
	if !remove && len(updates) == 1 {
		status, isStatus := updates["status"].(int16)
		if isStatus && status == 0 && key.Status != nil && *key.Status == 0 {
			if tx.Commit().Error != nil {
				return ErrAPIKeyWriteUnknown
			}
			return nil
		}
	}
	var result *gorm.DB
	if remove {
		result = scoped().Delete(&model.OpenAPIKey{})
	} else {
		updates["updated_at"] = time.Now()
		result = scoped().Updates(updates)
	}
	if result.Error != nil {
		return rollback(ErrAPIKeyStore)
	}
	if result.RowsAffected != 1 {
		return rollback(ErrAPIKeyWriteForbidden)
	}
	if tx.Commit().Error != nil {
		return ErrAPIKeyWriteUnknown
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

var (
	ErrAPIKeyNotFound = errors.New("api key not found")
	ErrAPIKeyDisabled = errors.New("api key disabled")
	ErrAPIKeyIdentity = errors.New("api key identity unavailable")
	ErrAPIKeyStore    = errors.New("api key store unavailable")
)

// LookupActiveOpenAPIKey always reads the primary. Redis allow entries are not
// authoritative for revocation; raw database errors and SQL are not exposed.
func LookupActiveOpenAPIKey(ctx context.Context, db *gorm.DB, appKey string) (*model.OpenAPIKey, error) {
	if appKey == "" {
		return nil, ErrAPIKeyNotFound
	}
	if db == nil {
		return nil, ErrAPIKeyStore
	}
	var key model.OpenAPIKey
	err := db.Session(&gorm.Session{Logger: logger.Discard}).WithContext(ctx).Clauses(dbresolver.Write).Where("api_key = ?", appKey).First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, ErrAPIKeyStore
	}
	if key.Status == nil || *key.Status != 1 {
		return nil, ErrAPIKeyDisabled
	}
	if strings.TrimSpace(key.TenantID) == "" || key.CreatedID == nil || strings.TrimSpace(*key.CreatedID) == "" {
		return nil, ErrAPIKeyIdentity
	}
	return &key, nil
}

// VerifyOpenAPIKey preserves the HTTP claims contract while checking the DB.
func VerifyOpenAPIKey(ctx context.Context, appKey string) (string, string, error) {
	key, err := LookupActiveOpenAPIKey(ctx, global.DB, appKey)
	if err != nil {
		return "", "", err
	}
	return key.TenantID, *key.CreatedID, nil
}
