package service

import (
	"os"
	"strings"
	"testing"

	model "project/internal/model"
	"project/mqtt"
	"project/pkg/global"
)

func TestOTAInformParamsUsesPackageMetadata(t *testing.T) {
	sign, method, module, path := "sha256-value", "SHA256", "MCU", "/api/v1/ota/download/files/upgradePackage/2026-08-31/firmware.bin"
	params := buildOTAInformParams(&model.OtaUpgradePackage{
		Version: "1.0.1", PackageURL: &path, Signature: &sign,
		SignatureType: &method, Module: &module,
	}, "https://yomitest.gwcz.online/tp", 1234)

	if params["url"] != "https://yomitest.gwcz.online/tp/api/v1/ota/download/files/upgradePackage/2026-08-31/firmware.bin" {
		t.Fatalf("url=%v", params["url"])
	}
	if params["size"] != "1234" || params["sign"] != sign || params["signMethod"] != method {
		t.Fatalf("params=%v", params)
	}
}

func TestOTAInformTopicsCoverOfficialTypoAndDocs(t *testing.T) {
	topics := mqtt.OTAInformTopics("testA001")
	if len(topics) != 2 {
		t.Fatalf("got %v", topics)
	}
	if topics[0] != "ota/devices/infrom/testA001" || topics[1] != "ota/devices/inform/testA001" {
		t.Fatalf("got %v", topics)
	}
}

func TestXiaoZhiHttpPackageDoesNotUseMqttInform(t *testing.T) {
	httpDelivery := `{"deliveryProtocol":"xiaozhi_http","chip":"ESP32S3"}`
	mqttDelivery := `{"deliveryProtocol":"ydp_mqtt"}`
	integrationDelivery := `{"deliveryProtocol":"ydp_mqtt","orchestrationRoute":"device_integration"}`
	if !usesDevicePullOTA(&model.OtaUpgradePackage{AdditionalInfo: &httpDelivery}) {
		t.Fatal("xiaozhi_http must wait for device HTTP check")
	}
	if usesDevicePullOTA(&model.OtaUpgradePackage{AdditionalInfo: &mqttDelivery}) {
		t.Fatal("ydp_mqtt must keep existing MQTT inform")
	}
	if !usesDevicePullOTA(&model.OtaUpgradePackage{AdditionalInfo: &integrationDelivery}) {
		t.Fatal("device_integration route must wait for device integration dispatch")
	}
}

func TestParseOfficialWrappedProgressPayload(t *testing.T) {
	p, err := parseOTAProgressPayload([]byte(`{"device_id":"testA001","values":{"step":"100","desc":"OTA升级完成","module":"MCU"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.DeviceKey != "testA001" || p.Step != 100 || p.Desc != "OTA升级完成" {
		t.Fatalf("got %+v", p)
	}
}

func TestParseGMQTTBase64WrappedProgressPayload(t *testing.T) {
	// GMQTT thingspanel 插件会把设备原文包成 device_id + values(base64)
	p, err := parseOTAProgressPayload([]byte(`{"device_id":"c6d967f6-093d-dd79-0f82-9ce753bd6d31","values":"eyJzdGVwIjoiMTAiLCJkZXNjIjoiT1RB5Y2H57qn5a6M5oiQMTAlIiwibW9kdWxlIjoiTUNVIn0="}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.DeviceKey != "c6d967f6-093d-dd79-0f82-9ce753bd6d31" || p.Step != 10 {
		t.Fatalf("got %+v", p)
	}
}

func TestParseDoubleWrappedProgressPayload(t *testing.T) {
	p, err := parseOTAProgressPayload([]byte(`{"device_id":"tp-1","values":{"device_id":"testA002","values":{"step":"100","desc":"升级完成","module":"MCU"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Step != 100 {
		t.Fatalf("got %+v", p)
	}
}

func TestParseMQTTXBareProgressPayload(t *testing.T) {
	p, err := parseOTAProgressPayload([]byte(`{"step":"100","desc":"OTA升级完成","module":"MCU"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Step != 100 {
		t.Fatalf("got %+v", p)
	}
}

func TestNextOTAStatusFromStep(t *testing.T) {
	st, ok := nextOTAStatusFromStep(100)
	if !ok || st != 4 {
		t.Fatalf("100 -> %d %v", st, ok)
	}
	st, ok = nextOTAStatusFromStep(50)
	if !ok || st != 3 {
		t.Fatalf("50 -> %d %v", st, ok)
	}
	st, ok = nextOTAStatusFromStep(-1)
	if !ok || st != 5 {
		t.Fatalf("-1 -> %d %v", st, ok)
	}
	if _, ok = nextOTAStatusFromStep(0); ok {
		t.Fatal("step 0 must be rejected")
	}
}

func TestOTAProgressLogKeepsEveryReport(t *testing.T) {
	first := buildOTAProgressLog("detail-1", "dev-1", otaProgress{Step: 10, Desc: "10%"}, 3, `{"step":"10"}`)
	second := buildOTAProgressLog("detail-1", "dev-1", otaProgress{Step: 100, Desc: "done"}, 4, `{"step":"100"}`)
	if first.TaskDetailID != second.TaskDetailID {
		t.Fatal("logs must belong to the same task detail")
	}
	if first.ID == second.ID {
		t.Fatal("each report must have its own log id")
	}
	if first.Step == nil || *first.Step != 10 || second.Step == nil || *second.Step != 100 {
		t.Fatalf("steps not preserved: %+v %+v", first, second)
	}
}

func TestProgressLooksAtPendingPushedAndUpgrading(t *testing.T) {
	got := otaInProgressStatuses()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("progress must match pending/pushed/upgrading, got %v", got)
	}
}

func TestOTAProgressLogUsesVersionedMigration(t *testing.T) {
	ddl, err := os.ReadFile("../../sql/24.sql")
	if err != nil {
		t.Fatal(err)
	}
	if global.VERSION_NUMBER != 24 || !strings.Contains(string(ddl), "ota_upgrade_progress_logs") || !strings.Contains(string(ddl), "COMMENT ON TABLE") {
		t.Fatalf("OTA progress schema must be released as migration 24, version=%d", global.VERSION_NUMBER)
	}
}
