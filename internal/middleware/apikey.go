package middleware

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"project/internal/dal"
)

var (
	ErrInvalidAPIKey  = errors.New("invalid api key")
	ErrAPIKeyDisabled = errors.New("api key is disabled")
	ErrAPIKeyNotFound = errors.New("api key not found")
	ErrInternalServer = errors.New("internal server error")
)

// APIKey数据模型
type OpenAPIKey struct {
	ID        string    `gorm:"column:id;primary_key"`
	TenantID  string    `gorm:"column:tenant_id"`
	APIKey    string    `gorm:"column:api_key"`
	Status    int       `gorm:"column:status"`
	Name      string    `gorm:"column:name"`
	CreatedID string    `gorm:"column:created_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (OpenAPIKey) TableName() string {
	return "open_api_keys"
}

// Redis缓存的APIKey信息
type APIKeyInfo struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Status    int    `json:"status"`
	Name      string `json:"name"`
	CreatedID string `json:"created_id"`
}

// APIKey验证器
type APIKeyValidator struct {
	db          *gorm.DB
	redisClient *redis.Client
	ctx         context.Context
}

// 创建验证器实例
func NewAPIKeyValidator(db *gorm.DB, redisClient *redis.Client) *APIKeyValidator {
	return &APIKeyValidator{
		db:          db,
		redisClient: redisClient,
		ctx:         context.Background(),
	}
}

// 验证APIKey
func (v *APIKeyValidator) ValidateAPIKey(apiKey string) (*APIKeyInfo, error) {
	key, err := dal.LookupActiveOpenAPIKey(v.ctx, v.db, apiKey)
	if err != nil {
		if errors.Is(err, dal.ErrAPIKeyNotFound) {
			return nil, ErrAPIKeyNotFound
		}
		if errors.Is(err, dal.ErrAPIKeyDisabled) {
			return nil, ErrAPIKeyDisabled
		}
		if errors.Is(err, dal.ErrAPIKeyIdentity) {
			return nil, ErrInvalidAPIKey
		}
		return nil, ErrInternalServer
	}
	return &APIKeyInfo{
		ID:        key.ID,
		TenantID:  key.TenantID,
		Status:    int(*key.Status),
		Name:      key.Name,
		CreatedID: *key.CreatedID,
	}, nil
}

// 删除APIKey缓存
func (v *APIKeyValidator) DeleteCache(apiKey string) error {
	key := fmt.Sprintf("apikey:info:%s", apiKey)
	return v.redisClient.Del(v.ctx, key).Err()
}

// 中间件示例用法
func ValidateAPIKeyMiddleware(validator *APIKeyValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.JSON(401, gin.H{"code": 40001, "message": "Missing API Key"})
			c.Abort()
			return
		}

		info, err := validator.ValidateAPIKey(apiKey)
		if err != nil {
			switch err {
			case ErrAPIKeyNotFound:
				c.JSON(401, gin.H{"code": 40001, "message": "Invalid API Key"})
			case ErrAPIKeyDisabled:
				c.JSON(401, gin.H{"code": 40002, "message": "API Key Disabled"})
			default:
				c.JSON(500, gin.H{"code": 50001, "message": "Internal Server Error"})
			}
			c.Abort()
			return
		}

		// 将验证信息存入上下文
		c.Set("apikey_info", info)
		c.Next()
	}
}
