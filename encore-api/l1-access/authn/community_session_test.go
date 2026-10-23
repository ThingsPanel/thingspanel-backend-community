package authn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckCommunitySessionAcceptsMatchingActiveSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != communitySessionPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-token"); got != "signed-token" {
			t.Errorf("unexpected x-token header: %q", got)
		}
		_, _ = fmt.Fprint(w, `{"code":200,"data":{"id":"user-1","tenant_id":"tenant-1","authority":"TENANT_USER","status":"N"}}`)
	}))
	defer server.Close()

	if err := checkCommunitySession(context.Background(), server.URL, "signed-token", sessionTestIdentity()); err != nil {
		t.Fatalf("matching active session was rejected: %v", err)
	}
}

func TestCheckCommunitySessionRejectsRevokedAndInvalidBusinessResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "revoked", statusCode: http.StatusUnauthorized, body: `{"code":40102}`},
		{name: "business error", statusCode: http.StatusOK, body: `{"code":40102,"data":{"id":"user-1","tenant_id":"tenant-1","authority":"TENANT_USER","status":"N"}}`},
		{name: "missing data", statusCode: http.StatusOK, body: `{"code":200}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			if err := checkCommunitySession(context.Background(), server.URL, "signed-token", sessionTestIdentity()); err == nil {
				t.Fatal("invalid session response was accepted")
			}
		})
	}
}

func TestCheckCommunitySessionRejectsIdentityRoleAndStatusMismatch(t *testing.T) {
	users := []string{
		`{"id":"user-1","tenant_id":"other-tenant","authority":"TENANT_USER","status":"N"}`,
		`{"id":"user-1","tenant_id":"tenant-1","authority":"TENANT_ADMIN","status":"N"}`,
		`{"id":"user-1","tenant_id":"tenant-1","authority":"TENANT_USER","status":"F"}`,
	}
	for i, user := range users {
		t.Run(fmt.Sprintf("mismatch-%d", i), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"code":200,"data":%s}`, user)
			}))
			defer server.Close()
			if err := checkCommunitySession(context.Background(), server.URL, "signed-token", sessionTestIdentity()); err == nil {
				t.Fatal("identity or status mismatch was accepted")
			}
		})
	}
}

func TestCheckCommunitySessionDoesNotForwardTokenOnRedirect(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls++
		if r.Header.Get("x-token") != "" {
			t.Error("token was forwarded to redirect target")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+communitySessionPath)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()

	if err := checkCommunitySession(context.Background(), redirect.URL, "sensitive-token", sessionTestIdentity()); err == nil {
		t.Fatal("redirect response was accepted")
	}
	if targetCalls != 0 {
		t.Fatalf("redirect target was called %d times", targetCalls)
	}
}

func TestCheckCommunitySessionDoesNotExposeTokenOrResponseBody(t *testing.T) {
	const token = "sensitive-token-value"
	const privateBody = "private-response-body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"code":50001,"message":%q}`, token+privateBody)
	}))
	defer server.Close()

	var logs bytes.Buffer
	logger := log.Default()
	previousOutput := logger.Writer()
	logger.SetOutput(&logs)
	defer logger.SetOutput(previousOutput)

	err := checkCommunitySession(context.Background(), server.URL, token, sessionTestIdentity())
	if err == nil {
		t.Fatal("business error response was accepted")
	}
	for _, secret := range []string{token, privateBody} {
		if strings.Contains(err.Error(), secret) || strings.Contains(logs.String(), secret) {
			t.Fatalf("failure leaked sensitive value %q", secret)
		}
	}
}

func TestCheckCommunitySessionRejectsInvalidBaseURLAndOversizedBody(t *testing.T) {
	for _, rawURL := range []string{
		"ftp://community.example",
		"https://user:password@community.example",
		"https://community.example?token=secret",
		"https://community.example/#fragment",
		"https://community.example/prefix",
	} {
		if _, err := communitySessionEndpoint(rawURL); err == nil {
			t.Errorf("accepted unsafe base URL %q", rawURL)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", communitySessionMaxBody+1))
	}))
	defer server.Close()
	if err := checkCommunitySession(context.Background(), server.URL, "signed-token", sessionTestIdentity()); err == nil {
		t.Fatal("oversized response body was accepted")
	}
}

func TestCheckCommunitySessionHonorsCancellationAndNetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := checkCommunitySession(ctx, server.URL, "signed-token", sessionTestIdentity()); err == nil {
		t.Fatal("cancelled session check was accepted")
	}
	server.Close()

	if err := checkCommunitySession(context.Background(), server.URL, "signed-token", sessionTestIdentity()); err == nil {
		t.Fatal("unreachable Community endpoint was accepted")
	}
}

func sessionTestIdentity() *Data {
	return &Data{UserID: "user-1", TenantID: "tenant-1", Roles: []string{"TENANT_USER"}}
}
