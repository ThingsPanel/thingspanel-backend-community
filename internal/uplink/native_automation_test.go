package uplink

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/model"
	"project/internal/service"
)

func isolateAutomationConfig(t *testing.T) bool {
	t.Helper()
	if os.Getenv("TP_TEST_UPLINK_CHILD") == t.Name() {
		return false
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), "TP_TEST_UPLINK_CHILD="+t.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, out)
	}
	return true
}

// deviceForAutomation is the minimum a device needs to carry for a dispatch.
func deviceForAutomation(id, tenant string) *model.Device {
	return &model.Device{ID: id, TenantID: tenant}
}

// TestTelemetryUplinkKeepsItsValues: the service evaluates post-transform
// values, so what leaves here must be the same numbers the in-process engine
// would have compared, not the raw payload.
func TestTelemetryUplinkKeepsItsValues(t *testing.T) {
	device := deviceForAutomation("dev-1", "t-1")
	req, err := buildNativeUplink(device, service.AutomateFromExt{
		TriggerParamType: model.TRIGGER_PARAM_TYPE_TEL,
		TriggerParam:     []string{"temperature"},
		TriggerValues:    map[string]interface{}{"temperature": 31.5, "switch": 1},
	}, time.Unix(1793100000, 0).UTC())
	if err != nil {
		t.Fatalf("遥测上报不应该被拒绝: %v", err)
	}
	if req.Class != "telemetry" {
		t.Fatalf("类型应该是 telemetry，实际 %q", req.Class)
	}
	var values map[string]interface{}
	if err := json.Unmarshal([]byte(req.Values), &values); err != nil {
		t.Fatalf("values 应该是合法 JSON: %v", err)
	}
	if values["temperature"] != 31.5 || values["switch"] != float64(1) {
		t.Fatalf("值应该原样带过去，实际 %v", values)
	}
}

// TestEventUplinkSeparatesIdentifierFromParams: an event's identity is its
// method, and its parameters are a separate object. Folding them together would
// make every event look like a different one.
func TestEventUplinkSeparatesIdentifierFromParams(t *testing.T) {
	device := deviceForAutomation("dev-1", "t-1")
	req, err := buildNativeUplink(device, service.AutomateFromExt{
		TriggerParamType: model.TRIGGER_PARAM_TYPE_EVT,
		TriggerParam:     []string{"overheat"},
		TriggerValues: map[string]interface{}{
			"overheat": `{"celsius":88,"zone":"b"}`,
		},
	}, time.Unix(1793100000, 0).UTC())
	if err != nil {
		t.Fatalf("事件上报不应该被拒绝: %v", err)
	}
	if req.Class != "event" {
		t.Fatalf("类型应该是 event，实际 %q", req.Class)
	}
	if req.Values != `{"identify":"overheat"}` {
		t.Fatalf("事件标识应该单独放在 identify，实际 %s", req.Values)
	}
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(req.Params), &params); err != nil {
		t.Fatalf("事件参数应该是合法 JSON: %v", err)
	}
	if params["celsius"] != float64(88) || params["zone"] != "b" {
		t.Fatalf("事件参数应该原样带过去，实际 %v", params)
	}
}

// TestStatusUplinkIsAnOnlineBit: the old engine spells status "ON-LINE" /
// "OFF-LINE"; the service wants a boolean. Both directions are checked because
// getting only the online case right would turn every offline into an online.
func TestStatusUplinkIsAnOnlineBit(t *testing.T) {
	cases := map[string]string{"ON-LINE": "true", "OFF-LINE": "false"}
	for login, want := range cases {
		req, err := buildNativeUplink(deviceForAutomation("dev-1", "t-1"), service.AutomateFromExt{
			TriggerParamType: model.TRIGGER_PARAM_TYPE_STATUS,
			TriggerValues:    map[string]interface{}{"login": login},
		}, time.Unix(1793100000, 0).UTC())
		if err != nil {
			t.Fatalf("%s 不应该被拒绝: %v", login, err)
		}
		if req.Class != "status" {
			t.Fatalf("类型应该是 status，实际 %q", req.Class)
		}
		if req.Values != `{"online":`+want+`}` {
			t.Fatalf("%s 应该翻译成 online=%s，实际 %s", login, want, req.Values)
		}
	}
}

// TestAnUnknownTriggerTypeIsRefused: forwarding something the service cannot
// interpret would create an event nobody evaluates while looking successful.
func TestAnUnknownTriggerTypeIsRefused(t *testing.T) {
	_, err := buildNativeUplink(deviceForAutomation("dev-1", "t-1"), service.AutomateFromExt{
		TriggerParamType: "SOMETHING-NEW",
		TriggerValues:    map[string]interface{}{"x": 1},
	}, time.Now())
	if err == nil {
		t.Fatal("未知触发类型应该被拒绝，而不是当成某一种上报发出去")
	}
}

// TestAnEmptyTelemetryUplinkIsRefused: a payload with no values would produce
// zero events and look like a rule that simply did not match.
func TestAnEmptyTelemetryUplinkIsRefused(t *testing.T) {
	_, err := buildNativeUplink(deviceForAutomation("dev-1", "t-1"), service.AutomateFromExt{
		TriggerParamType: model.TRIGGER_PARAM_TYPE_TEL,
		TriggerValues:    map[string]interface{}{},
	}, time.Now())
	if err == nil {
		t.Fatal("没有值的遥测上报应该被拒绝")
	}
}

// TestTheForwarderPostsToTheIngressPath: the service only accepts uplinks on
// the owner-only ingress, with independent service credentials.
func TestTheForwarderPostsToTheIngressPath(t *testing.T) {
	if isolateAutomationConfig(t) {
		return
	}
	t.Setenv(envAutomationMode, "legacy")
	var (
		mu     sync.Mutex
		gotURI string
		gotTok string
		got    nativeUplink
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotURI = r.URL.Path
		gotTok = r.Header.Get("Authorization")
		_ = json.Unmarshal(body, &got)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"class":"telemetry","accepted":1}`))
	}))
	defer server.Close()

	f := &nativeForwarder{
		baseURL: server.URL,
		token:   "tok-1",
		client:  &http.Client{Timeout: 2 * time.Second},
		queue:   make(chan nativeUplink, 4),
		logger:  nil,
	}
	err := f.post(nativeUplink{Class: "telemetry", DeviceID: "dev-1", Values: `{"x":1}`})
	if err != nil {
		t.Fatalf("转发不应该失败: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotURI != "/internal/automation/uplink" {
		t.Fatalf("应该发到服务上报入口，实际 %q", gotURI)
	}
	if gotTok != "Bearer tok-1" {
		t.Fatalf("应该带租户令牌，实际 %q", gotTok)
	}
	if got.DeviceID != "dev-1" || got.Values != `{"x":1}` {
		t.Fatalf("请求体应该原样到达，实际 %+v", got)
	}
}

// TestANativeInstallationStopsUsingTheBuiltInEngine is the safety property this
// whole file exists for: switching to the native engine must not leave the
// in-process engine running, because one sample would then dispatch twice.
func TestANativeInstallationStopsUsingTheBuiltInEngine(t *testing.T) {
	if isolateAutomationConfig(t) {
		return
	}
	t.Setenv(envAutomationMode, "native")
	t.Setenv(envAutomationBaseURL, "http://127.0.0.1:14000")
	t.Setenv(envAutomationToken, strings.Repeat("t", 32))
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment")
	resetForwarderForTest()

	f := sharedForwarder()
	if f == nil {
		t.Fatal("配置成 native 且给出地址时应该启用转发")
	}
	if f.baseURL != "http://127.0.0.1:14000" {
		t.Fatalf("地址应该来自配置，实际 %q", f.baseURL)
	}
}

func TestNativeWithoutAnAddressFailsClosed(t *testing.T) {
	if isolateAutomationConfig(t) {
		return
	}
	t.Setenv(envAutomationMode, "native")
	t.Setenv(envAutomationBaseURL, "")
	resetForwarderForTest()

	if f := sharedForwarder(); f != nil {
		t.Fatal("没有服务地址时不应该启用转发，否则每一条上报都会被丢掉")
	}
	if err := DispatchAutomation(deviceForAutomation("dev", "tenant"), service.AutomateFromExt{}); err == nil {
		t.Fatal("invalid native mode fell back to legacy")
	}
}

func TestInvalidModeDoesNotDispatchLegacy(t *testing.T) {
	if isolateAutomationConfig(t) {
		return
	}
	t.Setenv(envAutomationMode, "typo")
	resetForwarderForTest()
	if err := DispatchAutomation(deviceForAutomation("dev", "tenant"), service.AutomateFromExt{}); err == nil {
		t.Fatal("invalid mode dispatched")
	}
	if _, err := CurrentAutomationMode(); err == nil {
		t.Fatal("invalid mode reported as healthy")
	}
}

// TestALegacyInstallationKeepsTheBuiltInEngine: doing nothing must keep doing
// exactly what it does today.
func TestALegacyInstallationKeepsTheBuiltInEngine(t *testing.T) {
	if isolateAutomationConfig(t) {
		return
	}
	t.Setenv(envAutomationMode, "")
	resetForwarderForTest()

	if f := sharedForwarder(); f != nil {
		t.Fatal("没有配置切换时不应该启用转发")
	}
}

// resetForwarderForTest clears the process-wide forwarder so each test can pick
// its own configuration.
func resetForwarderForTest() {
	forwarderOnce = sync.Once{}
	forwarder = nil
}

func TestRuntimeModeMatchesUplinkDecision(t *testing.T) {
	if os.Getenv("TP_TEST_UPLINK_MODE_CASE") == "" {
		for _, name := range []string{"legacy", "invalid", "native"} {
			cmd := exec.Command(os.Args[0], "-test.run=^TestRuntimeModeMatchesUplinkDecision$")
			cmd.Env = append(os.Environ(), "TP_TEST_UPLINK_MODE_CASE="+name)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", name, err, out)
			}
		}
		return
	}
	for _, tc := range []struct {
		name      string
		mode, url string
		want      AutomationMode
		invalid   bool
	}{
		{"legacy", "", "", AutomationModeLegacy, false},
		{"invalid", "native", "", "", true},
		{"native", "native", "http://127.0.0.1:14000", AutomationModeNative, false},
	} {
		if tc.name != os.Getenv("TP_TEST_UPLINK_MODE_CASE") {
			continue
		}
		t.Setenv(envAutomationMode, tc.mode)
		t.Setenv(envAutomationBaseURL, tc.url)
		t.Setenv(envAutomationToken, strings.Repeat("t", 32))
		t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment")
		resetForwarderForTest()
		if got, err := CurrentAutomationMode(); got != tc.want || (err != nil) != tc.invalid {
			t.Fatalf("got %s error %v want %s", got, err, tc.want)
		}
	}
	resetForwarderForTest()
}

func TestDirectFirstConfigSnapshotAndRetry(t *testing.T) {
	scenario := os.Getenv("TP_TEST_UPLINK_FIRST_CASE")
	if scenario == "" {
		for _, name := range []string{"legacy", "native-invalid", "unknown"} {
			cmd := exec.Command(os.Args[0], "-test.run=^TestDirectFirstConfigSnapshotAndRetry$")
			cmd.Env = append(os.Environ(), "TP_TEST_UPLINK_FIRST_CASE="+name)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", name, err, out)
			}
		}
		return
	}
	t.Setenv(envAutomationBaseURL, "")
	t.Setenv(envAutomationToken, strings.Repeat("t", 32))
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment")
	if scenario == "legacy" {
		t.Setenv(envAutomationMode, "legacy")
		if mode, err := CurrentAutomationMode(); err != nil || mode != AutomationModeLegacy {
			t.Fatalf("mode %q err %v", mode, err)
		}
		if sharedForwarder() != nil {
			t.Fatal("legacy created forwarder")
		}
		t.Setenv(envAutomationMode, "native")
		t.Setenv(envAutomationBaseURL, "http://127.0.0.1:8000")
		if mode, err := CurrentAutomationMode(); err != nil || mode != AutomationModeLegacy || sharedForwarder() != nil {
			t.Fatalf("legacy snapshot changed: %q %v", mode, err)
		}
		return
	}
	if scenario == "unknown" {
		t.Setenv(envAutomationMode, "typo")
	} else {
		t.Setenv(envAutomationMode, "native")
	}
	if sharedForwarder() != nil {
		t.Fatal("invalid config created forwarder")
	}
	if err := DispatchAutomation(deviceForAutomation("dev", "tenant"), service.AutomateFromExt{}); err == nil {
		t.Fatal("invalid config dispatched")
	}
	t.Setenv(envAutomationMode, "native")
	t.Setenv(envAutomationBaseURL, "http://127.0.0.1:8000")
	if mode, err := CurrentAutomationMode(); err != nil || mode != AutomationModeNative {
		t.Fatalf("retry mode %q err %v", mode, err)
	}
	if sharedForwarder() == nil {
		t.Fatal("invalid first attempt consumed forwarder once")
	}
}
