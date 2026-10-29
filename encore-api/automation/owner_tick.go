package automation

import (
	"encoding/json"
	"net/http"
)

// OwnerTick performs deployment-wide work; a tenant session is never sufficient.
//
//encore:api public raw method=POST path=/internal/automation/tick
func (s *Service) OwnerTick(w http.ResponseWriter, r *http.Request) {
	if !authorizeOwnerDeployment(r) {
		http.Error(w, "unauthorized automation worker", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("X-Automation-Tenant") != "" {
		http.Error(w, "scheduler requires deployment scope", http.StatusBadRequest)
		return
	}
	recovered, err := s.Recover(r.Context())
	if err != nil {
		http.Error(w, "automation recovery failed", http.StatusServiceUnavailable)
		return
	}
	tick, err := s.Tick(r.Context())
	if err != nil {
		http.Error(w, "automation tick failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Replayed int           `json:"replayed"`
		Tick     *TickResponse `json:"tick"`
	}{recovered.Replayed, tick})
}
