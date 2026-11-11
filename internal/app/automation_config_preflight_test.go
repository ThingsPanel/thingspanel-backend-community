package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"project/internal/automationbridge"
)

func TestAutomationConfigPreflightSubprocess(t *testing.T) {
	if scenario := os.Getenv("TP_TEST_AUTOMATION_PREFLIGHT"); scenario != "" {
		t.Setenv("GOTP_AUTOMATION_MODE", "native")
		t.Setenv("GOTP_AUTOMATION_BASE_URL", "")
		t.Setenv("GOTP_AUTOMATION_TOKEN", "")
		t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "")
		if scenario == "valid" {
			t.Setenv("GOTP_AUTOMATION_BASE_URL", "http://127.0.0.1:8000")
			t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("t", 32))
			t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment")
			if _, err := NewApplication(); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOTP_AUTOMATION_MODE", "invalid")
			c, err := automationbridge.ReadConfig()
			if err != nil || c.Mode != "native" {
				t.Fatalf("snapshot mode=%q err=%v", c.Mode, err)
			}
			return
		}
		called := false
		_, err := NewApplication(func(*Application) error { called = true; return nil })
		if err == nil || called {
			t.Fatalf("invalid config err=%v option called=%v", err, called)
		}
		for _, profile := range []string{"legacy", "ingress_v1"} {
			viper.Set("mqtt.input_profile", profile)
			if err := (&MQTTService{}).configureBrokerIngress(context.Background(), nil); err == nil {
				t.Fatalf("profile %s accepted invalid configuration", profile)
			}
		}
		return
	}
	for _, scenario := range []string{"invalid", "valid"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAutomationConfigPreflightSubprocess$")
		cmd.Env = append(os.Environ(), "TP_TEST_AUTOMATION_PREFLIGHT="+scenario)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("scenario %s: %v: %s", scenario, err, out)
		}
	}
}
