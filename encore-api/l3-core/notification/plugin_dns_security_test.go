package notification

import (
	"context"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// An actual local DNS server returns unsafe answers. Production-shaped clients
// reject all of them before any connection/token reaches the target.
func TestPluginDNSMixedRebindingAndMetadataAnswersNeverReceiveToken(t *testing.T) {
	var sinkCalls atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sinkCalls.Add(1); w.WriteHeader(http.StatusOK) }))
	defer sink.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(sink.URL, "http://"))
	for _, tc := range []struct {
		name    string
		answers []netip.Addr
	}{
		{"mixed public and loopback", []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}},
		{"rebound loopback", []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{"metadata", []netip.Addr{netip.MustParseAddr("169.254.169.254")}},
		{"aliyun metadata", []netip.Addr{netip.MustParseAddr("100.100.100.200")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dns, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal("fixture DNS listen failed")
			}
			defer dns.Close()
			var dnsCalls atomic.Int32
			done := make(chan struct{})
			go func() {
				defer close(done)
				buf := make([]byte, 4096)
				for {
					n, remote, err := dns.ReadFrom(buf)
					if err != nil {
						return
					}
					var q dnsmessage.Message
					if q.Unpack(buf[:n]) != nil {
						continue
					}
					dnsCalls.Add(1)
					response := dnsmessage.Message{Header: dnsmessage.Header{ID: q.Header.ID, Response: true, Authoritative: true, RecursionDesired: q.Header.RecursionDesired, RecursionAvailable: true}, Questions: q.Questions}
					for _, question := range q.Questions {
						if question.Type == dnsmessage.TypeA {
							for _, ip := range tc.answers {
								response.Answers = append(response.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 0}, Body: &dnsmessage.AResource{A: ip.As4()}})
							}
						}
					}
					wire, err := response.Pack()
					if err == nil {
						_, _ = dns.WriteTo(wire, remote)
					}
				}
			}()
			resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "udp", dns.LocalAddr().String())
			}}
			origin := "https://plugin.rebinding.test:" + port
			client, err := NewSafePluginClient(PluginHTTPOptions{Resolver: resolver, Timeout: time.Second, AllowedOrigins: []string{origin}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}})
			if err != nil {
				t.Fatal("production-shaped client construction failed")
			}
			if _, _, err = client.FetchManifest(context.Background(), origin, "FIXTURE_DNS_ONLY_PLUGIN_TOKEN"); err == nil {
				t.Fatal("unsafe DNS answer was accepted")
			}
			if dnsCalls.Load() == 0 || sinkCalls.Load() != 0 {
				t.Fatalf("DNS calls=%d unsafe endpoint calls=%d", dnsCalls.Load(), sinkCalls.Load())
			}
			_ = dns.Close()
			<-done
		})
	}
}
