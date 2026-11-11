package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/model"
	"project/pkg/utils"
)

func TestDashboardPublicationOperationIdentity(t *testing.T) {
	claims := &utils.UserClaims{TenantID: "tenant-a", ID: "subject-a"}
	key, err := dashboardPublicationOperationKey(claims, "original/key")
	if err != nil || len(key) != 64 {
		t.Fatalf("key=%q err=%v", key, err)
	}
	replay, _ := dashboardPublicationOperationKey(claims, "original/key")
	if replay != key {
		t.Fatal("same operation changed identity")
	}
	for _, changed := range []struct{ tenant, subject, key string }{
		{"tenant-b", "subject-a", "original/key"}, {"tenant-a", "subject-b", "original/key"}, {"tenant-a", "subject-a", "new/key"},
	} {
		other, err := dashboardPublicationOperationKey(&utils.UserClaims{TenantID: changed.tenant, ID: changed.subject}, changed.key)
		if err != nil || other == key {
			t.Fatalf("scope collision: %+v err=%v", changed, err)
		}
	}
	for _, invalid := range []string{"", " leading", "trailing ", "newline\nkey", strings.Repeat("x", 129)} {
		if _, err := dashboardPublicationOperationKey(claims, invalid); err == nil {
			t.Fatalf("accepted invalid key %q", invalid)
		}
	}
	if _, err := dashboardPublicationOperationKey(nil, "original"); err == nil {
		t.Fatal("missing verified platform identity accepted")
	}
}

func TestPublicationReceiptProxyKeepsOriginalTargetAndIdentity(t *testing.T) {
	claims := &utils.UserClaims{TenantID: "tenant-a", ID: "subject-a"}
	key, _ := dashboardPublicationOperationKey(claims, "original/key")
	state := "deleted"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/market/bundles/publication-receipts/"+key || r.URL.RawQuery != "" {
			t.Errorf("unexpected query method/path: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer original-market-token" || r.Header.Get("X-User-Id") != "" || r.Header.Get("X-Publisher-Id") != "" {
			t.Error("publisher must be established by the verified market token, not a caller header")
		}
		data := &model.PublicationReceiptResponse{State: "confirmed", TargetState: state, Result: &model.HorizonPublishData{
			BundleKey: "original-bundle", Version: "1.0.0", VersionID: "original-version", ContentHash: "sha256:original", Status: "pending_review", TargetState: state,
		}}
		if state == "not_observed" {
			data = &model.PublicationReceiptResponse{State: state}
		}
		_ = json.NewEncoder(w).Encode(model.HorizonPublicationReceiptResponse{Data: data})
	}))
	defer server.Close()
	service := &MarketDashboardBundleService{market: &MarketClient{baseURL: server.URL, httpClient: server.Client()}}
	// thingsvis is nil: recovery may not export/recreate the old source.
	for _, target := range []string{"deleted", "unknown", "pending_review", "published", "rejected", "unpublished", "not_observed"} {
		state = target
		result, err := service.QueryPublicationReceipt(context.Background(), "original/key", "original-market-token", claims)
		if err != nil || result.OperationKey != "original/key" {
			t.Fatalf("target=%s result=%+v err=%v", target, result, err)
		}
		if target == "not_observed" {
			if result.State != "not_observed" || result.Result != nil {
				t.Fatal("unobserved result was fabricated")
			}
		} else if result.State != "confirmed" || result.TargetState != target || result.Result.VersionID != "original-version" || result.Result.Status != "pending_review" {
			t.Fatalf("original receipt or current target changed: %+v", result)
		}
	}
	if calls != 7 {
		t.Fatalf("unexpected side effects/retries: %d queries", calls)
	}
}

func TestPublicationReceiptProxyRejectsUnknownAndContradictoryResponses(t *testing.T) {
	good := `{"code":0,"data":{"state":"confirmed","targetState":"published","result":{"bundleKey":"fixture","version":"1.0.0","contentHash":"sha256:fixture","status":"pending_review","versionId":"original-version","targetState":"published"}}}`
	for name, body := range map[string]string{
		"malformed": `{`, "no data": `{"code":0}`, "future state": `{"code":0,"data":{"state":"future"}}`,
		"contradictory absence": `{"code":0,"data":{"state":"not_observed","result":{}}}`,
		"mismatched target":     strings.Replace(good, `"targetState":"published"`, `"targetState":"deleted"`, 1),
		"missing result":        `{"code":0,"data":{"state":"confirmed"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			_, err := (&MarketClient{baseURL: server.URL, httpClient: server.Client()}).GetBundlePublicationReceipt(context.Background(), "token", "key")
			if !errors.Is(err, ErrMarketInvalidResponse) {
				t.Fatalf("response accepted: %s err=%v", body, err)
			}
		})
	}
	t.Run("historical receipt lacks version identity", func(t *testing.T) {
		body := strings.Replace(good, `"versionId":"original-version",`, "", 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		defer server.Close()
		result, err := (&MarketClient{baseURL: server.URL, httpClient: server.Client()}).GetBundlePublicationReceipt(context.Background(), "token", "key")
		if err != nil || result.TargetState != "unknown" || result.Result.VersionID != "" {
			t.Fatalf("legacy identity fabricated: %+v %v", result, err)
		}
	})
	t.Run("HTTP failure is not absence", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
		defer server.Close()
		result, err := (&MarketClient{baseURL: server.URL, httpClient: server.Client()}).GetBundlePublicationReceipt(context.Background(), "token", "key")
		if err == nil || result != nil {
			t.Fatalf("unknown query was treated as known: %+v %v", result, err)
		}
	})
}

func TestPublicationReceiptIsNotCurrentPublishSuccess(t *testing.T) {
	for _, target := range []string{"deleted", "unknown", "rejected", "unpublished", ""} {
		_, err := requireSuccessfulHorizonPublish(&model.HorizonPublishResponse{Data: &model.HorizonPublishData{
			BundleKey: "fixture", Version: "1.0.0", VersionID: "original-version", ContentHash: "sha256:original", Status: "pending_review", TargetState: target,
		}})
		if !errors.Is(err, ErrMarketInvalidResponse) {
			t.Fatalf("target=%q was returned as a successful current publication", target)
		}
	}
	_, err := requireSuccessfulHorizonPublish(&model.HorizonPublishResponse{Data: &model.HorizonPublishData{
		BundleKey: "fixture", Version: "1.0.0", ContentHash: "sha256:original", Status: "pending_review", TargetState: "published",
	}})
	if !errors.Is(err, ErrMarketInvalidResponse) {
		t.Fatal("legacy response without original version accepted as current")
	}
}

func TestMarketPublicationHTTPConflictPreserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusConflict) }))
	defer server.Close()
	_, err := (&MarketClient{baseURL: server.URL, httpClient: server.Client()}).PublishBundle(context.Background(), "token", "key", &model.HorizonPublishRequest{})
	var conflict *MarketPublicationConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrMarketRequestRejected) {
		t.Fatalf("conflict lost: %v", err)
	}
}
