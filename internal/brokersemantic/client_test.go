package brokersemantic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	tenantID   = "tenant-a"
	deviceID   = "device-a"
	templateID = "template-a"
	modelID    = "model-a"
	snapshotID = "snapshot-a"
)

func validEncoreHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer delegated-token" {
			http.Error(w, "invalid method or authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/devices/device-a":
			fmt.Fprintf(w, `{"id":"%s","tenant_id":"%s","device_template_id":"%s"}`, deviceID, tenantID, templateID)
		case "/api/device-templates/template-a":
			fmt.Fprintf(w, `{"id":"%s","tenant_id":"%s","thing_model_id":"%s","thing_model_snapshot_id":"%s"}`, templateID, tenantID, modelID, snapshotID)
		case "/api/v2/thing-models/model-a/versions":
			fmt.Fprintf(w, `{"items":[{"publishedRevision":12,"snapshotID":"%s"}],"page":1,"pageSize":200,"total":1}`, snapshotID)
		case "/api/v2/thing-models/model-a/versions/12":
			fmt.Fprintf(w, `{"modelID":"%s","publishedRevision":12,"snapshotID":"%s","model":{"fields":[{"identifier":"temp"}]},"resources":{"modelID":"%s","revision":9,"profile":{},"webChartConfig":{},"appChartConfig":{}}}`, modelID, snapshotID, modelID)
		default:
			http.NotFound(w, r)
		}
	})
}

func testClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c, err := NewClient(Config{BaseURL: srv.URL, Development: true})
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return c, srv
}

func describe(c *Client) (Description, error) {
	return c.Describe(context.Background(), tenantID, "subject-a", "delegated-token", deviceID)
}

func TestDescribeReturnsExactPublishedSnapshotAndPins(t *testing.T) {
	c, _ := testClient(t, validEncoreHandler())
	result, err := describe(c)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pins.TenantID != tenantID || result.Pins.SubjectID != "subject-a" || result.Pins.SnapshotID != snapshotID || result.Pins.PublishedRevision != 12 || result.Pins.ResourceRevision != 9 {
		t.Fatalf("wrong semantic pins: %+v", result.Pins)
	}
	if !strings.Contains(string(result.Model), `"temp"`) || !strings.Contains(string(result.Resources), `"revision":9`) {
		t.Fatalf("semantic documents missing: model=%s resources=%s", result.Model, result.Resources)
	}
}

func TestDescribeAcceptsExplicitZeroResourceRevisionButRejectsMissingOrNull(t *testing.T) {
	for _, tc := range []struct {
		name, resources string
		wantErr         bool
	}{
		{name: "revision-zero-is-published-shape", resources: `{"modelID":"model-a","revision":0}`, wantErr: false},
		{name: "revision-missing", resources: `{"modelID":"model-a"}`, wantErr: true},
		{name: "revision-null", resources: `{"modelID":"model-a","revision":null}`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/thing-models/model-a/versions/12" {
					fmt.Fprintf(w, `{"modelID":"model-a","publishedRevision":12,"snapshotID":"snapshot-a","model":{"fields":[]},"resources":%s}`, tc.resources)
					return
				}
				validEncoreHandler().ServeHTTP(w, r)
			})
			c, _ := testClient(t, h)
			result, err := describe(c)
			if tc.wantErr {
				if err != ErrSemanticUnavailable {
					t.Fatalf("error=%v, want unavailable", err)
				}
				return
			}
			if err != nil || result.Pins.ResourceRevision != 0 {
				t.Fatalf("explicit revision zero result=(%+v,%v)", result.Pins, err)
			}
		})
	}
}

func TestNewClientRequiresHTTPSExceptExplicitLoopbackDevelopment(t *testing.T) {
	for _, cfg := range []Config{
		{BaseURL: "http://example.com", Development: true},
		{BaseURL: "http://127.0.0.1:1234"},
		{BaseURL: "https://example.com/path"},
		{BaseURL: "https://user:pass@example.com"},
		{BaseURL: "https://example.com?tenant=x"},
	} {
		if _, err := NewClient(cfg); err == nil {
			t.Fatalf("accepted unsafe URL config %+v", cfg)
		}
	}
}

func TestDescribeRejectsRedirectAndIdentityOrSnapshotMismatch(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		var target *httptest.Server
		redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/api/devices/device-a", http.StatusFound)
		}))
		defer redirect.Close()
		target = httptest.NewServer(validEncoreHandler())
		defer target.Close()
		c, err := NewClient(Config{BaseURL: redirect.URL, Development: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := describe(c); err != ErrUnavailable {
			t.Fatalf("redirect error=%v", err)
		}
	})
	t.Run("cross-tenant", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/devices/device-a" {
				fmt.Fprint(w, `{"id":"device-a","tenant_id":"other","device_template_id":"template-a"}`)
				return
			}
			validEncoreHandler().ServeHTTP(w, r)
		})
		c, _ := testClient(t, h)
		if _, err := describe(c); err != ErrSemanticUnavailable {
			t.Fatalf("cross-tenant error=%v", err)
		}
	})
	t.Run("wrong-exact-snapshot", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/thing-models/model-a/versions" {
				fmt.Fprint(w, `{"items":[{"publishedRevision":99,"snapshotID":"newer"}],"page":1,"pageSize":200,"total":1}`)
				return
			}
			validEncoreHandler().ServeHTTP(w, r)
		})
		c, _ := testClient(t, h)
		if _, err := describe(c); err != ErrSemanticUnavailable {
			t.Fatalf("unmatched snapshot error=%v", err)
		}
	})
	t.Run("wrong-version-response", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/thing-models/model-a/versions/12" {
				fmt.Fprint(w, `{"modelID":"model-a","publishedRevision":12,"snapshotID":"other","model":{"fields":[]},"resources":{"modelID":"model-a","revision":9}}`)
				return
			}
			validEncoreHandler().ServeHTTP(w, r)
		})
		c, _ := testClient(t, h)
		if _, err := describe(c); err != ErrSemanticUnavailable {
			t.Fatalf("wrong snapshot detail error=%v", err)
		}
	})
}

func TestDescribePaginationBoundAndLegacyBindingFailClosed(t *testing.T) {
	t.Run("finds-snapshot-on-next-page", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/thing-models/model-a/versions" {
				if r.URL.Query().Get("page") == "1" {
					items := strings.TrimSuffix(strings.Repeat(`{"publishedRevision":1,"snapshotID":"older"},`, 200), ",")
					fmt.Fprintf(w, `{"items":[%s],"page":1,"pageSize":200,"total":201}`, items)
				} else {
					fmt.Fprintf(w, `{"items":[{"publishedRevision":12,"snapshotID":"%s"}],"page":2,"pageSize":200,"total":201}`, snapshotID)
				}
				return
			}
			validEncoreHandler().ServeHTTP(w, r)
		})
		c, _ := testClient(t, h)
		if _, err := describe(c); err != nil {
			t.Fatalf("second-page snapshot: %v", err)
		}
	})
	t.Run("over-limit-fails-closed", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/thing-models/model-a/versions" {
				fmt.Fprint(w, `{"items":[],"page":1,"pageSize":200,"total":1001}`)
				return
			}
			validEncoreHandler().ServeHTTP(w, r)
		})
		c, _ := testClient(t, h)
		if _, err := describe(c); err != ErrSemanticUnavailable {
			t.Fatalf("over-limit error=%v", err)
		}
	})
	t.Run("legacy-snapshot-not-found", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/thing-models/model-a/versions" {
				fmt.Fprint(w, `{"items":[],"page":1,"pageSize":200,"total":0}`)
				return
			}
			validEncoreHandler().ServeHTTP(w, r)
		})
		c, _ := testClient(t, h)
		if _, err := describe(c); err != ErrSemanticUnavailable {
			t.Fatalf("legacy binding error=%v", err)
		}
	})
}

func TestDescribeRejectsOversizeTimeoutCancellationAndDuplicateKeys(t *testing.T) {
	t.Run("oversize", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/devices/device-a" {
				fmt.Fprintf(w, `{"id":"device-a","tenant_id":"%s","device_template_id":"template-a"}`, tenantID)
				fmt.Fprint(w, strings.Repeat(" ", maxResponse))
				return
			}
			validEncoreHandler().ServeHTTP(w, r)
		})
		c, _ := testClient(t, h)
		if _, err := describe(c); err != ErrResponse {
			t.Fatalf("oversize error=%v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		c, srv := testClient(t, h)
		defer srv.CloseClientConnections()
		started := time.Now()
		if _, err := describe(c); err != ErrUnavailable {
			t.Fatalf("timeout error=%v", err)
		}
		if time.Since(started) > 3*time.Second {
			t.Fatal("request timeout exceeded bound")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		c, srv := testClient(t, h)
		defer srv.CloseClientConnections()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.Describe(ctx, tenantID, "subject-a", "delegated-token", deviceID); err != ErrUnavailable {
			t.Fatalf("cancel error=%v", err)
		}
	})
	t.Run("duplicate-keys", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/devices/device-a" {
				fmt.Fprintf(w, `{"id":"device-a","tenant_id":"%s","tenant_id":"other","device_template_id":"template-a"}`, tenantID)
				return
			}
			validEncoreHandler().ServeHTTP(w, r)
		})
		c, _ := testClient(t, h)
		if _, err := describe(c); err != ErrResponse {
			t.Fatalf("duplicate key error=%v", err)
		}
	})
}
