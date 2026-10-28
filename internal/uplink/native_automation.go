package uplink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"project/internal/model"
	"project/internal/service"

	"github.com/sirupsen/logrus"
)

// AutomationMode selects which rule engine evaluates an uplink.
//
// Exactly one engine runs, and that is the whole point of this file. Running
// both would evaluate one telemetry sample twice and dispatch the matching
// actions twice — the failure the rewrite exists to remove. Switching is
// therefore one value, not two switches that can drift apart.
type AutomationMode string

const (
	// AutomationModeLegacy keeps the in-process engine that ships with the
	// community edition. This is the default: an installation that says nothing
	// keeps behaving exactly as it does today.
	AutomationModeLegacy AutomationMode = "legacy"
	// AutomationModeNative forwards uplinks to the automation service and stops
	// calling the in-process engine.
	AutomationModeNative AutomationMode = "native"
)

// Environment variables. They are read directly rather than through the config
// file so an existing deployment can be switched without editing conf.yml.
const (
	envAutomationMode    = "GOTP_AUTOMATION_MODE"
	envAutomationBaseURL = "GOTP_AUTOMATION_BASE_URL"
	envAutomationToken   = "GOTP_AUTOMATION_TOKEN"
	envAutomationTimeout = "GOTP_AUTOMATION_TIMEOUT_MS"
	envAutomationQueue   = "GOTP_AUTOMATION_QUEUE"

	defaultAutomationTimeout = 5 * time.Second
	defaultAutomationQueue   = 1024
	nativeWorkers            = 4
)

// nativeUplink mirrors the automation service's UplinkRequest. Values and
// Params are JSON text because the service's API schema does not allow untyped
// maps on the wire.
type nativeUplink struct {
	Class      string `json:"class"`
	DeviceID   string `json:"device_id"`
	MessageID  string `json:"message_id,omitempty"`
	ObservedAt string `json:"observed_at,omitempty"`
	Values     string `json:"values"`
	Params     string `json:"params,omitempty"`
	Source     string `json:"source,omitempty"`
}

// nativeForwarder posts uplinks to the automation service.
//
// Posting happens on a bounded queue drained by a few workers. The uplink path
// carries every sample a device produces, so it must never block on another
// service: a slow or unreachable engine costs dropped automation events, not
// delayed telemetry. Dropping is counted and logged instead of being silent.
type nativeForwarder struct {
	baseURL string
	token   string
	client  *http.Client
	queue   chan nativeUplink
	dropped uint64
	logger  *logrus.Logger
}

var (
	forwarderOnce sync.Once
	forwarder     *nativeForwarder
)

// DispatchAutomation hands one uplink to the configured engine.
//
// Call sites used to call service.GroupApp.Execute directly. Routing them
// through here is what makes "which engine runs" a deployment decision instead
// of six independent code paths that could disagree.
func DispatchAutomation(device *model.Device, ext service.AutomateFromExt) error {
	if device == nil {
		return fmt.Errorf("automation dispatch: nil device")
	}
	f := sharedForwarder()
	if f == nil {
		return service.GroupApp.Execute(device, ext)
	}
	return f.enqueue(device, ext)
}

// CurrentAutomationMode reports the same process-frozen decision used by uplinks.
// In particular, a missing native URL is reported as legacy to every client.
func CurrentAutomationMode() AutomationMode {
	if sharedForwarder() != nil {
		return AutomationModeNative
	}
	return AutomationModeLegacy
}

// sharedForwarder builds the forwarder once per process. It returns nil when the
// installation has not switched, which is how DispatchAutomation knows to keep
// using the in-process engine.
func sharedForwarder() *nativeForwarder {
	forwarderOnce.Do(func() {
		mode := AutomationMode(strings.ToLower(strings.TrimSpace(os.Getenv(envAutomationMode))))
		if mode != AutomationModeNative {
			return
		}
		baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv(envAutomationBaseURL)), "/")
		if baseURL == "" {
			// Refusing to start forwarding is deliberate: a native mode with no
			// address would drop every uplink and look like "automation stopped
			// working". Falling back keeps the old engine alive and says why.
			logrus.WithField("env", envAutomationBaseURL).
				Error("自动化已配置为 native 但未给出服务地址，回退到内置引擎")
			return
		}
		timeout := defaultAutomationTimeout
		if raw := strings.TrimSpace(os.Getenv(envAutomationTimeout)); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
				timeout = time.Duration(ms) * time.Millisecond
			}
		}
		size := defaultAutomationQueue
		if raw := strings.TrimSpace(os.Getenv(envAutomationQueue)); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				size = n
			}
		}
		f := &nativeForwarder{
			baseURL: baseURL,
			token:   strings.TrimSpace(os.Getenv(envAutomationToken)),
			client:  &http.Client{Timeout: timeout},
			queue:   make(chan nativeUplink, size),
			logger:  logrus.StandardLogger(),
		}
		for i := 0; i < nativeWorkers; i++ {
			go f.run()
		}
		logrus.WithFields(logrus.Fields{
			"base_url": baseURL,
			"queue":    size,
			"workers":  nativeWorkers,
		}).Info("自动化已切换到原生引擎，内置引擎不再执行规则")
		forwarder = f
	})
	return forwarder
}

// DroppedAutomationUplinks reports how many uplinks were shed because the queue
// was full. Exposed for diagnostics and tests.
func DroppedAutomationUplinks() uint64 {
	f := sharedForwarder()
	if f == nil {
		return 0
	}
	return atomic.LoadUint64(&f.dropped)
}

func (f *nativeForwarder) enqueue(device *model.Device, ext service.AutomateFromExt) error {
	req, err := buildNativeUplink(device, ext, time.Now().UTC())
	if err != nil {
		return err
	}
	select {
	case f.queue <- req:
		return nil
	default:
		n := atomic.AddUint64(&f.dropped, 1)
		// Only the first drop of a burst is logged at error level; the rest are
		// counted. A full queue is one incident, not one incident per sample.
		if n == 1 || n%1000 == 0 {
			f.logger.WithFields(logrus.Fields{
				"device_id": device.ID,
				"dropped":   n,
			}).Error("自动化转发队列已满，本次上报未进入规则引擎")
		}
		return fmt.Errorf("automation dispatch queue is full")
	}
}

func (f *nativeForwarder) run() {
	for req := range f.queue {
		if err := f.post(req); err != nil {
			f.logger.WithError(err).WithFields(logrus.Fields{
				"device_id": req.DeviceID,
				"class":     req.Class,
			}).Warn("原生自动化转发失败")
		}
	}
}

func (f *nativeForwarder) post(req nativeUplink) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequest(http.MethodPost, f.baseURL+"/v1/ingress/uplink", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if f.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+f.token)
		httpReq.Header.Set("x-token", f.token)
	}
	resp, err := f.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("automation service answered %d", resp.StatusCode)
	}
	return nil
}

// buildNativeUplink translates the in-process engine's trigger payload into the
// automation service's uplink envelope.
//
// The values are already post-transform: the uplink pipeline applied unit
// conversion and the thing-model mapping before reaching here, so the service
// never re-interprets a raw payload.
func buildNativeUplink(device *model.Device, ext service.AutomateFromExt, observed time.Time) (nativeUplink, error) {
	req := nativeUplink{
		DeviceID:   device.ID,
		ObservedAt: observed.Format(time.RFC3339Nano),
		Source:     "community-uplink",
	}

	switch ext.TriggerParamType {
	case model.TRIGGER_PARAM_TYPE_TEL, model.TRIGGER_PARAM_TYPE_TELEMETRY:
		req.Class = "telemetry"
	case model.TRIGGER_PARAM_TYPE_ATTR, model.TRIGGER_PARAM_TYPE_ATTRIBUTES:
		req.Class = "attribute"
	case model.TRIGGER_PARAM_TYPE_EVT, model.TRIGGER_PARAM_TYPE_EVENT:
		req.Class = "event"
	case model.TRIGGER_PARAM_TYPE_STATUS:
		req.Class = "status"
	default:
		return req, fmt.Errorf("unsupported trigger param type %q", ext.TriggerParamType)
	}

	values, params, err := nativeValues(req.Class, ext)
	if err != nil {
		return req, err
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return req, err
	}
	req.Values = string(encoded)
	if len(params) > 0 {
		encodedParams, err := json.Marshal(params)
		if err != nil {
			return req, err
		}
		req.Params = string(encodedParams)
	}
	return req, nil
}

// nativeValues produces the value object each class expects.
//
// Telemetry and attributes are already key→value. Events carry their identifier
// in the value object and their parameters separately. Status is a single
// online/offline bit, which the old engine spelled "ON-LINE"/"OFF-LINE".
func nativeValues(class string, ext service.AutomateFromExt) (map[string]interface{}, map[string]interface{}, error) {
	switch class {
	case "telemetry", "attribute":
		if len(ext.TriggerValues) == 0 {
			return nil, nil, fmt.Errorf("no values to forward")
		}
		return ext.TriggerValues, nil, nil

	case "event":
		if len(ext.TriggerParam) == 0 {
			return nil, nil, fmt.Errorf("event uplink has no identifier")
		}
		method := ext.TriggerParam[0]
		params := map[string]interface{}{}
		if raw, ok := ext.TriggerValues[method]; ok {
			// The uplink pipeline stores event parameters as a JSON string.
			if s, isString := raw.(string); isString && strings.TrimSpace(s) != "" {
				if err := json.Unmarshal([]byte(s), &params); err != nil {
					return nil, nil, fmt.Errorf("event params are not a JSON object: %w", err)
				}
			} else if m, isMap := raw.(map[string]interface{}); isMap {
				params = m
			}
		}
		return map[string]interface{}{"identify": method}, params, nil

	case "status":
		online, ok := ext.TriggerValues["login"]
		if !ok {
			return nil, nil, fmt.Errorf("status uplink has no login value")
		}
		switch v := online.(type) {
		case string:
			upper := strings.ToUpper(strings.TrimSpace(v))
			return map[string]interface{}{"online": upper == "ON-LINE"}, nil, nil
		case bool:
			return map[string]interface{}{"online": v}, nil, nil
		}
		return nil, nil, fmt.Errorf("unrecognised status value %v", online)
	}
	return nil, nil, fmt.Errorf("unsupported class %q", class)
}
