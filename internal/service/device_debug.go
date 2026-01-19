package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	model "project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	global "project/pkg/global"
	utils "project/pkg/utils"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type DeviceDebug struct{}

const (
	devDebugCfgKeyPrefix  = "tp:devdebug:cfg:"
	devDebugLogsKeyPrefix = "tp:devdebug:logs:"

	defaultDebugDurationSeconds = int64(30 * 60)
	defaultDebugMaxItems        = 1000
	defaultDebugPayloadMaxBytes = 4096
	debugTTLExtendSeconds       = int64(10 * 60)
)

func devDebugCfgKey(deviceID string) string  { return devDebugCfgKeyPrefix + deviceID }
func devDebugLogsKey(deviceID string) string { return devDebugLogsKeyPrefix + deviceID }

func (s *DeviceDebug) assertDeviceTenant(ctx context.Context, deviceID string, claims *utils.UserClaims) error {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return errcode.NewWithMessage(errcode.CodeParamError, "device_id is required")
	}

	dev, err := query.Device.WithContext(ctx).Where(query.Device.ID.Eq(deviceID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.NewWithMessage(errcode.CodeNotFound, "device not found")
		}
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{"sql_error": err.Error()})
	}
	if claims != nil && dev.TenantID != claims.TenantID {
		return errcode.NewWithMessage(errcode.CodeNoPermission, "no permission")
	}
	return nil
}

type DeviceDebugStatus struct {
	Enabled          bool                    `json:"enabled"`
	ExpireAt         int64                   `json:"expire_at"`
	RemainingSeconds int64                   `json:"remaining_seconds"`
	Config           model.DeviceDebugConfig `json:"config"`
}

func (s *DeviceDebug) SetDeviceDebug(ctx context.Context, deviceID string, req *model.SetDeviceDebugReq, claims *utils.UserClaims) (DeviceDebugStatus, error) {
	var resp DeviceDebugStatus
	if global.REDIS == nil {
		return resp, errcode.NewWithMessage(errcode.CodeSystemError, "redis not initialized")
	}
	if err := s.assertDeviceTenant(ctx, deviceID, claims); err != nil {
		return resp, err
	}

	now := time.Now().Unix()

	if req != nil && req.Enabled != nil && !*req.Enabled {
		_ = global.REDIS.Del(ctx, devDebugCfgKey(deviceID)).Err()
		return s.GetDeviceDebugStatus(ctx, deviceID, claims)
	}

	expireAt := int64(0)
	switch {
	case req != nil && req.ExpireAt != nil && *req.ExpireAt > 0:
		expireAt = *req.ExpireAt
	case req != nil && req.Duration != nil:
		if *req.Duration <= 0 {
			_ = global.REDIS.Del(ctx, devDebugCfgKey(deviceID)).Err()
			return s.GetDeviceDebugStatus(ctx, deviceID, claims)
		}
		expireAt = now + *req.Duration
	default:
		expireAt = now + defaultDebugDurationSeconds
	}

	maxItems := defaultDebugMaxItems
	payloadMaxBytes := defaultDebugPayloadMaxBytes
	if req != nil && req.MaxItems != nil {
		maxItems = *req.MaxItems
	}
	if req != nil && req.PayloadMaxBytes != nil {
		payloadMaxBytes = *req.PayloadMaxBytes
	}

	ttlSeconds := (expireAt - now) + debugTTLExtendSeconds
	if ttlSeconds <= 0 {
		_ = global.REDIS.Del(ctx, devDebugCfgKey(deviceID)).Err()
		return s.GetDeviceDebugStatus(ctx, deviceID, claims)
	}

	cfg := model.DeviceDebugConfig{
		Enabled:         true,
		ExpireAt:        expireAt,
		MaxItems:        maxItems,
		PayloadMaxBytes: payloadMaxBytes,
	}
	raw, _ := json.Marshal(cfg)

	pipe := global.REDIS.Pipeline()
	pipe.Set(ctx, devDebugCfgKey(deviceID), raw, time.Duration(ttlSeconds)*time.Second)
	pipe.Expire(ctx, devDebugLogsKey(deviceID), time.Duration(ttlSeconds)*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return resp, errcode.WithData(errcode.CodeCacheError, map[string]interface{}{"cache_error": err.Error()})
	}

	return s.GetDeviceDebugStatus(ctx, deviceID, claims)
}

func (s *DeviceDebug) GetDeviceDebugStatus(ctx context.Context, deviceID string, claims *utils.UserClaims) (DeviceDebugStatus, error) {
	var resp DeviceDebugStatus
	if global.REDIS == nil {
		return resp, errcode.NewWithMessage(errcode.CodeSystemError, "redis not initialized")
	}
	if err := s.assertDeviceTenant(ctx, deviceID, claims); err != nil {
		return resp, err
	}

	now := time.Now().Unix()
	resp.Config = model.DeviceDebugConfig{
		Enabled:         false,
		ExpireAt:        0,
		MaxItems:        defaultDebugMaxItems,
		PayloadMaxBytes: defaultDebugPayloadMaxBytes,
	}

	val, err := global.REDIS.Get(ctx, devDebugCfgKey(deviceID)).Result()
	if err != nil {
		if err == redis.Nil {
			resp.Enabled = false
			resp.ExpireAt = 0
			resp.RemainingSeconds = 0
			return resp, nil
		}
		return resp, errcode.WithData(errcode.CodeCacheError, map[string]interface{}{"cache_error": err.Error()})
	}

	var cfg model.DeviceDebugConfig
	if uerr := json.Unmarshal([]byte(val), &cfg); uerr != nil {
		return resp, errcode.WithData(errcode.CodeCacheError, map[string]interface{}{"cache_error": uerr.Error()})
	}

	enabled := cfg.Enabled
	if cfg.ExpireAt > 0 && now > cfg.ExpireAt {
		enabled = false
	}
	remain := int64(0)
	if cfg.ExpireAt > now {
		remain = cfg.ExpireAt - now
	}

	resp.Config = cfg
	resp.Enabled = enabled
	resp.ExpireAt = cfg.ExpireAt
	resp.RemainingSeconds = remain
	return resp, nil
}

type DeviceDebugLogsResp struct {
	Total  int64                       `json:"total"`
	Offset int64                       `json:"offset"`
	Limit  int64                       `json:"limit"`
	List   []model.DeviceDebugLogEntry `json:"list"`
}

func (s *DeviceDebug) GetDeviceDebugLogs(ctx context.Context, deviceID string, req *model.GetDeviceDebugLogsReq, claims *utils.UserClaims) (DeviceDebugLogsResp, error) {
	var resp DeviceDebugLogsResp
	if global.REDIS == nil {
		return resp, errcode.NewWithMessage(errcode.CodeSystemError, "redis not initialized")
	}
	if err := s.assertDeviceTenant(ctx, deviceID, claims); err != nil {
		return resp, err
	}

	offset := int64(0)
	limit := int64(100)
	if req != nil {
		if req.Offset > 0 {
			offset = req.Offset
		}
		if req.Limit > 0 {
			limit = req.Limit
		}
	}

	key := devDebugLogsKey(deviceID)
	start := offset
	stop := offset + limit - 1

	pipe := global.REDIS.Pipeline()
	llen := pipe.LLen(ctx, key)
	lrange := pipe.LRange(ctx, key, start, stop)
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return resp, errcode.WithData(errcode.CodeCacheError, map[string]interface{}{"cache_error": err.Error()})
	}

	total, _ := llen.Result()
	rows, _ := lrange.Result()

	resp.Total = total
	resp.Offset = offset
	resp.Limit = limit
	resp.List = make([]model.DeviceDebugLogEntry, 0, len(rows))
	for _, raw := range rows {
		var item model.DeviceDebugLogEntry
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			resp.List = append(resp.List, model.DeviceDebugLogEntry{
				Event:  "error",
				Result: "error",
				Error:  "invalid log json",
				Extra: map[string]interface{}{
					"raw": raw,
				},
			})
			continue
		}
		resp.List = append(resp.List, item)
	}
	return resp, nil
}
