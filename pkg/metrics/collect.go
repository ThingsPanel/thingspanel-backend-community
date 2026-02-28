package metrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	const configDir = "configs"
	const idFile = ".instance_id"
	idPath := filepath.Join(configDir, idFile)

	// 1. 尝试从文件读取
	if data, err := os.ReadFile(idPath); err == nil {
		instanceID := strings.TrimSpace(string(data))
		if instanceID != "" {
			return instanceID
		}
	}

	// 2. 如果没有，生成一个
	instanceID := uuid.New()

	// 保存到文件
	_ = os.MkdirAll(configDir, 0755)
	if err := os.WriteFile(idPath, []byte(instanceID), 0644); err != nil {
		logrus.Errorf("Failed to save instance_id to file: %v", err)
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
