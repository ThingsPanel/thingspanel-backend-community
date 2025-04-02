package automatecache

import "project/internal/model"

//单类设备
type MultipleDeviceCache struct{}

func NewMultipleDeviceCache() *MultipleDeviceCache {
	return &MultipleDeviceCache{}
}

func (*MultipleDeviceCache) GetAutomateCacheKeyPrefix() string {
	return "multiple"
}

func (*MultipleDeviceCache) GetDeviceTriggerConditionType() string {
	return model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE
}
