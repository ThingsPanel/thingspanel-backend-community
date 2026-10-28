package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/brokersemantic"
	"project/internal/brokerstate"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

var (
	ErrBrokerSemanticUnauthorized = errors.New("broker semantic evidence unauthorized")
	ErrBrokerSemanticUnsupported  = errors.New("broker semantic evidence unsupported")
	ErrBrokerSemanticNotFound     = errors.New("broker semantic evidence not found")
	ErrBrokerSemanticUnavailable  = errors.New("broker semantic source unavailable")
)

type DeviceSemanticEvidence struct {
	Description brokersemantic.Description `json:"description"`
	State       brokerstate.Evidence       `json:"state"`
}

var semanticClientCache struct {
	sync.Mutex
	key    string
	client *brokersemantic.Client
}

func (*Device) BrokerSemanticEvidence(ctx context.Context, claims *utils.UserClaims, sessionToken, deviceID string) (DeviceSemanticEvidence, error) {
	if ctx == nil || claims == nil || strings.TrimSpace(deviceID) == "" {
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnauthorized
	}
	if strings.TrimSpace(viper.GetString("mqtt.input_profile")) != "ingress_v1" {
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnsupported
	}
	baseURL := strings.TrimSpace(viper.GetString("mqtt.semantic_base_url"))
	if baseURL == "" {
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := ValidateBrokerSubject(ctx, claims, sessionToken); err != nil {
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnauthorized
	}
	if global.DB == nil {
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnavailable
	}
	var device model.Device
	err := global.DB.WithContext(ctx).Where("id = ? AND tenant_id = ? AND is_enabled = ? AND activate_flag = ?", deviceID, claims.TenantID, "enabled", "active").Take(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DeviceSemanticEvidence{}, ErrBrokerSemanticNotFound
		}
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnavailable
	}
	client, err := configuredSemanticClient()
	if err != nil {
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnsupported
	}
	semantic, err := client.Describe(ctx, claims.TenantID, claims.ID, sessionToken, deviceID)
	if err != nil {
		if errors.Is(err, brokersemantic.ErrNotFound) || errors.Is(err, brokersemantic.ErrSemanticUnavailable) {
			return DeviceSemanticEvidence{}, ErrBrokerSemanticUnsupported
		}
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnavailable
	}
	if semantic.Pins.TenantID != claims.TenantID || semantic.Pins.SubjectID != claims.ID || semantic.Pins.DeviceID != device.ID {
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnsupported
	}
	state, err := brokerstate.New(global.DB).InspectCurrent(ctx, claims.TenantID, deviceID)
	if err != nil {
		if errors.Is(err, brokerstate.ErrDeviceUnavailable) {
			return DeviceSemanticEvidence{}, ErrBrokerSemanticNotFound
		}
		return DeviceSemanticEvidence{}, ErrBrokerSemanticUnavailable
	}
	return DeviceSemanticEvidence{Description: semantic, State: state}, nil
}

// ValidateBrokerSubject revalidates the exact x-token session used by the
// authenticated route. It rejects API-key middleware identities and stale or
// revoked sessions before forwarding the delegated bearer to Encore.
func ValidateBrokerSubject(ctx context.Context, claims *utils.UserClaims, token string) error {
	if ctx == nil || claims == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(claims.ID) == "" || strings.TrimSpace(claims.TenantID) == "" || global.REDIS == nil || global.DB == nil {
		return ErrBrokerSemanticUnauthorized
	}
	authority := strings.ToUpper(strings.TrimSpace(claims.Authority))
	if authority != "TENANT_ADMIN" && authority != "TENANT_USER" && authority != "SYS_ADMIN" {
		return ErrBrokerSemanticUnauthorized
	}
	key := strings.TrimSpace(viper.GetString("jwt.key"))
	if key == "" {
		return ErrBrokerSemanticUnauthorized
	}
	parsed, err := utils.NewJWT([]byte(key)).ParseToken(token)
	if err != nil || parsed == nil || parsed.ID != claims.ID || strings.TrimSpace(parsed.TenantID) != strings.TrimSpace(claims.TenantID) || strings.ToUpper(strings.TrimSpace(parsed.Authority)) != authority {
		return ErrBrokerSemanticUnauthorized
	}
	active, err := global.REDIS.Get(ctx, token).Result()
	if err != nil || active != "1" {
		return ErrBrokerSemanticUnauthorized
	}
	var user model.User
	if err := global.DB.WithContext(ctx).Select("id", "tenant_id", "status", "authority").Where("id = ?", claims.ID).Take(&user).Error; err != nil {
		return ErrBrokerSemanticUnauthorized
	}
	if user.Status == nil || *user.Status != "N" || user.TenantID == nil || strings.TrimSpace(*user.TenantID) != strings.TrimSpace(claims.TenantID) || user.Authority == nil || strings.ToUpper(strings.TrimSpace(*user.Authority)) != authority {
		return ErrBrokerSemanticUnauthorized
	}
	return nil
}

// CloseBrokerSemanticIdleConnections lets application shutdown release the
// single cached configured Encore transport.
func CloseBrokerSemanticIdleConnections() {
	semanticClientCache.Lock()
	defer semanticClientCache.Unlock()
	if semanticClientCache.client != nil {
		semanticClientCache.client.CloseIdleConnections()
	}
}

func configuredSemanticClient() (*brokersemantic.Client, error) {
	baseURL := strings.TrimSpace(viper.GetString("mqtt.semantic_base_url"))
	development := viper.GetBool("mqtt.semantic_development_http")
	if baseURL == "" {
		return nil, brokersemantic.ErrInvalid
	}
	cacheKey := baseURL + "\x00" + strconv.FormatBool(development)
	semanticClientCache.Lock()
	defer semanticClientCache.Unlock()
	if semanticClientCache.client != nil && semanticClientCache.key == cacheKey {
		return semanticClientCache.client, nil
	}
	client, err := brokersemantic.NewClient(brokersemantic.Config{BaseURL: baseURL, Development: development})
	if err != nil {
		return nil, err
	}
	if semanticClientCache.client != nil {
		semanticClientCache.client.CloseIdleConnections()
	}
	semanticClientCache.key, semanticClientCache.client = cacheKey, client
	return client, nil
}
