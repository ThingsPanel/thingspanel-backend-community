package service

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestIngressV1RejectsUnsupportedLegacyDownlinksBeforeDatabaseAccess(t *testing.T) {
	previousProfile := viper.GetString("mqtt.input_profile")
	previousDB := global.DB
	viper.Set("mqtt.input_profile", "ingress_v1")
	global.DB = nil
	t.Cleanup(func() {
		viper.Set("mqtt.input_profile", previousProfile)
		global.DB = previousDB
	})

	cases := []struct {
		name string
		call func() error
	}{
		{"attribute-control", func() error { return (&AttributeData{}).AttributePutMessage(context.Background(), "user", nil, "1") }},
		{"attribute-query", func() error { return (&AttributeData{}).AttributeGetMessage(&utils.UserClaims{}, nil) }},
		{"telemetry-control", func() error { return (&TelemetryData{}).TelemetryPutMessage(context.Background(), "user", nil, "1") }},
		{"ota-create", func() error { return (&OTA{}).CreateOTAUpgradeTask(nil) }},
		{"ota-publish", func() error { return (&OTA{}).PushOTAUpgradePackage(nil) }},
		{"expected-create", func() error {
			_, err := (&ExpectedData{}).Create(context.Background(), nil, &utils.UserClaims{})
			return err
		}},
		{"expected-send", func() error { return (&ExpectedData{}).Send(context.Background(), "device") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("unsupported ingress_v1 downlink was accepted")
			}
			if global.DB != nil {
				t.Fatal("test expected nil database")
			}
		})
	}
}

func TestLegacyDownlinkCapabilityRemainsAvailable(t *testing.T) {
	previousProfile := viper.GetString("mqtt.input_profile")
	viper.Set("mqtt.input_profile", "legacy")
	t.Cleanup(func() { viper.Set("mqtt.input_profile", previousProfile) })
	if err := requireLegacyDownlink("test control"); err != nil {
		t.Fatalf("legacy profile unexpectedly rejected: %v", err)
	}
}
