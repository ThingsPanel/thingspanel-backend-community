package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/global"
)

// Uses the existing real PostgreSQL/Redis authorization fixture and the
// project's actual Casbin matcher. The downstream native owner is an HTTP
// probe; this is not a JWT, multi-instance policy propagation or receiver
// transaction-fencing test. "thingsvis-authorize" exercises the exact provider
// function called by the trusted ThingsVis /internal/market-install/authorize
// callback, not a ThingsVis process or dashboard-import acceptance test.
func TestMarketInstallGrantPolicyRevocationPostgresRedis(t *testing.T) {
	for _, change := range []string{"canonical_resource", "role_policy", "subject_role", "permission_owner_unavailable"} {
		t.Run(change, func(t *testing.T) {
			db := marketInstallProviderFixture(t)
			ctx := context.Background()
			var downstream atomic.Int64
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downstream.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/internal/market-install/native" {
					t.Error("unexpected native owner request")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"state":"not_observed"}`))
			}))
			defer owner.Close()
			t.Setenv("TP_MARKETINSTALL_NATIVE_BASE_URL", owner.URL)
			t.Setenv("TP_MARKETINSTALL_NATIVE_TOKEN", strings.Repeat("n", 40))

			type entry struct {
				name    string
				grant   *repo.MarketInstallAuthorization
				request model.MarketInstallStepRequest
				before  []byte
			}
			var entries []entry
			for _, name := range []string{"prepare", "query", "execute", "thingsvis-authorize"} {
				step := model.MarketInstallStep{ID: "native:create", Owner: "thingmodel", Action: "create", ResourceKey: "native", Payload: json.RawMessage(`{"model":{"name":"fixed"},"resources":{}}`)}
				if name == "thingsvis-authorize" {
					step = model.MarketInstallStep{ID: "dashboard:import", Owner: "thingsvis", Action: "import", ResourceKey: "dashboard", Payload: json.RawMessage(`{"dashboard":{"name":"fixed dashboard"}}`)}
				}
				grant, request, _, _ := marketInstallGrantFixture(t, step)
				// Establish the same original plan and permission before revoking
				// it. Preparation is read-only and leaves operation_id unbound.
				prepared, err := ReadMarketInstallPreparedPlan(ctx, model.MarketInstallPrepareRequest{ProtocolVersion: 1, MarketInstallIdentity: request.MarketInstallIdentity, Request: model.MarketInstallRequest{BundleKey: "bundle", Version: "1"}})
				if err != nil || len(prepared.Steps) != 1 || prepared.Steps[0].Owner != step.Owner {
					t.Fatalf("original grant was not usable before revocation: %v", err)
				}
				var stored repo.MarketInstallAuthorization
				if err := db.Where("id=?", grant.ID).Take(&stored).Error; err != nil {
					t.Fatal(err)
				}
				if stored.OperationID != "" {
					t.Fatal("read-only preparation bound an operation")
				}
				before, err := json.Marshal(stored)
				if err != nil {
					t.Fatal(err)
				}
				entries = append(entries, entry{name: name, grant: grant, request: request, before: before})
			}

			enforcer := global.CasbinEnforcer
			requireChanged := func(changed bool, err error) {
				t.Helper()
				if err != nil || !changed {
					t.Fatalf("fixture policy did not change: %v", err)
				}
			}
			switch change {
			case "canonical_resource":
				requireChanged(enforcer.RemoveNamedGroupingPolicy("g2", MarketInstallationReadResource, "a08-grant-fixture-function"))
			case "role_policy":
				requireChanged(enforcer.RemovePolicy("a08-grant-fixture-role", "a08-grant-fixture-function", "allow"))
			case "subject_role":
				for _, e := range entries {
					requireChanged(enforcer.RemoveNamedGroupingPolicy("g", e.request.SubjectID, "a08-grant-fixture-role"))
				}
			case "permission_owner_unavailable":
				global.CasbinEnforcer = nil
			}

			// This callback observes real GORM writes; it does not suppress them
			// or substitute an error that could fake an authorization denial.
			var mutations atomic.Int64
			observeWrite := func(*gorm.DB) { mutations.Add(1) }
			const callback = "a08:grant-current-permission-writes"
			for _, err := range []error{
				db.Callback().Create().Before("gorm:create").Register(callback, observeWrite),
				db.Callback().Update().Before("gorm:update").Register(callback, observeWrite),
				db.Callback().Delete().Before("gorm:delete").Register(callback, observeWrite),
			} {
				if err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				_ = db.Callback().Create().Remove(callback)
				_ = db.Callback().Update().Remove(callback)
				_ = db.Callback().Delete().Remove(callback)
			})
			for _, e := range entries {
				t.Run(e.name, func(t *testing.T) {
					var err error
					switch e.name {
					case "prepare":
						var plan model.MarketInstallPlan
						plan, err = ReadMarketInstallPreparedPlan(ctx, model.MarketInstallPrepareRequest{ProtocolVersion: 1, MarketInstallIdentity: e.request.MarketInstallIdentity, Request: model.MarketInstallRequest{BundleKey: "bundle", Version: "1"}})
						if len(plan.Steps) != 0 || plan.ArtifactDigest != "" {
							t.Error("revoked grant returned an executable plan")
						}
					case "thingsvis-authorize":
						var plan model.MarketInstallPlan
						plan, err = AuthorizeMarketInstallStep(ctx, e.request)
						if len(plan.Steps) != 0 || plan.ArtifactDigest != "" {
							t.Error("revoked ThingsVis authorization returned a plan")
						}
					default:
						var observation model.MarketInstallObservation
						observation, err = NewMarketInstallProvider().Step(ctx, e.request, e.name == "execute")
						if observation.State != "" || observation.Receipt != nil {
							t.Error("revoked provider entry returned a business observation")
						}
					}
					if change == "permission_owner_unavailable" {
						if err == nil || errors.Is(err, repo.ErrMarketInstallUnauthorized) {
							t.Fatalf("unavailable policy owner was confirmed or misclassified: %v", err)
						}
					} else if !errors.Is(err, repo.ErrMarketInstallUnauthorized) {
						t.Fatalf("issued grant retained revoked current permission: %v", err)
					}
					if mutations.Load() != 0 || downstream.Load() != 0 {
						t.Fatalf("denied grant reached side effects: SQL mutations=%d downstream=%d", mutations.Load(), downstream.Load())
					}
					var stored repo.MarketInstallAuthorization
					if err := db.Where("id=?", e.grant.ID).Take(&stored).Error; err != nil {
						t.Fatal(err)
					}
					after, err := json.Marshal(stored)
					if err != nil {
						t.Fatal(err)
					}
					if string(after) != string(e.before) {
						t.Fatal("permission denial renewed or modified the immutable grant")
					}
					if marker, err := global.REDIS.Get(ctx, e.grant.SessionHash).Result(); err != nil || marker != "1" {
						t.Fatal("permission denial changed the original session marker")
					}
				})
			}

			// Restoring this private fixture's policy permits a read of the same
			// original grant. No new authorization reference or lifetime is issued.
			switch change {
			case "canonical_resource":
				requireChanged(enforcer.AddNamedGroupingPolicy("g2", MarketInstallationReadResource, "a08-grant-fixture-function"))
			case "role_policy":
				requireChanged(enforcer.AddPolicy("a08-grant-fixture-role", "a08-grant-fixture-function", "allow"))
			case "subject_role":
				for _, e := range entries {
					requireChanged(enforcer.AddNamedGroupingPolicy("g", e.request.SubjectID, "a08-grant-fixture-role"))
				}
			case "permission_owner_unavailable":
				global.CasbinEnforcer = enforcer
			}
			for _, e := range entries {
				grant, _, err := readCurrentMarketInstallGrant(ctx, e.request.MarketInstallIdentity)
				if err != nil || grant == nil || grant.ID != e.grant.ID {
					t.Fatalf("original grant could not be read after policy restoration: %v", err)
				}
				after, err := json.Marshal(grant)
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != string(e.before) {
					t.Fatal("current permission check replaced the original grant")
				}
			}
			if mutations.Load() != 0 || downstream.Load() != 0 {
				t.Fatal("permission-only checks issued a mutation or owner call")
			}
		})
	}
}
