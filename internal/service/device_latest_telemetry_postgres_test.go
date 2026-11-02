package service

import (
	"errors"
	"github.com/spf13/viper"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/utils"
	"reflect"
	"testing"
	"time"
)

func TestTenantLatestTelemetryOwnerPostgres(t *testing.T) {
	db := setupSceneDeleteBarrierDB(t)
	useSceneDeleteBarrierDB(t, db)
	if err := db.AutoMigrate(&model.DeviceTemplate{}, &model.DeviceModelTelemetry{}); err != nil {
		t.Fatal(err)
	}
	previous := viper.Get("grpc.tptodb_type")
	viper.Set("grpc.tptodb_type", "NONE")
	t.Cleanup(func() { viper.Set("grpc.tptodb_type", previous) })
	name, label, unit, poison := "Owned device", "Temperature", "C", "FOREIGN SECRET"
	template, config := "latest-template", "latest-config"
	if err := db.Create(&model.DeviceTemplate{ID: template, TenantID: "tenant-a", Name: "owned"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.DeviceConfig{ID: config, TenantID: "tenant-a", DeviceType: "1", DeviceTemplateID: &template}).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range []model.DeviceModelTelemetry{{ID: "z-owned-model", TenantID: "tenant-a", DeviceTemplateID: template, DataIdentifier: "number", DataName: &label, Unit: &unit}, {ID: "a-poison-model", TenantID: "tenant-b", DeviceTemplateID: template, DataIdentifier: "number", DataName: &poison, Unit: &poison}} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"latest-a", "latest-b", "latest-c", "latest-d"} {
		d := model.Device{ID: id, TenantID: "tenant-a", DeviceNumber: id, Voucher: id, Name: &name, IsOnline: 1, DeviceConfigID: &config, IsEnabled: "enabled", ActivateFlag: "active"}
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Device{ID: "foreign-device", TenantID: "tenant-b", DeviceNumber: "foreign-device", Voucher: "foreign", IsEnabled: "enabled", ActivateFlag: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	a, b := "tenant-a", "tenant-b"
	stamp := time.Now().UTC().Truncate(time.Second)
	zero := float64(0)
	falseValue := false
	empty := ""
	for _, row := range []model.TelemetryCurrentData{
		{DeviceID: "latest-a", TenantID: &a, Key: "number", T: stamp, NumberV: &zero},
		{DeviceID: "latest-a", TenantID: &a, Key: "boolean", T: stamp, BoolV: &falseValue},
		{DeviceID: "latest-a", TenantID: &a, Key: "string", T: stamp, StringV: &empty},
		{DeviceID: "latest-b", TenantID: &a, Key: "number", T: stamp, NumberV: &zero},
		{DeviceID: "latest-c", TenantID: &a, Key: "number", T: stamp, NumberV: &zero},
		{DeviceID: "latest-d", TenantID: &a, Key: "number", T: stamp, NumberV: &zero},
		{DeviceID: "latest-a", TenantID: &b, Key: "foreign-secret", T: stamp.Add(time.Hour), StringV: &poison},
		{DeviceID: "foreign-device", TenantID: &a, Key: "foreign-device-secret", T: stamp.Add(time.Hour), StringV: &poison},
		{DeviceID: "deleted-orphan", TenantID: &a, Key: "orphan-secret", T: stamp.Add(time.Hour), StringV: &poison},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	read := func() []map[string]interface{} {
		t.Helper()
		out, err := (&Device{}).GetTenantTelemetryData(a)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := read()
	ids := []string{}
	for _, d := range got {
		ids = append(ids, d["device_id"].(string))
	}
	if !reflect.DeepEqual(ids, []string{"latest-a", "latest-b", "latest-c"}) {
		t.Fatalf("unstable ranking or ownership leak: %v", ids)
	}
	items := got[0]["telemetry_data"].([]map[string]interface{})
	values := map[string]interface{}{}
	for _, item := range items {
		key := item["key"].(string)
		values[key] = item["value"]
		if key == "number" {
			l, ok := item["label"].(*string)
			if !ok || l == nil || *l != label {
				t.Fatalf("unscoped metadata: %+v", item)
			}
		}
	}
	if !reflect.DeepEqual(values, map[string]interface{}{"boolean": false, "number": float64(0), "string": ""}) {
		t.Fatalf("invalid zero/empty values or foreign contamination: %+v", values)
	}
	if err := (&Device{}).DeleteDevice("latest-a", &utils.UserClaims{ID: "owner", TenantID: a}); err != nil {
		t.Fatal(err)
	}
	got = read()
	ids = nil
	for _, d := range got {
		ids = append(ids, d["device_id"].(string))
	}
	if !reflect.DeepEqual(ids, []string{"latest-b", "latest-c", "latest-d"}) {
		t.Fatalf("deleted device remained visible: %v", ids)
	}
	if out, err := (&Device{}).GetTenantTelemetryData("no-data-tenant"); err != nil || out == nil || len(out) != 0 {
		t.Fatalf("empty result=%v err=%v", out, err)
	}
	if out, err := (&Device{}).GetTenantTelemetryData(""); out != nil || err == nil {
		t.Fatal("empty tenant was accepted")
	}
	for _, profile := range []string{"TSDB", "KINGBASE", "POLARDB", "unknown"} {
		viper.Set("grpc.tptodb_type", profile)
		out, err := (&Device{}).GetTenantTelemetryData(a)
		var code *errcode.Error
		if out != nil || !errors.As(err, &code) || code.Code != errcode.CodeServiceUnavailable {
			t.Fatalf("external/unknown profile accepted: %s %v", profile, err)
		}
	}
	viper.Set("grpc.tptodb_type", "NONE")
	if err := db.Migrator().DropTable(&model.TelemetryCurrentData{}); err != nil {
		t.Fatal(err)
	}
	out, err := (&Device{}).GetTenantTelemetryData(a)
	var code *errcode.Error
	if out != nil || !errors.As(err, &code) || code.Code != errcode.CodeDBError {
		t.Fatalf("SQL failure reported empty success: %v %v", out, err)
	}
}
