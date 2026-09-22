package global

import (
	"project/internal/middleware/response"

	"github.com/casbin/casbin/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	VERSION         = "0.0.20"
	VERSION_NUMBER  = 21
	SYSTEM_VERSION  = "dev" // 发布时通过 -ldflags -X 注入 Git Tag。
	DB              *gorm.DB
	REDIS           *redis.Client
	STATUS_REDIS    *redis.Client
	CasbinEnforcer  *casbin.Enforcer
	OtaAddress      string
	TPSSEManager    *SSEManager
	ResponseHandler *response.Handler
)

type EventData struct {
	Name    string
	Message string
}

// 事件通道
var EventChan chan EventData
