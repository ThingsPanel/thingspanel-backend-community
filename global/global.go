package global

import (
	"github.com/casbin/casbin/v2"
	"gopkg.in/redis.v5"
	"gorm.io/gorm"
)

var VERSION = "0.0.4"
var VERSION_NUMBER = 4
var DB *gorm.DB
var REDIS *redis.Client
var CasbinEnforcer *casbin.Enforcer
var OtaAddress string
var TPSSEManager *SSEManager

type EventData struct {
	Name    string
	Message string
}

// 事件通道
var EventChan chan EventData
