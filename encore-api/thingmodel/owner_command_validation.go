package thingmodel

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
)

type ownerCommandValidationRequest struct {
	DeviceID        string          `json:"device_id"`
	ConfigID        string          `json:"config_id"`
	ModelID         string          `json:"model_id"`
	SnapshotID      string          `json:"snapshot_id"`
	Revision        int             `json:"revision"`
	BindingRevision int             `json:"binding_revision"`
	Identifier      string          `json:"identifier"`
	Params          json.RawMessage `json:"params"`
}

// OwnerValidateCommand is result-only. The authenticated Community owner reads
// and locks its tenant/device binding; this owner reads the exact tenant-scoped
// immutable publication. It never accepts model content or a browser identity.
//
//encore:api public raw method=POST path=/internal/thingmodel/validate-command
func (s *Service) OwnerValidateCommand(w http.ResponseWriter, r *http.Request) {
	handleOwnerCommandValidation(w, r, func(ctx context.Context, tenant, model string, revision uint64) (app.Publication, error) {
		p := core.Principal{SubjectID: "community-command-owner", TenantID: core.TenantID(canonicalIdentity("tenant", tenant)), Capabilities: []string{string(core.CapabilityRead)}}
		return application().Version(ctx, p, core.ModelID(model), revision)
	})
}

type ownerPublicationReader func(context.Context, string, string, uint64) (app.Publication, error)

func handleOwnerCommandValidation(w http.ResponseWriter, r *http.Request, read ownerPublicationReader) {
	token := strings.TrimSpace(os.Getenv("TP_AUTOMATION_SERVICE_TOKEN"))
	deployment := strings.TrimSpace(os.Getenv("TP_SOURCE_DEPLOYMENT_ID"))
	if len(token) < 32 || deployment == "" || r.Header.Get("X-Automation-Deployment") != deployment || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
		http.Error(w, "invalid command owner", 401)
		return
	}
	tenant := strings.TrimSpace(r.Header.Get("X-Automation-Tenant"))
	if tenant == "" || len(tenant) > 256 {
		http.Error(w, "invalid tenant context", 400)
		return
	}
	var req ownerCommandValidationRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 70<<10))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || req.DeviceID == "" || req.ConfigID == "" || req.ModelID == "" || req.SnapshotID == "" || req.Revision < 1 || req.BindingRevision < 1 || req.Identifier == "" {
		http.Error(w, "invalid command context", 400)
		return
	}
	publication, err := read(r.Context(), tenant, req.ModelID, uint64(req.Revision))
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, app.ErrNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, "published command unavailable", status)
		return
	}
	snapshot := publication.Snapshot
	if string(snapshot.TenantID) != canonicalIdentity("tenant", tenant) || string(snapshot.ModelID) != req.ModelID || string(snapshot.ID) != req.SnapshotID || snapshot.Revision != uint64(req.Revision) {
		http.Error(w, "published binding changed", 409)
		return
	}
	if core.ValidateCommandParameters(snapshot, req.Identifier, req.Params) != nil {
		http.Error(w, "invalid command parameters", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Valid           bool   `json:"valid"`
		DeviceID        string `json:"device_id"`
		SnapshotID      string `json:"snapshot_id"`
		BindingRevision int    `json:"binding_revision"`
	}{true, req.DeviceID, req.SnapshotID, req.BindingRevision})
}
