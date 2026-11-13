package thingmodel

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
)

type ownerUplinkContractRequest struct {
	ModelID    string `json:"model_id"`
	SnapshotID string `json:"snapshot_id"`
	Revision   uint64 `json:"revision"`
}

// OwnerUplinkContract exposes immutable published semantics to the trusted
// Community data owner. It never resolves a mutable Community device binding,
// returns resources/drafts, or accepts a browser token. Consumers must fence
// their current tenant/device binding in the business write transaction.
//
//encore:api public raw method=POST path=/internal/thingmodel/uplink-contract
func (s *Service) OwnerUplinkContract(w http.ResponseWriter, r *http.Request) {
	handleOwnerUplinkContract(w, r, func(ctx context.Context, tenant, model string, revision uint64) (app.Publication, error) {
		p := core.Principal{SubjectID: "community-uplink-owner", TenantID: core.TenantID(canonicalIdentity("tenant", tenant)), Capabilities: []string{string(core.CapabilityRead)}}
		return application().Version(ctx, p, core.ModelID(model), revision)
	})
}

func handleOwnerUplinkContract(w http.ResponseWriter, r *http.Request, read ownerPublicationReader) {
	token := strings.TrimSpace(os.Getenv("TP_AUTOMATION_SERVICE_TOKEN"))
	deployment := strings.TrimSpace(os.Getenv("TP_SOURCE_DEPLOYMENT_ID"))
	if len(token) < 32 || deployment == "" || r.Header.Get("X-Automation-Deployment") != deployment || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
		http.Error(w, "invalid uplink owner", http.StatusUnauthorized)
		return
	}
	tenant := strings.TrimSpace(r.Header.Get("X-Automation-Tenant"))
	if tenant == "" || len(tenant) > 256 {
		http.Error(w, "invalid tenant context", http.StatusBadRequest)
		return
	}
	var input ownerUplinkContractRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || input.ModelID == "" || input.SnapshotID == "" || len(input.ModelID) > 128 || len(input.SnapshotID) > 128 || input.Revision == 0 || strings.TrimSpace(input.ModelID) != input.ModelID || strings.TrimSpace(input.SnapshotID) != input.SnapshotID {
		http.Error(w, "invalid publication pins", http.StatusBadRequest)
		return
	}
	publication, err := read(r.Context(), tenant, input.ModelID, input.Revision)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, app.ErrNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, "published uplink contract unavailable", status)
		return
	}
	snapshot := publication.Snapshot
	if string(snapshot.TenantID) != canonicalIdentity("tenant", tenant) || string(snapshot.ModelID) != input.ModelID || string(snapshot.ID) != input.SnapshotID || snapshot.Revision != input.Revision {
		http.Error(w, "publication pins mismatch", http.StatusConflict)
		return
	}
	// This is the existing frozen v2 transport representation, not a second model
	// schema. Keep its scalar/enum encoding identical to Version's published DTO.
	model, err := json.Marshal(draftFromCore(core.Model{Name: snapshot.Name, Description: snapshot.Description, Category: snapshot.Category, Fields: snapshot.Fields}))
	if err != nil || len(model) > 1<<20 {
		http.Error(w, "unsupported publication size", http.StatusServiceUnavailable)
		return
	}
	digest := sha256.Sum256(model)
	body, err := json.Marshal(struct {
		SchemaVersion  int             `json:"schema_version"`
		TenantID       string          `json:"tenant_id"`
		ModelID        string          `json:"model_id"`
		SnapshotID     string          `json:"snapshot_id"`
		Revision       uint64          `json:"revision"`
		ContractSHA256 string          `json:"contract_sha256"`
		Model          json.RawMessage `json:"model"`
	}{1, tenant, input.ModelID, input.SnapshotID, input.Revision, hex.EncodeToString(digest[:]), model})
	if err != nil || len(body) > 1<<20 {
		http.Error(w, "unsupported publication size", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // only the trusted in-process cache owns retention
	_, _ = w.Write(body)
}
