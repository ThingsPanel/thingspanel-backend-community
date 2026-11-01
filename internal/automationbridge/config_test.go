package automationbridge

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestAutomationModeConfig(t *testing.T) {
	for _, tc := range []struct {
		name, mode, url, token, dep, want string
		invalid                           bool
	}{
		{"default", "", "", "", "", "legacy", false}, {"legacy", "LeGaCy", "", "", "", "legacy", false},
		{"native", "native", "http://127.0.0.1:8000", strings.Repeat("x", 32), "dep", "native", false},
		{"missing url", "native", "", strings.Repeat("x", 32), "dep", "", true},
		{"bad url", "native", "http://user:secret@example.com", strings.Repeat("x", 32), "dep", "", true},
		{"short token", "native", "http://example.com", "secret", "dep", "", true},
		{"invalid header", "native", "http://example.com", strings.Repeat("x", 32) + "\nsecret", "dep", "", true},
		{"missing deployment", "native", "http://example.com", strings.Repeat("x", 32), "", "", true},
		{"unknown mode", "surprise", "", "", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOTP_AUTOMATION_MODE", tc.mode)
			t.Setenv("GOTP_AUTOMATION_BASE_URL", tc.url)
			t.Setenv("GOTP_AUTOMATION_TOKEN", tc.token)
			t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", tc.dep)
			c, err := parseConfig()
			if (err != nil) != tc.invalid || c.Mode != tc.want {
				t.Fatalf("mode=%q err=%v", c.Mode, err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret") || (len(tc.token) >= 32 && strings.Contains(err.Error(), tc.token))) {
				t.Fatal("configuration error exposed a secret")
			}
		})
	}
}

func TestFrozenServiceIdentitySubprocess(t *testing.T) {
	if os.Getenv("TP_TEST_AUTOMATION_FROZEN") == "1" {
		token := strings.Repeat("a", 32)
		t.Setenv("GOTP_AUTOMATION_MODE", "native")
		t.Setenv("GOTP_AUTOMATION_BASE_URL", "http://127.0.0.1:8000")
		t.Setenv("GOTP_AUTOMATION_TOKEN", token)
		t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "original")
		if err := Initialize(); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("b", 32))
		t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "changed")
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(DeploymentHeader, "original")
		r.Header.Set(TenantHeader, "tenant")
		if _, err := Authenticate(r); err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 32))
		r.Header.Set(DeploymentHeader, "changed")
		if _, err := Authenticate(r); err == nil {
			t.Fatal("unfrozen identity accepted")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFrozenServiceIdentitySubprocess$")
	cmd.Env = append(os.Environ(), "TP_TEST_AUTOMATION_FROZEN=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("subprocess: %v: %s", err, out)
	}
}

func TestDirectLegacyIdentityFreezesModeSubprocess(t *testing.T) {
	if os.Getenv("TP_TEST_LEGACY_IDENTITY_CHILD") == "1" {
		token := strings.Repeat("a", 32)
		t.Setenv("GOTP_AUTOMATION_MODE", "legacy")
		t.Setenv("GOTP_AUTOMATION_BASE_URL", "")
		t.Setenv("GOTP_AUTOMATION_TOKEN", token)
		t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "original")
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(DeploymentHeader, "original")
		r.Header.Set(TenantHeader, "tenant")
		if _, err := Authenticate(r); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GOTP_AUTOMATION_MODE", "native")
		t.Setenv("GOTP_AUTOMATION_BASE_URL", "http://localhost:8000")
		t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("b", 32))
		t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "changed")
		c, err := ReadConfig()
		if err != nil || c.Mode != "legacy" || NativeEnabled() || Deployment() != "original" {
			t.Fatalf("identity/mode drift mode=%s err=%v", c.Mode, err)
		}
		if _, err := Authenticate(r); err != nil {
			t.Fatal(err)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDirectLegacyIdentityFreezesModeSubprocess$")
	cmd.Env = append(os.Environ(), "TP_TEST_LEGACY_IDENTITY_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, out)
	}
}

func TestInvalidFirstReadCanBeCorrectedSubprocess(t *testing.T) {
	scenario := os.Getenv("TP_TEST_INVALID_CONFIG_CHILD")
	if scenario != "" {
		t.Setenv("GOTP_AUTOMATION_MODE", scenario)
		t.Setenv("GOTP_AUTOMATION_BASE_URL", "")
		t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("a", 32))
		t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "original")
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
		r.Header.Set(DeploymentHeader, "original")
		r.Header.Set(TenantHeader, "tenant")
		if NativeEnabled() || Deployment() != "" {
			t.Fatal("invalid mode exposed identity")
		}
		if _, err := Authenticate(r); err == nil {
			t.Fatal("invalid mode authenticated")
		}
		t.Setenv("GOTP_AUTOMATION_MODE", "native")
		t.Setenv("GOTP_AUTOMATION_BASE_URL", "http://127.0.0.1:8000")
		c, err := ReadConfig()
		if err != nil || c.Mode != "native" || !NativeEnabled() || Deployment() != "original" {
			t.Fatalf("corrected mode=%s err=%v", c.Mode, err)
		}
		if _, err := Authenticate(r); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, scenario := range []string{"native", "unknown"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestInvalidFirstReadCanBeCorrectedSubprocess$")
		cmd.Env = append(os.Environ(), "TP_TEST_INVALID_CONFIG_CHILD="+scenario)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", scenario, err, out)
		}
	}
}
