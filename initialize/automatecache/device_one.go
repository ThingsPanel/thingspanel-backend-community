package automatecache

import "project/internal/model"

//单一设备
type OneDeviceCache struct{}

func NewOneDeviceCache() *OneDeviceCache {
	return &OneDeviceCache{}
}

//获取单个设置一级缓存key
func (*OneDeviceCache) GetAutomateCacheKeyPrefix() string {
	return "one"
}

func (*OneDeviceCache) GetDeviceTriggerConditionType() string {
	return model.DEVICE_TRIGGER_CONDITION_TYPE_ONE
}
