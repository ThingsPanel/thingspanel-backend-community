package automatecache

import "project/internal/model"

//单类设备
type MultipleDeviceCache struct{}

func NewMultipleDeviceCache() *MultipleDeviceCache {
	return &MultipleDeviceCache{}
}

func (c *MultipleDeviceCache) GetAutomateCacheKeyPrefix() string {
	return "multiple"
}

func (c *MultipleDeviceCache) GetDeviceTriggerConditionType() string {
	return model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE
}
