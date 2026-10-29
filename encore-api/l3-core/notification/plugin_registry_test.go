package notification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"encore.app/l3-core/notification/contract"
)

const safeManifestFixture = `{"api_version":"1.0","plugin_id":"fixture-mail","name":"Fixture Mail","plugin_version":"1.2.3","channels":["email"],"content_modes":["text"],"config_schema":{"type":"object","additionalProperties":false,"properties":{"region":{"type":"string","minLength":2},"password":{"type":"string","minLength":8}},"required":["region","password"]},"recipient_schema":{"type":"object","additionalProperties":false,"properties":{"kind":{"const":"email"},"address":{"type":"string","format":"email"}},"required":["kind","address"]},"secret_fields":["password"],"capabilities":{"delivery_receipts":true}}`

func TestUnconfiguredPluginClientDeniesOutboundOrigins(t *testing.T) {
	client, err := NewSafePluginClient(PluginHTTPOptions{})
	if err != nil {
		t.Fatalf("unconfigured notification provider prevented startup: %v", err)
	}
	for _, origin := range []string{"https://example.com", "https://frontend:8443", "http://127.0.0.1:8080"} {
		if _, _, err := client.FetchManifest(context.Background(), origin, "unused"); err != ErrPluginEndpoint {
			t.Fatalf("unlisted provider %q was not denied: %v", origin, err)
		}
	}
}

func TestPluginEndpointRejectsUnsafeOrigins(t *testing.T) {
	for _, origin := range []string{
		"http://127.0.0.1:8080", "https://user:pass@example.com", "https://example.com/path", "https://example.com/", "https://example.com/?token=x", "https://example.com?", "https://example.com/#fragment", "https://example.com/#", "file:///etc/passwd",
	} {
		if _, err := pluginEndpoint(origin, "/v1/manifest", PluginHTTPOptions{}); err == nil {
			t.Errorf("unsafe origin accepted: %q", origin)
		}
	}
	if _, err := pluginEndpoint("http://127.0.0.1:8080", "/v1/manifest", PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true}); err != nil {
		t.Fatalf("explicit loopback test origin rejected: %v", err)
	}
	if _, err := pluginEndpoint("http://localhost:8080", "/v1/manifest", PluginHTTPOptions{AllowHTTPTestOrigin: true}); err == nil {
		t.Fatal("HTTP was accepted without test mode")
	}
	if _, err := pluginEndpoint("https://example.com", "/v1/manifest", PluginHTTPOptions{}); err == nil {
		t.Fatal("unlisted HTTPS origin was accepted")
	}
	if _, err := pluginEndpoint("https://example.com", "/v1/manifest", PluginHTTPOptions{AllowedOrigins: []string{"https://example.com"}}); err != nil {
		t.Fatalf("listed HTTPS origin rejected: %v", err)
	}
}

func TestPluginIPAllowlistPinsPrivateCIDRsAndBlocksMetadata(t *testing.T) {
	endpoint := &url.URL{Scheme: "https", Host: "plugin.internal.example:443"}
	private := netip.MustParseAddr("10.24.1.5")
	if pluginIPAllowed(private, endpoint, PluginHTTPOptions{}) {
		t.Fatal("private plugin address accepted without an operator CIDR")
	}
	options := PluginHTTPOptions{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.24.0.0/16")}}
	if !pluginIPAllowed(private, endpoint, options) {
		t.Fatal("operator-allowlisted private plugin address rejected")
	}
	for _, raw := range []string{"127.0.0.1", "169.254.169.254", "100.100.100.200", "224.0.0.1", "0.1.2.3"} {
		ip := netip.MustParseAddr(raw)
		if pluginIPAllowed(ip, endpoint, PluginHTTPOptions{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}}) {
			t.Errorf("unsafe IP accepted even with broad CIDR: %s", raw)
		}
	}
}

func TestSafePluginClientRejectsRedirectAndValidatesExactManifestBytes(t *testing.T) {
	var redirected atomic.Int32
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		w.Write([]byte(safeManifestFixture))
	}))
	defer redirectTarget.Close()
	redirectOrigin := strings.TrimPrefix(redirectTarget.URL, "http://")
	redirectHost, redirectPort, _ := strings.Cut(redirectOrigin, ":")
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/manifest" || r.Header.Get("Authorization") != "Bearer fixture-plugin-auth" {
			t.Errorf("request path/auth mismatch")
		}
		http.Redirect(w, r, "http://"+redirectHost+":"+redirectPort+"/secret", http.StatusFound)
	}))
	defer redirectServer.Close()
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true})
	if err != nil {
		t.Fatal(err)
	}
	origin := strings.TrimSuffix(redirectServer.URL, "/")
	if _, _, err := client.FetchManifest(context.Background(), origin, "fixture-plugin-auth"); err == nil {
		t.Fatal("redirect response accepted as manifest")
	}
	if redirected.Load() != 0 {
		t.Fatal("redirect target received a request")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/manifest" || r.Header.Get("Authorization") != "Bearer fixture-plugin-auth" {
			t.Errorf("request path/auth mismatch")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(safeManifestFixture))
	}))
	defer server.Close()
	raw, manifest, err := client.FetchManifest(context.Background(), server.URL, "fixture-plugin-auth")
	if err != nil {
		t.Fatalf("fetch safe manifest: %v", err)
	}
	if string(raw) != safeManifestFixture || manifest.PluginID != "fixture-mail" || pluginManifestDigest(raw) != "sha256:"+strings.Repeat("0", 64) && !compareManifestDigest(pluginManifestDigest(raw), raw) {
		t.Fatal("raw manifest or exact-byte digest was not preserved")
	}
	if err := validatePluginManifest(manifest); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	bad := manifest
	bad.SecretFields = []string{"missing"}
	if validatePluginManifest(bad) == nil {
		t.Fatal("secret field outside config schema accepted")
	}
	badMode := manifest
	badMode.ContentModes = []string{"text_template"}
	if validatePluginManifest(badMode) == nil {
		t.Fatal("unsupported content mode accepted")
	}
}

func TestHTTPPluginOriginsRequireExplicitLoopbackAllowlist(t *testing.T) {
	var allowedCalls, otherCalls atomic.Int32
	allowedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedCalls.Add(1)
		if r.URL.Path != "/v1/manifest" || r.Header.Get("Authorization") != "Bearer fixture-plugin-auth" {
			t.Errorf("request path/auth mismatch")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(safeManifestFixture))
	}))
	defer allowedServer.Close()
	otherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(safeManifestFixture))
	}))
	defer otherServer.Close()

	options := PluginHTTPOptions{
		TestMode:            true,
		AllowHTTPTestOrigin: true,
		AllowedOrigins:      []string{allowedServer.URL},
	}
	client, err := NewSafePluginClient(options)
	if err != nil {
		t.Fatalf("construct explicit loopback test client: %v", err)
	}
	if _, _, err := client.FetchManifest(context.Background(), allowedServer.URL, "fixture-plugin-auth"); err != nil {
		t.Fatalf("allowed loopback HTTP manifest rejected: %v", err)
	}
	if allowedCalls.Load() != 1 {
		t.Fatalf("allowed loopback server calls = %d, want 1", allowedCalls.Load())
	}
	if _, _, err := client.FetchManifest(context.Background(), otherServer.URL, "fixture-plugin-auth"); err == nil {
		t.Fatal("different loopback port outside the exact allowlist was accepted")
	}
	if otherCalls.Load() != 0 {
		t.Fatalf("non-allowlisted loopback server calls = %d, want 0", otherCalls.Load())
	}

	for _, tc := range []struct {
		name    string
		options PluginHTTPOptions
	}{
		{
			name: "remote HTTP origin in test mode",
			options: PluginHTTPOptions{
				TestMode:            true,
				AllowHTTPTestOrigin: true,
				AllowedOrigins:      []string{"http://plugin.example.test:19586"},
			},
		},
		{
			name: "loopback HTTP origin in production mode",
			options: PluginHTTPOptions{
				AllowedOrigins: []string{"http://127.0.0.1:19586"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewSafePluginClient(tc.options); err == nil {
				t.Fatal("unsafe HTTP allowlist accepted")
			}
		})
	}
}

func TestDigestAndSafeManifestSummaryNeverIncludeAuthSecret(t *testing.T) {
	raw := []byte(safeManifestFixture)
	manifest := contract.PluginManifestV1{}
	if err := contract.DecodeStrictJSON(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if compareManifestDigest(pluginManifestDigest(raw)+"\n", raw) || compareManifestDigest("sha256:"+strings.Repeat("f", 64), raw) {
		t.Fatal("manifest digest comparison accepted non-exact bytes")
	}
	if err := validatePluginManifest(manifest); err != nil {
		t.Fatal(err)
	}
}
