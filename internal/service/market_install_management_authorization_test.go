package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/global"
)

// Count the real GORM execution paths. Only the current users SELECT belongs
// to this permission decision; any grant/installation read or write fails.
func managementSQLProbe(t *testing.T, db *gorm.DB) (userQueries, unexpected *int) {
	t.Helper()
	userQueries, unexpected = new(int), new(int)
	block := func(tx *gorm.DB) {
		(*unexpected)++
		tx.AddError(errors.New("unexpected owner access before permission"))
	}
	if err := db.Callback().Query().Before("gorm:query").Register("a08:management-query", func(tx *gorm.DB) {
		if tx.Statement.Table != "users" {
			block(tx)
			return
		}
		(*userQueries)++
	}); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		db.Callback().Create().Before("*").Register("a08:management-create", block),
		db.Callback().Update().Before("*").Register("a08:management-update", block),
		db.Callback().Delete().Before("*").Register("a08:management-delete", block),
		db.Callback().Raw().Before("*").Register("a08:management-raw", block),
		db.Callback().Row().Before("*").Register("a08:management-row", block),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return userQueries, unexpected
}

func requireManagementDenied(t *testing.T, err error) {
	t.Helper()
	var classified *errcode.Error
	if !errors.As(err, &classified) || classified.Code != errcode.CodeNoPermission {
		t.Fatalf("expected canonical resource denial, got %v", err)
	}
}

func TestMarketInstallationManagementCommunityEntriesDenyBeforeOwnerAccess(t *testing.T) {
	for _, owner := range []string{"legacy", "encore"} {
		for _, entry := range []string{"install", "get-by-id", "get-by-key"} {
			if entry == "get-by-key" && owner == "legacy" {
				continue
			}
			t.Run(owner+"/"+entry, func(t *testing.T) {
				db, enforcer := lookupPermissionFixture(t)
				if _, err := enforcer.RemovePolicy("lookup-fixture-role", "lookup-fixture-function", "allow"); err != nil {
					t.Fatal(err)
				}
				userQueries, unexpected := managementSQLProbe(t, db)
				var downstream atomic.Int64
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					downstream.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
				}))
				defer endpoint.Close()
				t.Setenv("TP_MARKETINSTALL_OWNER", owner)
				t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", endpoint.URL)
				var err error
				switch entry {
				case "install":
					var result *model.InstallBundleResponse
					result, err = InstallMarketBundleWithSelectedOwner(context.Background(), &model.InstallBundleRequest{BundleKey: "bundle", Version: "1", IdempotencyKey: "original-key"}, lookupClaims(), "live-session")
					if result != nil {
						t.Fatal("denied install returned a result")
					}
				case "get-by-id":
					var result *model.InstallBundleResponse
					result, err = GetSelectedMarketInstallation(context.Background(), "original-operation", lookupClaims(), "live-session")
					if result != nil {
						t.Fatal("denied ID query returned a result")
					}
				case "get-by-key":
					var result *model.MarketInstallationLookup
					result, err = GetSelectedMarketInstallationByKey(context.Background(), "original-key", lookupClaims(), "live-session")
					if result != nil {
						t.Fatal("denied key query returned a result")
					}
				}
				requireManagementDenied(t, err)
				if *userQueries != 1 || *unexpected != 0 || downstream.Load() != 0 {
					t.Fatalf("permission denial accessed owner: user checks=%d unexpected SQL=%d downstream=%d", *userQueries, *unexpected, downstream.Load())
				}
			})
		}
	}
}

func TestMarketInstallationManagementGrantIssuanceRechecksRevocation(t *testing.T) {
	db, enforcer := lookupPermissionFixture(t)
	ctx := context.Background()
	claims := lookupClaims()
	claims.ExpiresAt = time.Now().Add(time.Hour).Unix()
	// The initial API check succeeds. Revoke while its remote plan preparation
	// would be in flight; Issue must independently refuse the now-stale request.
	if err := requireCurrentMarketInstallationManagement(ctx, claims); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.RemoveNamedGroupingPolicy("g", claims.ID, "lookup-fixture-role"); err != nil {
		t.Fatal(err)
	}
	userQueries, unexpected := managementSQLProbe(t, db)
	var redisDials atomic.Int64
	client := redis.NewClient(&redis.Options{Addr: "unused", MaxRetries: -1, Dialer: func(context.Context, string, string) (net.Conn, error) {
		redisDials.Add(1)
		return nil, errors.New("unexpected session-marker access")
	}})
	previousRedis := global.REDIS
	global.REDIS = client
	t.Cleanup(func() { global.REDIS = previousRedis; client.Close() })
	grant, err := IssueMarketInstallAuthorization(ctx, model.MarketInstallRequest{BundleKey: "bundle", Version: "1"}, model.MarketInstallPlan{}, claims, "original-live-session", "original-key")
	requireManagementDenied(t, err)
	if grant != nil || *userQueries != 1 || *unexpected != 0 || redisDials.Load() != 0 {
		t.Fatalf("revocation issued grant or changed session: grant=%#v user checks=%d unexpected SQL=%d redis dials=%d", grant, *userQueries, *unexpected, redisDials.Load())
	}
}
