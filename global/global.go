package global

import (
	"github.com/casbin/casbin/v2"
	"gopkg.in/redis.v5"
	"gorm.io/gorm"
)

var VERSION = "0.0.2"
var VERSION_NUMBER = 2
var DB *gorm.DB
var REDIS *redis.Client
var CasbinEnforcer *casbin.Enforcer
var OtaAddress string
