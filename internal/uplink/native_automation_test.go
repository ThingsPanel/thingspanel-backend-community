package uplink

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"project/internal/model"
	"project/internal/service"
)

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
// /v1/ingress/uplink, and it needs the tenant token on both headers.
func TestTheForwarderPostsToTheIngressPath(t *testing.T) {
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
	if gotURI != "/v1/ingress/uplink" {
		t.Fatalf("应该发到 /v1/ingress/uplink，实际 %q", gotURI)
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
	t.Setenv(envAutomationMode, "native")
	t.Setenv(envAutomationBaseURL, "http://127.0.0.1:14000")
	resetForwarderForTest()

	f := sharedForwarder()
	if f == nil {
		t.Fatal("配置成 native 且给出地址时应该启用转发")
	}
	if f.baseURL != "http://127.0.0.1:14000" {
		t.Fatalf("地址应该来自配置，实际 %q", f.baseURL)
	}
}

// TestNativeWithoutAnAddressFallsBackInsteadOfDroppingEverything: a half
// configured switch must not silently stop all automation.
func TestNativeWithoutAnAddressFallsBackInsteadOfDroppingEverything(t *testing.T) {
	t.Setenv(envAutomationMode, "native")
	t.Setenv(envAutomationBaseURL, "")
	resetForwarderForTest()

	if f := sharedForwarder(); f != nil {
		t.Fatal("没有服务地址时不应该启用转发，否则每一条上报都会被丢掉")
	}
}

// TestALegacyInstallationKeepsTheBuiltInEngine: doing nothing must keep doing
// exactly what it does today.
func TestALegacyInstallationKeepsTheBuiltInEngine(t *testing.T) {
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
	for _, tc := range []struct {
		mode, url string
		want      AutomationMode
	}{
		{"", "", AutomationModeLegacy},
		{"native", "", AutomationModeLegacy},
		{"native", "http://127.0.0.1:14000", AutomationModeNative},
	} {
		t.Setenv(envAutomationMode, tc.mode)
		t.Setenv(envAutomationBaseURL, tc.url)
		resetForwarderForTest()
		if got := CurrentAutomationMode(); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
	resetForwarderForTest()
}
