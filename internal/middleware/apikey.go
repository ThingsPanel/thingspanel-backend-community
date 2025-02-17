package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
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
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (OpenAPIKey) TableName() string {
	return "open_api_keys"
}

// Redis缓存的APIKey信息
type APIKeyInfo struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Status   int    `json:"status"`
	Name     string `json:"name"`
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
	// 1. 从Redis缓存中获取
	info, err := v.getFromCache(apiKey)
	if err == nil {
		// 检查状态
		if info.Status != 1 {
			return nil, ErrAPIKeyDisabled
		}
		return info, nil
	}

	// 2. 缓存未命中,从数据库查询
	var key OpenAPIKey
	err = v.db.Where("api_key = ?", apiKey).First(&key).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, fmt.Errorf("query database error: %w", err)
	}

	// 3. 检查APIKey状态
	if key.Status != 1 {
		return nil, ErrAPIKeyDisabled
	}

	// 4. 构造缓存信息
	info = &APIKeyInfo{
		ID:       key.ID,
		TenantID: key.TenantID,
		Status:   key.Status,
		Name:     key.Name,
	}

	// 5. 更新缓存
	if err := v.setCache(apiKey, info); err != nil {
		// 缓存更新失败仅记录日志,不影响验证结果
		fmt.Printf("update cache error: %v\n", err)
	}

	return info, nil
}

// 从缓存获取APIKey信息
func (v *APIKeyValidator) getFromCache(apiKey string) (*APIKeyInfo, error) {
	key := fmt.Sprintf("apikey:%s", apiKey)
	data, err := v.redisClient.Get(v.ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var info APIKeyInfo
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// 设置APIKey缓存
func (v *APIKeyValidator) setCache(apiKey string, info *APIKeyInfo) error {
	key := fmt.Sprintf("apikey:%s", apiKey)
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}

	// 设置5分钟过期
	return v.redisClient.Set(v.ctx, key, data, 5*time.Minute).Err()
}

// 删除APIKey缓存
func (v *APIKeyValidator) DeleteCache(apiKey string) error {
	key := fmt.Sprintf("apikey:%s", apiKey)
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
