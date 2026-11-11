package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"project/internal/brokercommands"
	"strings"
	"testing"
)

func TestPostgresNativeCommandRejectsSupersededAndForgedIdentity(t *testing.T) {
	if os.Getenv("TP_TEST_NATIVE_COMMAND_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestPostgresNativeCommandRejectsSupersededAndForgedIdentity$")
		cmd.Env = append(os.Environ(), "TP_TEST_NATIVE_COMMAND_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v: %s", err, out)
		}
		if strings.Contains(string(out), "--- SKIP:") {
			t.Skip("isolated PostgreSQL DSN unavailable")
		}
		return
	}
	db := automationPG(t)
	t.Setenv("GOTP_AUTOMATION_MODE", "native")
	t.Setenv("GOTP_AUTOMATION_BASE_URL", "http://127.0.0.1:8000")
	t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("t", 32))
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment-a")
	if err := db.Exec("CREATE TABLE automation_scope_generation(scope_key text PRIMARY KEY,generation bigint NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	scope := nativeAutomationScope("deployment-a", "tenant-a", "rule-a/device-a")
	if err := db.Exec("INSERT INTO automation_scope_generation VALUES(?,2)", scope).Error; err != nil {
		t.Fatal(err)
	}
	makeRow := func(generation int64) brokercommands.Row {
		accepted := NativeCommand{TenantID: "tenant-a", DeploymentID: "deployment-a", Kind: "telemetry", DeviceID: "device-a", Value: `{"switch":1}`, Key: "key-a", Scope: "rule-a/device-a", Generation: generation}
		canonical, _ := json.Marshal(accepted)
		payload, _ := json.Marshal(brokercommands.PreparedCommand{NativeAutomation: &brokercommands.NativeAutomationProof{DeploymentID: "deployment-a", Scope: scope, Generation: generation}})
		return brokercommands.Row{TenantID: "tenant-a", DeviceID: "device-a", CallerID: "native-automation:deployment-a", Intent: nativeAutomationPrefix + "telemetry", IdempotencyKey: "key-a", CanonicalRequest: canonical, Payload: payload}
	}
	if err := ValidateNativeAutomationCommand(context.Background(), db, makeRow(2)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNativeAutomationCommand(context.Background(), db, makeRow(1)); !errors.Is(err, ErrNativeSuperseded) || !errors.Is(err, brokercommands.ErrFenced) {
		t.Fatalf("old generation not fenced: %v", err)
	}
	for _, field := range []string{"tenant", "caller", "device", "idempotency"} {
		row := makeRow(2)
		switch field {
		case "tenant":
			row.TenantID = "tenant-b"
		case "caller":
			row.CallerID = "browser-user"
		case "device":
			row.DeviceID = "device-b"
		case "idempotency":
			row.IdempotencyKey = "key-b"
		}
		if err := ValidateNativeAutomationCommand(context.Background(), db, row); !errors.Is(err, brokercommands.ErrRejected) {
			t.Fatalf("forged %s identity accepted: %v", field, err)
		}
	}
	t.Setenv("GOTP_AUTOMATION_MODE", "legacy")
	if err := ValidateNativeAutomationCommand(context.Background(), db, makeRow(2)); err != nil {
		t.Fatalf("frozen native mode changed after environment mutation: %v", err)
	}
}

func TestLegacyModeFencesNativeCommand(t *testing.T) {
	if os.Getenv("TP_TEST_LEGACY_COMMAND_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLegacyModeFencesNativeCommand$")
		cmd.Env = append(os.Environ(), "TP_TEST_LEGACY_COMMAND_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v: %s", err, out)
		}
		return
	}
	t.Setenv("GOTP_AUTOMATION_MODE", "legacy")
	if err := ValidateNativeAutomationCommand(context.Background(), nil, brokercommands.Row{}); !errors.Is(err, brokercommands.ErrFenced) {
		t.Fatalf("legacy mode error: %v", err)
	}
}
