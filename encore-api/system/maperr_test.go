package system

import (
	"errors"
	"strings"
	"testing"

	"encore.dev/beta/errs"

	"encore.app/internal/systemagent"
)

func TestMapErrAgentCodeMessages(t *testing.T) {
	out := mapErr(&systemagent.Error{Code: "upgrade_helper_image_unavailable", Message: "raw"})
	var e *errs.Error
	if !errors.As(out, &e) || e.Code != errs.FailedPrecondition {
		t.Fatalf("unexpected error: %v", out)
	}
	if d, ok := e.Details.(errDetails); !ok || d.Code != "upgrade_helper_image_unavailable" {
		t.Fatalf("code detail lost: %#v", e.Details)
	}
	for _, want := range []string{"docker pull docker:27-cli", "UPGRADE-HELPER-IMAGE", "docker save", "nothing was changed", "未对站点做任何修改"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("message missing %q: %s", want, e.Message)
		}
	}
	if strings.Contains(e.Message, "raw") {
		t.Errorf("raw agent message leaked: %s", e.Message)
	}
	// Unknown codes keep the Agent's message.
	e2 := mapErr(&systemagent.Error{Code: "other", Message: "agent text"}).(*errs.Error)
	if e2.Message != "agent text" {
		t.Errorf("fallback changed: %s", e2.Message)
	}
}
