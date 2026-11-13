package communityclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func callerReadContext(token string) context.Context {
	r := httptest.NewRequest(http.MethodGet, "/native-read", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return WithAuthenticatedReadRequest(r.Context(), r)
}

func TestAuthenticatedReadConcurrentCallerIsolation(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		expected := strings.TrimPrefix(r.URL.Path, "/read/")
		if r.Header.Get("x-token") != expected || r.Header.Get("Authorization") != "Bearer "+expected || r.Header.Get("x-api-key") != "" {
			t.Error("upstream received a different caller or service credential")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `{"code":200}`)
	}))
	defer ts.Close()
	// A configured service key must not take precedence over the caller identity.
	c := &Client{baseURL: ts.URL, apiKey: "service-key", http: ts.Client()}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := fmt.Sprintf("caller-%d", i)
			ctx := callerReadContext(token)
			got, err := c.Login(ctx)
			if err != nil || got != token {
				t.Error("request login did not retain caller credential")
				return
			}
			if _, err := c.doRaw(ctx, http.MethodGet, "/read/"+token, got, nil); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if calls.Load() != 64 || c.apiKey != "service-key" {
		t.Fatalf("requests=%d or shared client mutated", calls.Load())
	}
}

func TestAuthenticatedReadWorksWithOnlyEndpoint(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/api/v1/login" || r.Header.Get("x-token") != "caller" {
			t.Error("unexpected service login or missing request token")
		}
		fmt.Fprint(w, `{"code":200,"data":{"is_online":1}}`)
	}))
	defer ts.Close()
	c := &Client{baseURL: ts.URL, http: ts.Client()}
	if c.Configured() {
		t.Fatal("fixture unexpectedly has service credentials")
	}
	for _, header := range []string{"Authorization", "x-token", "both"} {
		r := httptest.NewRequest(http.MethodGet, "/native-read", nil)
		value := "caller"
		if header == "Authorization" {
			value = "Bearer caller"
		}
		if header == "both" {
			r.Header.Set("Authorization", "Bearer caller")
			r.Header.Set("x-token", "caller")
		} else {
			r.Header.Set(header, value)
		}
		ctx := WithAuthenticatedReadRequest(r.Context(), r)
		// Headers can be reused/mutated after extraction without changing the caller.
		r.Header.Set(header, "different")
		flag, err := c.GetOnlineFlag(ctx, "own-device")
		if err != nil || flag != 1 {
			t.Fatalf("read with %s: flag=%d err=%v", header, flag, err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("unexpected upstream calls: %d", calls.Load())
	}
	if _, err := c.Login(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatal("background request inherited caller credentials")
	}
}

func TestAuthenticatedReadRejectsMissingOrAmbiguousCredentialWithoutFallback(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer ts.Close()
	c := &Client{baseURL: ts.URL, apiKey: "service-key", email: "service", password: "secret", http: ts.Client()}
	cases := []http.Header{
		{},
		{"Authorization": {"Bearer "}},
		{"Authorization": {"Basic abc"}, "X-Token": {"caller"}},
		{"Authorization": {"Bearer a"}, "X-Token": {"b"}},
		{"Authorization": {"Bearer a", "Bearer a"}},
		{"X-Token": {"a", "a"}},
		{"X-Token": {"a,b"}},
		{"X-Token": {"a\nb"}},
		{"X-Token": {strings.Repeat("a", 16385)}},
	}
	for i, h := range cases {
		r := httptest.NewRequest(http.MethodGet, "/native-read", nil)
		r.Header = h
		ctx := WithAuthenticatedReadRequest(r.Context(), r)
		if _, err := c.GetOnlineFlag(ctx, "device"); !errors.Is(err, ErrRequestAuthentication) {
			t.Errorf("case %d: missing credential was not rejected: %v", i, err)
		}
		if _, err := c.doRaw(ctx, http.MethodGet, "/read", "fallback", nil); !errors.Is(err, ErrRequestAuthentication) {
			t.Errorf("case %d: direct request accepted fallback", i)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid authentication sent %d upstream requests", calls.Load())
	}
}

func TestAuthenticatedReadRejectsWritesAndDoesNotFollowRedirect(t *testing.T) {
	var unexpected atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/read" {
			http.Redirect(w, r, "/credential-sink", http.StatusFound)
			return
		}
		unexpected.Add(1)
	}))
	defer ts.Close()
	c := &Client{baseURL: ts.URL, http: ts.Client()}
	ctx := callerReadContext("caller")
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if _, err := c.doRaw(ctx, method, "/write", "caller", nil); !errors.Is(err, ErrRequestAuthentication) {
			t.Errorf("request-scoped read allowed %s", method)
		}
	}
	if _, err := c.doRaw(ctx, http.MethodGet, "/read", "caller", nil); err == nil {
		t.Fatal("redirect did not fail closed")
	}
	if unexpected.Load() != 0 || c.http.CheckRedirect != nil {
		t.Fatal("redirect leaked credentials or shared HTTP client was mutated")
	}
}

func TestAuthenticatedReadUpstreamRejectionAndCancellation(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()
	c := &Client{baseURL: ts.URL, apiKey: "service-key", http: ts.Client()}
	ctx := callerReadContext("caller")
	if _, err := c.GetOnlineFlag(ctx, "foreign-device"); !errors.Is(err, ErrRejected) {
		t.Fatalf("upstream denial not retained: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("upstream denial retried with different credentials")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.GetOnlineFlag(cancelled, "device"); !errors.Is(err, context.Canceled) {
		t.Fatalf("request cancellation lost: %v", err)
	}
}
