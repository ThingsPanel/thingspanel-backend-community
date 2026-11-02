package service

import (
	"context"
	"encoding/json"
	"testing"

	"project/internal/model"
	"project/internal/repo"
)

// This regression uses the real Community grant table, PostgreSQL JSONB and
// the existing Redis/session/Casbin fixture. It deliberately keeps the v1
// number-lexeme digest: normalizing a stored digest would silently change the
// identity of an already-issued immutable execution plan.
func TestMarketInstallGrantExactPlanRoundTripPostgresRedis(t *testing.T) {
	db := marketInstallProviderFixture(t)
	ctx := context.Background()
	for _, number := range []string{"1e+20", "1e-7", "-0", "1.2300", "9007199254740993"} {
		t.Run(number, func(t *testing.T) {
			step := model.MarketInstallStep{ID: "native:create", Owner: "thingmodel", Action: "create", ResourceKey: "native", Payload: json.RawMessage(`{"model":{"name":"numeric plan","threshold":` + number + `},"resources":{}}`)}
			grant, request, _, _ := marketInstallGrantFixture(t, step)
			var stored repo.MarketInstallAuthorization
			if err := db.Where("id=?", grant.ID).Take(&stored).Error; err != nil {
				t.Fatal(err)
			}
			var legacyJSONB model.MarketInstallPlan
			if err := json.Unmarshal(stored.Plan, &legacyJSONB); err != nil {
				t.Fatal(err)
			}
			legacyDigest, err := marketInstallDigest(legacyJSONB)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("original plan digest=%s jsonb-only digest=%s original number=%s", grant.PlanDigest, legacyDigest, number)
			prepared, err := ReadMarketInstallPreparedPlan(ctx, model.MarketInstallPrepareRequest{ProtocolVersion: 1, MarketInstallIdentity: request.MarketInstallIdentity, Request: model.MarketInstallRequest{BundleKey: "bundle", Version: "1"}})
			if err != nil {
				t.Fatalf("newly issued original grant lost its v1 plan identity after PostgreSQL round trip: %v", err)
			}
			digest, err := marketInstallDigest(prepared)
			if err != nil || digest != grant.PlanDigest {
				t.Fatalf("prepared plan changed original digest: got=%s original=%s err=%v", digest, grant.PlanDigest, err)
			}
			if len(prepared.Steps) != 1 {
				t.Fatal("prepared plan changed its step count")
			}
			hash, err := marketInstallStepHash(prepared.Steps[0])
			if err != nil || hash != request.PayloadHash {
				t.Fatalf("prepared step changed original payload hash: got=%s original=%s err=%v", hash, request.PayloadHash, err)
			}
			if _, err := AuthorizeMarketInstallStep(ctx, request); err != nil {
				t.Fatalf("exact original step was refused after database round trip: %v", err)
			}
			if err := db.Where("id=?", grant.ID).Take(&stored).Error; err != nil || stored.OperationID != request.OperationID || stored.PlanDigest != grant.PlanDigest {
				t.Fatalf("authorization did not retain original operation and digest: %v", err)
			}
		})
	}
}
