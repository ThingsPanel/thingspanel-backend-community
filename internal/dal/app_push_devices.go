package dal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/go-basic/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"project/pkg/global"
	"sort"
	"time"
)

type AppPushDevice struct {
	AuthVersion    int64     `json:"-"`
	ID             string    `json:"id"`
	TenantID       string    `json:"tenantId"`
	UserID         string    `json:"userId"`
	AppID          string    `json:"appId"`
	InstallationID string    `json:"-"`
	CID            string    `gorm:"column:cid" json:"cid"`
	Platform       string    `json:"-"`
	SessionHash    string    `json:"-"`
	ExpiresAt      time.Time `json:"-"`
	Active         bool      `json:"-"`
	UpdatedAt      time.Time `json:"-"`
}

func (AppPushDevice) TableName() string { return "app_push_devices" }
func AppPushSessionKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return "app-push-session:" + hex.EncodeToString(h[:])
}

func BindAppPushDevice(ctx context.Context, device AppPushDevice, token string) (AppPushDevice, error) {
	if global.DB == nil || global.REDIS == nil {
		return device, ErrNotificationMemberReadUnavailable
	}
	// A Lua script makes binding conditional on the still-live authenticated
	// session; a concurrent logout cannot resurrect its notification eligibility.
	device.SessionHash = AppPushSessionKey(token)
	ttl := time.Until(device.ExpiresAt).Milliseconds()
	if ttl <= 0 {
		return device, errors.New("session expired")
	}
	ok, err := global.REDIS.Eval(ctx, `if redis.call('GET',KEYS[1]) ~= '1' then return 0 end
 local ttl=redis.call('PTTL',KEYS[1]); local cap=tonumber(ARGV[1]); if ttl<0 or ttl>cap then ttl=cap end
 redis.call('SET',KEYS[2],'1','PX',ttl); return 1`, []string{token, device.SessionHash}, ttl).Int()
	if err != nil || ok != 1 {
		return device, errors.New("session unavailable")
	}
	err = global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locks := []string{device.AppID + ":cid:" + device.CID, device.AppID + ":installation:" + device.InstallationID}
		sort.Strings(locks)
		for _, key := range locks {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", key).Error; err != nil {
				return err
			}
		}
		var old AppPushDevice
		found := tx.Where("app_id = ? AND installation_id = ? AND active", device.AppID, device.InstallationID).Take(&old)
		if found.Error != nil && !errors.Is(found.Error, gorm.ErrRecordNotFound) {
			return found.Error
		}
		device.ID = uuid.New()
		if found.Error == nil && old.UserID == device.UserID && old.TenantID == device.TenantID && old.CID == device.CID && old.SessionHash == device.SessionHash {
			device.ID = old.ID
		}
		if err := tx.Model(&AppPushDevice{}).Where("app_id = ? AND (cid = ? OR installation_id = ?) AND active", device.AppID, device.CID, device.InstallationID).Updates(map[string]any{"active": false, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		device.Active = true
		device.UpdatedAt = time.Now()
		return tx.Save(&device).Error
	})
	return device, err
}

func UnbindAppPushDevice(ctx context.Context, user, cid, token string) error {
	if global.DB == nil {
		return ErrNotificationMemberReadUnavailable
	}
	return global.DB.WithContext(ctx).Model(&AppPushDevice{}).Where("user_id = ? AND cid = ? AND session_hash = ?", user, cid, AppPushSessionKey(token)).Updates(map[string]any{"active": false, "updated_at": time.Now()}).Error
}

// Both expansion and the final pre-send lookup check the live session and active
// tenant member. Never expose a stored CID after logout, account switch or expiry.
func ReadAppPushDevices(ctx context.Context, tenant, user, id string) ([]AppPushDevice, error) {
	if global.DB == nil || global.REDIS == nil {
		return nil, ErrNotificationMemberReadUnavailable
	}
	var rows []AppPushDevice
	q := global.DB.WithContext(ctx).Table("app_push_devices AS d").Select("d.*").Joins("JOIN users u ON u.id = d.user_id AND u.tenant_id = d.tenant_id AND u.status = 'N' AND u.auth_version = d.auth_version").Where("d.tenant_id = ? AND d.active AND d.expires_at > now()", tenant)
	if user != "" {
		q = q.Where("d.user_id = ?", user)
	}
	if id != "" {
		q = q.Where("d.id = ?", id)
	}
	if err := q.Order("d.id").Limit(501).Find(&rows).Error; err != nil {
		return nil, ErrNotificationMemberReadUnavailable
	}
	if len(rows) > 500 {
		return nil, ErrNotificationMemberReadUnavailable
	}
	result := make([]AppPushDevice, 0, len(rows))
	for _, row := range rows {
		value, err := global.REDIS.Get(ctx, row.SessionHash).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, ErrNotificationMemberReadUnavailable
		}
		if value == "1" {
			result = append(result, row)
		}
	}
	return result, nil
}
