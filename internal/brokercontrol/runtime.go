package brokercontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	bc "project/internal/brokercontract"
)

// RuntimeFences includes live changes and five minutes of terminal tombstones.
// Tombstones outlive the maximum 300s grant so completing a migration cannot
// briefly revive an old socket before its next authentication refresh.
func (s *MigrationStore) RuntimeFences(ctx context.Context, cell string) (bc.RuntimeSyncResponse, error) {
	var out bc.RuntimeSyncResponse
	if !s.valid(ctx) || !routeField(cell, 128) {
		return out, ErrMigrationInvalid
	}
	e := s.db.WithContext(ctx).Raw(`SELECT id AS change_id,tenant_id,device_id,source_epoch AS denied_epoch,(phase IN ('frozen','connecting')) AS needs_ack FROM broker_route_changes WHERE source_cell=? AND (phase IN ('frozen','connecting') OR activated_at>=now()-interval '5 minutes' OR (phase='superseded' AND prepared_at>=now()-interval '5 minutes')) ORDER BY created_at,id LIMIT ?`, cell, bc.MaxRuntimeFences+1).Scan(&out.Fences).Error
	if len(out.Fences) > bc.MaxRuntimeFences {
		out.Fences = nil
		out.Overflow = true
	}
	return out, e
}

// SyncBrokerRuntime accepts only installed fences reported by this real broker
// boot. Registering a replacement before ACK invalidates its predecessor's
// generation. It does not infer a legacy process fence or a consumer ACK.
func (s *MigrationStore) SyncBrokerRuntime(ctx context.Context, r bc.RuntimeSyncRequest) (bc.RuntimeSyncResponse, error) {
	var empty bc.RuntimeSyncResponse
	if !s.valid(ctx) || !routeField(r.BrokerCell, 128) || !routeField(r.BrokerInstance, 128) || !routeField(r.BootID, 128) || len(r.Applied) > bc.MaxRuntimeFences {
		return empty, ErrMigrationInvalid
	}
	for _, ack := range r.Applied {
		if !routeHash(ack.EvidenceSHA256) || !routeField(ack.TenantID, 36) || !routeField(ack.DeviceID, 36) || ack.DeniedEpoch == 0 {
			return empty, ErrMigrationInvalid
		}
	}
	if _, e := s.RegisterMember(ctx, r.BrokerCell, "broker", r.BrokerInstance, r.BootID); e != nil {
		return empty, e
	}
	out, e := s.RuntimeFences(ctx, r.BrokerCell)
	if e != nil || out.Overflow {
		return out, e
	}
	for _, ack := range r.Applied {
		found := false
		for _, f := range out.Fences {
			if f.NeedsAck && f.ChangeID == ack.ChangeID && f.TenantID == ack.TenantID && f.DeviceID == ack.DeviceID && f.DeniedEpoch == ack.DeniedEpoch {
				found = true
				break
			}
		}
		if !found {
			continue
		} // An already-terminal change needs no further ACK.
		if e := s.AcknowledgeFence(ctx, ack.TenantID, ack.DeviceID, ack.ChangeID, "broker", r.BrokerInstance, r.BootID, ack.EvidenceSHA256); e != nil {
			// A new boot has installed the fence but cannot inherit the frozen
			// roster's ACK. Keep it alive/fenced so an explicit Rebase can recover;
			// Prepare/Activate still reject the changed membership digest.
			if errors.Is(e, ErrBarrierIncomplete) || errors.Is(e, ErrBindingConflict) {
				continue
			}
			return empty, e
		}
	}
	return out, nil
}

// RuntimeHandler keeps the device authentication endpoint and adds a separately
// bounded broker-only control endpoint under the SAME cell+instance token.
func (o *Owner) RuntimeHandler(store *MigrationStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runtime/sync" {
			o.Handler().ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", 405)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		body, e := io.ReadAll(r.Body)
		if e != nil || bc.ValidateUniqueJSON(body) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		var request bc.RuntimeSyncRequest
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if e := dec.Decode(&request); e != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if !o.authorizedToken(bearerToken(r.Header.Get("Authorization")), request.BrokerCell, request.BrokerInstance) {
			http.Error(w, "unauthorized", 401)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		response, e := store.SyncBrokerRuntime(ctx, request)
		if e != nil {
			status := 503
			if errors.Is(e, ErrMigrationInvalid) {
				status = 400
			}
			http.Error(w, "runtime synchronization unavailable", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
}
