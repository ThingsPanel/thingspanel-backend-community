package service

import (
	"encoding/json"
	"errors"
	"testing"

	"project/internal/model"
	"project/internal/repo"
)

func TestMarketInstallGrantPlanEncoding(t *testing.T) {
	plan := model.MarketInstallPlan{ArtifactDigest: "original-artifact", Steps: []model.MarketInstallStep{{ID: "original", Owner: "thingmodel", Action: "create", Payload: json.RawMessage(`{"n":1e+20}`)}}}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := marketInstallDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	exact, encoding := string(raw), repo.MarketInstallPlanEncoding
	base := repo.MarketInstallAuthorization{Plan: raw, PlanDigest: digest, PlanExact: &exact, PlanEncoding: &encoding}
	for _, name := range []string{"known_exact", "verified_legacy", "unknown_encoding", "missing_exact", "missing_encoding", "empty_exact", "invalid_json", "changed_hash", "normalized_legacy"} {
		t.Run(name, func(t *testing.T) {
			grant := base
			wantLegacy := false
			switch name {
			case "verified_legacy":
				grant.PlanExact, grant.PlanEncoding, wantLegacy = nil, nil, true
			case "unknown_encoding":
				unknown := "future-number-format"
				grant.PlanEncoding = &unknown
			case "missing_exact":
				grant.PlanExact = nil
			case "missing_encoding":
				grant.PlanEncoding = nil
			case "empty_exact":
				empty := ""
				grant.PlanExact = &empty
			case "invalid_json":
				invalid := "{"
				grant.PlanExact = &invalid
			case "changed_hash":
				grant.PlanDigest = "changed"
			case "normalized_legacy":
				grant.PlanExact, grant.PlanEncoding = nil, nil
				changed := plan
				changed.Steps = append([]model.MarketInstallStep(nil), plan.Steps...)
				changed.Steps[0].Payload = json.RawMessage(`{"n":100000000000000000000}`)
				grant.Plan, err = json.Marshal(changed)
				if err != nil {
					t.Fatal(err)
				}
			}
			got, encoded, legacy, err := decodeMarketInstallGrantPlan(grant)
			if name != "known_exact" && name != "verified_legacy" {
				if !errors.Is(err, repo.ErrMarketInstallPlanIntegrity) || len(got.Steps) != 0 || got.ArtifactDigest != "" || encoded != "" {
					t.Fatalf("unverifiable plan was accepted or returned: plan=%+v err=%v", got, err)
				}
				return
			}
			if err != nil || encoded != exact || legacy != wantLegacy {
				t.Fatalf("original plan representation changed: legacy=%v want=%v err=%v", legacy, wantLegacy, err)
			}
			actual, err := marketInstallDigest(got)
			if err != nil || actual != digest {
				t.Fatal("original digest changed", err)
			}
		})
	}
}
