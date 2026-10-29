package automation

import (
	"crypto/subtle"
	"encoding/json"
	"encore.dev/beta/errs"
	"io"
	"net/http"
	"os"
	"strings"
)

// OwnerUplink receives only authenticated community-owner events. Tenant context
// is supplied by that owner's durable receipt, independently of browser sessions.
//
//encore:api public raw method=POST path=/internal/automation/uplink
func (s *Service) OwnerUplink(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authorizeOwnerUplink(r)
	if !ok {
		http.Error(w, "unauthorized automation owner", http.StatusUnauthorized)
		return
	}
	var req UplinkRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil {
		http.Error(w, "invalid uplink", http.StatusBadRequest)
		return
	}
	if req.DeviceID == "" || req.MessageID == "" {
		http.Error(w, "device and stable message ID required", http.StatusBadRequest)
		return
	}
	response, err := s.ingestUplink(r.Context(), tenant, &req)
	if err != nil {
		status := http.StatusInternalServerError
		if errs.Code(err) == errs.InvalidArgument {
			status = http.StatusBadRequest
		}
		http.Error(w, "automation ingress rejected", status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func authorizeOwnerUplink(r *http.Request) (string, bool) {
	if !authorizeOwnerDeployment(r) {
		return "", false
	}
	tenant := strings.TrimSpace(r.Header.Get("X-Automation-Tenant"))
	return tenant, tenant != "" && len(tenant) <= 256
}

func authorizeOwnerDeployment(r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("TP_AUTOMATION_SERVICE_TOKEN"))
	deployment := strings.TrimSpace(os.Getenv("TP_SOURCE_DEPLOYMENT_ID"))
	if len(expected) < 32 || deployment == "" || r.Header.Get("X-Automation-Deployment") != deployment {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+expected)) != 1 {
		return false
	}
	return true
}
