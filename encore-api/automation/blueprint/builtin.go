package blueprint

import (
	"strings"
	"time"

	"encore.app/automation/domain"
)

// The four built-in templates. This is deliberately a small, fixed set: each one
// is a recipe for a job customers already do by hand, not a marketplace.
//
// Three of them produce an alarm policy, because "持续高温 / 门没关 / 设备离线"
// are things a person must be told about and must be able to acknowledge and
// trace. The fourth produces an automation, because "有人之后延时关灯" is a
// sequence of device actions with no alarm lifecycle at all. Mixing the two would
// be the "把所有执行结果都伪装成告警" mistake the task book warns about.
//
// No template ships with a device id, a notification group or a broker address
// filled in. The defaults are thresholds and durations only.

func init() {
	register(highTemperatureV1())
	register(highTemperatureV2())
	register(doorLeftOpenV1())
	register(doorLeftOpenV2())
	register(deviceOfflineV1())
	register(motionLightOffV1())
}

// ---------------------------------------------------------------------------
// 持续高温与恢复
// ---------------------------------------------------------------------------

const keyHighTemperature = "sustained_high_temperature"

func highTemperatureParams(thresholdMax float64, defaultDuration string) []Param {
	return []Param{
		{Key: "device", Label: "设备", Type: ParamDevice, Required: true,
			Description: "监测哪台设备的温度"},
		{Key: "signal", Label: "上报方式", Type: ParamEnum, Required: true, Default: "telemetry",
			Options: []string{"telemetry", "attribute"},
			Description: "温度是作为遥测还是属性上报；选错会导致规则一直读不到数据"},
		{Key: "metric", Label: "温度标识符", Type: ParamText, Required: true, Default: "temperature",
			Capability:  &Capability{Kind: "telemetry"},
			Description: "设备上报温度用的字段名"},
		{Key: "threshold", Label: "高温阈值", Type: ParamNumber, Required: true, Default: "35",
			Unit: "°C", HasMin: true, Min: -50, HasMax: true, Max: thresholdMax,
			Description: "高于这个值开始计时"},
		{Key: "duration", Label: "持续多久算告警", Type: ParamDuration, Required: true, Default: defaultDuration,
			Description: "必须连续超过阈值这么久才告警，避免瞬时波动"},
		{Key: "recovery_threshold", Label: "恢复阈值", Type: ParamNumber, Default: "30",
			Unit: "°C", HasMin: true, Min: -50, HasMax: true, Max: thresholdMax,
			Description: "低于这个值算恢复；留空表示没有恢复条件，只能人工确认"},
		{Key: "recovery_duration", Label: "恢复持续多久", Type: ParamDuration, Default: "2m",
			Description: "低于恢复阈值并持续这么久才算真的恢复"},
		{Key: "notify_policy", Label: "通知组（可选）", Type: ParamPolicy,
			Description: "告警发生时通知谁；留空则只出现在告警列表里"},
	}
}

func highTemperatureV1() *Blueprint {
	return &Blueprint{
		Key: keyHighTemperature, Version: 1,
		Name:     "持续高温与恢复",
		Summary:  "温度连续高于阈值才告警，降到恢复阈值以下持续一段时间自动恢复。",
		Guidance: "选一台设备、填两个温度值即可。持续时长决定抗抖动能力：设得越短越灵敏，也越容易误报。",
		Params:   highTemperatureParams(200, "5m"),
		build:    buildHighTemperature,
	}
}

func highTemperatureV2() *Blueprint {
	return &Blueprint{
		Key: keyHighTemperature, Version: 2,
		Name:     "持续高温与恢复",
		Summary:  "温度连续高于阈值才告警，降到恢复阈值以下持续一段时间自动恢复。",
		Guidance: "选一台设备、填两个温度值即可。本版本收紧了阈值上限，并把默认持续时长缩短到 3 分钟。",
		Params:   highTemperatureParams(120, "3m"),
		build:    buildHighTemperature,
	}
}

func buildHighTemperature(v Values) (*domain.Definition, []Issue) {
	metric := v["metric"]
	signal := signalOf(v["signal"], domain.SignalTelemetry)
	wait := durOf(v["duration"])

	def := &domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{
			Groups: []domain.Group{{ID: "too-hot", Conditions: []domain.Condition{{
				Source: "self", Signal: signal, Key: metric,
				Operator: domain.OpGt, Value: v["threshold"], Unit: "°C",
			}}}},
			For: domain.Duration(wait),
		},
		Freshness: freshnessFor(wait),
		Targets:   domain.TargetSpec{DeviceIDs: []string{v["device"]}},
		Alarm: &domain.AlarmSpec{
			Key:            "high-temperature:" + v["device"] + ":" + sanitize(metric),
			Sources:        []domain.AlarmSource{domain.AlarmSourceDetector},
			Level:          "H",
			Dimension:      sanitize(metric),
			NotifyPolicyID: v["notify_policy"],
		},
	}
	// A recovery condition is optional on purpose. "The value went back to
	// normal" is only one way an alarm ends; the other is a person acknowledging
	// it, and inventing a recovery condition would silently clear alarms nobody
	// asked to clear.
	if rt := v["recovery_threshold"]; rt != "" {
		def.Recovery = &domain.Trigger{
			Groups: []domain.Group{{ID: "cooled-down", Conditions: []domain.Condition{{
				Source: "self", Signal: signal, Key: metric,
				Operator: domain.OpLt, Value: rt, Unit: "°C",
			}}}},
			For: domain.Duration(durOf(v["recovery_duration"])),
		}
	}
	return def, nil
}

// ---------------------------------------------------------------------------
// 门未关超时
// ---------------------------------------------------------------------------

const keyDoorLeftOpen = "door_left_open"

func doorLeftOpenParams(withGrace bool) []Param {
	params := []Param{
		{Key: "device", Label: "设备", Type: ParamDevice, Required: true,
			Description: "门磁所在设备"},
		{Key: "signal", Label: "上报方式", Type: ParamEnum, Required: true, Default: "telemetry",
			Options: []string{"telemetry", "attribute"},
			Description: "门磁状态是作为遥测还是属性上报"},
		{Key: "door_metric", Label: "门磁标识符", Type: ParamText, Required: true, Default: "door",
			Capability:  &Capability{Kind: "telemetry"},
			Description: "设备上报门磁状态用的字段名"},
		{Key: "open_value", Label: "开门时的值", Type: ParamText, Required: true, Default: "1",
			Description: "设备上报什么值代表门是开的（常见为 1、true、open）"},
		{Key: "duration", Label: "开门多久算异常", Type: ParamDuration, Required: true, Default: "3m",
			Description: "超过这个时长仍未关闭才告警"},
		{Key: "notify_policy", Label: "通知组（可选）", Type: ParamPolicy,
			Description: "告警发生时通知谁；留空则只出现在告警列表里"},
	}
	if withGrace {
		params = append(params, Param{
			Key: "recovery_grace", Label: "恢复确认时长", Type: ParamDuration,
			Description: "门关上后要稳定这么久才自动恢复；留空表示立即恢复。门被风吹动一下不应该清掉一条真实的告警",
		})
	}
	return params
}

func doorLeftOpenV1() *Blueprint {
	return &Blueprint{
		Key: keyDoorLeftOpen, Version: 1,
		Name:     "门未关超时",
		Summary:  "门打开超过设定时长仍未关闭时告警，门关上即恢复。",
		Guidance: "选门磁设备、填开门时的值和超时时长。注意「开门时的值」要与设备实际上报一致，填错会永远不告警。",
		Params:   doorLeftOpenParams(false),
		build:    buildDoorLeftOpen,
	}
}

func doorLeftOpenV2() *Blueprint {
	return &Blueprint{
		Key: keyDoorLeftOpen, Version: 2,
		Name:     "门未关超时",
		Summary:  "门打开超过设定时长仍未关闭时告警，门关上并稳定一段时间后恢复。",
		Guidance: "选门磁设备、填开门时的值和超时时长。本版本新增「恢复确认时长」，默认行为与上一版一致。",
		Params:   doorLeftOpenParams(true),
		build:    buildDoorLeftOpen,
	}
}

func buildDoorLeftOpen(v Values) (*domain.Definition, []Issue) {
	metric := v["door_metric"]
	signal := signalOf(v["signal"], domain.SignalTelemetry)
	open := v["open_value"]
	wait := durOf(v["duration"])

	def := &domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{
			Groups: []domain.Group{{ID: "left-open", Conditions: []domain.Condition{{
				Source: "self", Signal: signal, Key: metric,
				Operator: domain.OpEq, Value: open,
			}}}},
			For: domain.Duration(wait),
		},
		Recovery: &domain.Trigger{
			Groups: []domain.Group{{ID: "closed", Conditions: []domain.Condition{{
				Source: "self", Signal: signal, Key: metric,
				Operator: domain.OpNeq, Value: open,
			}}}},
			For: domain.Duration(durOf(v["recovery_grace"])),
		},
		Freshness: freshnessFor(wait),
		Targets:   domain.TargetSpec{DeviceIDs: []string{v["device"]}},
		Alarm: &domain.AlarmSpec{
			Key:            "door-left-open:" + v["device"] + ":" + sanitize(metric),
			Sources:        []domain.AlarmSource{domain.AlarmSourceDetector},
			Level:          "M",
			Dimension:      sanitize(metric),
			NotifyPolicyID: v["notify_policy"],
		},
	}
	return def, nil
}

// ---------------------------------------------------------------------------
// 设备离线 / 无数据
// ---------------------------------------------------------------------------

const keyDeviceOffline = "device_offline"

func deviceOfflineV1() *Blueprint {
	return &Blueprint{
		Key: keyDeviceOffline, Version: 1,
		Name:     "设备离线 / 无数据",
		Summary:  "设备离线超过设定时长时告警，重新上线即恢复；同时把「一直没有数据」显式记下来。",
		Guidance: "选一台设备、填离线多久算异常。这个模板只关心在线状态，不关心设备上报的具体数值。",
		Params: []Param{
			{Key: "device", Label: "设备", Type: ParamDevice, Required: true,
				Description: "监测哪台设备的在线状态"},
			{Key: "offline_after", Label: "离线多久算异常", Type: ParamDuration, Required: true, Default: "10m",
				Description: "持续离线超过这个时长才告警，避免网络抖动误报"},
			{Key: "notify_policy", Label: "通知组（可选）", Type: ParamPolicy,
				Description: "告警发生时通知谁；留空则只出现在告警列表里"},
		},
		build: buildDeviceOffline,
	}
}

func buildDeviceOffline(v Values) (*domain.Definition, []Issue) {
	wait := durOf(v["offline_after"])
	// The online flag is a status signal with a fixed key: it is produced by the
	// ingress normaliser, not by the device's own naming, so it is not a
	// parameter. Letting a customer rename it would produce a rule that can never
	// match anything.
	def := &domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{
			Groups: []domain.Group{{ID: "offline", Conditions: []domain.Condition{{
				Source: "self", Signal: domain.SignalStatus, Key: "online",
				Operator: domain.OpEq, Value: "false",
			}}}},
			For: domain.Duration(wait),
		},
		Recovery: &domain.Trigger{
			Groups: []domain.Group{{ID: "online", Conditions: []domain.Condition{{
				Source: "self", Signal: domain.SignalStatus, Key: "online",
				Operator: domain.OpEq, Value: "true",
			}}}},
		},
		Freshness: freshnessFor(wait),
		Targets:   domain.TargetSpec{DeviceIDs: []string{v["device"]}},
		Alarm: &domain.AlarmSpec{
			Key:            "device-offline:" + v["device"],
			Sources:        []domain.AlarmSource{domain.AlarmSourceDetector},
			Level:          "M",
			Dimension:      "online",
			NotifyPolicyID: v["notify_policy"],
		},
	}
	// Silence is not the same fact as "reported offline". The no-data window is
	// declared so a device that simply stops talking produces an explicit
	// "no data" record instead of quietly looking healthy.
	def.Freshness.NoData = domain.Duration(wait)
	return def, nil
}

// ---------------------------------------------------------------------------
// 有人后延时关灯
// ---------------------------------------------------------------------------

const keyMotionLightOff = "motion_then_light_off"

func motionLightOffV1() *Blueprint {
	return &Blueprint{
		Key: keyMotionLightOff, Version: 1,
		Name:     "有人后延时关灯",
		Summary:  "检测到有人就把灯打开，保持一段时间后自动关闭；期间再次检测到有人会重新计时。",
		Guidance: "人感和灯需要在同一台设备上（例如带人感的多键开关）。这是动作流程，不产生告警；用「重新计时」模式，所以持续有人不会让灯中途熄灭。",
		Params: []Param{
			{Key: "device", Label: "设备", Type: ParamDevice, Required: true,
				Description: "同时提供人感和灯控的设备"},
			{Key: "motion_metric", Label: "人感标识符", Type: ParamText, Required: true, Default: "motion",
				Capability:  &Capability{Kind: "telemetry"},
				Description: "设备上报「有人」用的字段名"},
			{Key: "motion_value", Label: "有人的值", Type: ParamText, Required: true, Default: "1",
				Description: "设备上报什么值代表检测到有人"},
			{Key: "light_metric", Label: "灯控标识符", Type: ParamText, Required: true, Default: "power",
				Capability:  &Capability{Kind: "telemetry"},
				Description: "控制灯开关用的字段名"},
			{Key: "on_value", Label: "开灯的值", Type: ParamText, Required: true, Default: "1",
				Description: "写入什么值表示开灯"},
			{Key: "off_value", Label: "关灯的值", Type: ParamText, Required: true, Default: "0",
				Description: "写入什么值表示关灯"},
			{Key: "hold", Label: "保持时长", Type: ParamDuration, Required: true, Default: "5m",
				Description: "最后一次检测到有人之后，灯再亮这么久"},
		},
		build: buildMotionLightOff,
	}
}

// buildMotionLightOff produces an action flow, not an alarm.
//
// The run mode is "restart" on purpose. With the default "single" mode, a second
// person walking in while the countdown is running would be refused as a
// concurrent trigger, and the light would still go out at the original deadline
// while somebody is standing in the room. Restarting is what "延时关灯" means.
func buildMotionLightOff(v Values) (*domain.Definition, []Issue) {
	hold := durOf(v["hold"])
	return &domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeRestart,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{
			Groups: []domain.Group{{ID: "motion", Conditions: []domain.Condition{{
				Source: "self", Signal: domain.SignalTelemetry, Key: v["motion_metric"],
				Operator: domain.OpEq, Value: v["motion_value"],
			}}}},
		},
		Freshness: freshnessFor(hold),
		Targets:   domain.TargetSpec{DeviceIDs: []string{v["device"]}},
		Steps: []domain.Step{
			{Kind: domain.StepAction, DeviceRef: "target", Action: "telemetry",
				Identifier: v["light_metric"], Value: v["on_value"]},
			{Kind: domain.StepWait, Wait: domain.Duration(hold)},
			{Kind: domain.StepAction, DeviceRef: "target", Action: "telemetry",
				Identifier: v["light_metric"], Value: v["off_value"]},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func durOf(v string) time.Duration {
	if strings.TrimSpace(v) == "" {
		return 0
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	return d
}

func signalOf(v string, fallback domain.SignalKind) domain.SignalKind {
	switch domain.SignalKind(v) {
	case domain.SignalTelemetry, domain.SignalAttribute, domain.SignalEvent,
		domain.SignalStatus, domain.SignalAlarm:
		return domain.SignalKind(v)
	}
	return fallback
}

// freshnessFor ties the trust window to the declared wait.
//
// This is the A08 lesson applied to templates: a window shorter than the wait
// makes the wait impossible to complete, because the evidence goes stale before
// the condition has been true long enough. Ten minutes of slack on top of the
// wait covers a normal reporting interval without turning "yesterday's reading"
// into current evidence.
func freshnessFor(wait time.Duration) domain.Freshness {
	window := wait + 10*time.Minute
	if window < 5*time.Minute {
		window = 5 * time.Minute
	}
	if window > 24*time.Hour {
		window = 24 * time.Hour
	}
	return domain.Freshness{
		MaxAge: domain.Duration(window),
		MaxGap: domain.Duration(window),
	}
}

// sanitize keeps a customer-supplied identifier usable inside an alarm business
// key, which only accepts letters, digits, dot, underscore, colon and hyphen.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "default"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}
