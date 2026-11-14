package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func applyNativeRuntimeLifecycle(ctx context.Context, item runtimeOutboxItem, number string) error {
	endpoint := strings.TrimRight(os.Getenv("TP_COMMUNITY_BASE_URL"), "/")
	parsed, err := url.Parse(endpoint)
	token, deployment := os.Getenv("TP_AUTOMATION_SERVICE_TOKEN"), os.Getenv("TP_SOURCE_DEPLOYMENT_ID")
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(token) < 32 || deployment == "" {
		return fmt.Errorf("runtime owner identity unavailable")
	}
	payload, _ := json.Marshal(map[string]any{"eventId": item.EventID, "deviceId": item.DeviceID, "generation": item.Generation, "kind": item.Kind, "deviceNumber": number})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/internal/automation/runtime-devices/lifecycle", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Automation-Deployment", deployment)
	req.Header.Set("X-Automation-Tenant", item.TenantID)
	client := http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("runtime owner unavailable")
	}
	defer resp.Body.Close()
	var result struct {
		Code int `json:"code"`
		Data struct {
			DeviceID   string `json:"deviceId"`
			EventID    string `json:"eventId"`
			Generation int64  `json:"generation"`
		} `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result) != nil || result.Code != 200 || result.Data.DeviceID != item.DeviceID || result.Data.EventID != item.EventID || result.Data.Generation != item.Generation {
		return fmt.Errorf("runtime lifecycle not confirmed")
	}
	return nil
}
