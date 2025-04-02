// initialize/croninit/device_stats.go
package croninit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"project/internal/dal"
	"project/internal/service"
	"project/pkg/global"

	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
)

// DeviceStatsData 设备统计数据结构
type DeviceStatsData struct {
	DeviceTotal int64     `json:"device_total"`
	DeviceOn    int64     `json:"device_on"`
	Timestamp   time.Time `json:"timestamp"`
}

const (
	// Redis键模式: device_stats:{tenant_id}:{date}
	deviceStatsKeyPattern = "device_stats:%s:%s"
	// 数据保留时间(48小时)
	dataRetentionPeriod = 48 * time.Hour
)

// InitDeviceStatsCron 初始化设备统计定时任务
func InitDeviceStatsCron(c *cron.Cron) {
	// 每小时整点执行
	c.AddFunc("0 0 * * * *", func() {
		collectDeviceStats()
	})
}

// collectDeviceStats 收集设备统计数据
func collectDeviceStats() {
	ctx := context.Background()
	logrus.Info("开始执行设备状态统计任务")

	// 获取所有租户ID列表
	userList, err := dal.UserVo{}.GetTenantAdminList()
	if err != nil {
		logrus.Errorf("获取租户列表失败: %v", err)
		return
	}

	currentTime := time.Now()
	dateStr := currentTime.Format("2006-01-02")

	for _, user := range userList {
		// 获取该租户的设备统计数据
		deviceStats, err := service.GroupApp.Board.GetDeviceByTenantID(ctx, *user.TenantID)
		if err != nil {
			logrus.Errorf("获取租户 %s 的设备统计数据失败: %v", *user.TenantID, err)
			continue
		}

		// 构建统计数据
		statsData := DeviceStatsData{
			DeviceTotal: deviceStats.DeviceTotal,
			DeviceOn:    deviceStats.DeviceOn,
			Timestamp:   currentTime,
		}

		// 序列化数据
		statsJSON, err := json.Marshal(statsData)
		if err != nil {
			logrus.Errorf("序列化统计数据失败: %v", err)
			continue
		}

		// 构建Redis键
		key := fmt.Sprintf(deviceStatsKeyPattern, *user.TenantID, dateStr)

		// 将数据存储到Redis List中
		err = global.REDIS.RPush(ctx, key, string(statsJSON)).Err()
		if err != nil {
			logrus.Errorf("存储统计数据到Redis失败: %v", err)
			continue
		}

		// 设置过期时间
		err = global.REDIS.Expire(ctx, key, dataRetentionPeriod).Err()
		if err != nil {
			logrus.Errorf("设置Redis key过期时间失败: %v", err)
		}
	}

	logrus.Info("设备状态统计任务执行完成")
}
