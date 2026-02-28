package metrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"project/pkg/global"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type InstanceInfo struct {
	InstanceID  string `json:"instance_id"`
	DeviceCount int64  `json:"device_count"`
	UserCount   int64  `json:"user_count"`
	Version     string `json:"version"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Timestamp   int64  `json:"timestamp"`
}

func NewInstance() *InstanceInfo {
	return &InstanceInfo{
		Version: global.VERSION,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

// GetPersistentInstanceID 获取或生成持久化的实例ID
func GetPersistentInstanceID() string {
	if global.DB == nil {
		return "unknown"
	}

	var instanceID string
	err := global.DB.Table("sys_config").Where("config_key = ?", "instance_id").Select("config_value").Scan(&instanceID).Error
	if err == nil && instanceID != "" {
		return instanceID
	}

	// 如果没有，生成一个
	instanceID = uuid.New()
	err = global.DB.Exec("INSERT INTO sys_config (config_key, config_value, remark) VALUES (?, ?, ?)", "instance_id", instanceID, "Telemetry Instance ID").Error
	if err != nil {
		logrus.Errorf("Failed to save instance_id to DB: %v", err)
	}
	return instanceID
}

// Instan 获取实例运行时信息
func (ins *InstanceInfo) Instan() {
	ins.Timestamp = time.Now().Unix()
	ins.InstanceID = GetPersistentInstanceID()
}

// SendToPostHog 发送数据到 PostHog
func (ins *InstanceInfo) SendToPostHog() {
	enabled := viper.GetBool("telemetry.enabled")
	if !enabled {
		return
	}

	apiKey := viper.GetString("telemetry.posthog_key")
	if apiKey == "" {
		logrus.Warn("PostHog API Key is not configured, skipping telemetry")
		return
	}

	host := viper.GetString("telemetry.posthog_host")
	if host == "" {
		host = "https://us.i.posthog.com"
	}

	// PostHog Capture API format
	payload := map[string]interface{}{
		"api_key": apiKey,
		"event":   "installation_heartbeat",
		"properties": map[string]interface{}{
			"distinct_id":  ins.InstanceID,
			"version":      ins.Version,
			"device_count": ins.DeviceCount,
			"user_count":   ins.UserCount,
			"os":           ins.OS,
			"arch":         ins.Arch,
			"timestamp":    ins.Timestamp,
			"$lib":         "thingspanel-go-client",
		},
	}

	jsonData, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/capture/", host)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logrus.Debugf("Failed to send telemetry to PostHog: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Debugf("PostHog returned non-OK status: %d", resp.StatusCode)
	}
}

// Deprecated: 使用 SendToPostHog 代替
func (ins *InstanceInfo) SendSignedRequest() {
	ins.SendToPostHog()
}
