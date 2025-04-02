package subscribe

import (
	"context"
	"encoding/json"
	"fmt"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/global"
	"time"

	"github.com/sirupsen/logrus"
)

func HeartbeatDeal(device *model.Device) {
	// 获取设备配置信息
	if device.DeviceConfigID == nil {
		return
	}
	// 从数据库中获取设备配置信息
	deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
	if err != nil {
		return
	}

	// 检查是否设置了心跳或者超时时间
	// other_config:{"online_timeout":0,"heartbeat":30}
	if deviceConfig.OtherConfig == nil {
		return
	}

	type OtherConfig struct {
		OnlineTimeout int `json:"online_timeout"`
		Heartbeat     int `json:"heartbeat"`
	}

	var otherConfig OtherConfig
	err = json.Unmarshal([]byte(*deviceConfig.OtherConfig), &otherConfig)
	if err != nil {
		return
	}

	if otherConfig.Heartbeat > 0 {
		if device.IsOnline != int16(1) {
			// 设备上线
			DeviceOnline([]byte("1"), "devices/status/"+device.ID)
		}
		//设置超时key
		err := global.STATUS_REDIS.Set(context.Background(),
			fmt.Sprintf("device:%s:heartbeat", device.ID),
			1,
			time.Duration(otherConfig.Heartbeat)*time.Second,
		).Err()
		if err != nil {
			logrus.Error(err)
			return
		}
		// 心跳优先于超时
		return
	}

	if otherConfig.OnlineTimeout > 0 {
		if device.IsOnline != int16(1) {
			// 设备上线
			DeviceOnline([]byte("1"), "devices/status/"+device.ID)
		}
		// 设置超时key
		err := global.STATUS_REDIS.Set(context.Background(),
			fmt.Sprintf("device:%s:timeout", device.ID),
			1,
			time.Duration(otherConfig.OnlineTimeout)*time.Second,
		).Err()
		if err != nil {
			logrus.Error(err)
			return
		}
	}
}
