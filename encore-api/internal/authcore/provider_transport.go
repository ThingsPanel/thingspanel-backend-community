package authcore

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"time"
)

// Providers are configured by trusted administrators, but their URLs must not
// turn sign-in tests/JWKS fetches into an unrestricted server-side network proxy.
// Private enterprise IdPs require an exact HTTPS origin in deployment policy.
func dialProvider(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, ErrUnavailable
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	var origins []string
	if json.Unmarshal([]byte(os.Getenv("TP_AUTH_OIDC_ALLOWED_ORIGINS")), &origins) == nil {
		for _, origin := range origins {
			u, e := url.Parse(origin)
			if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
				continue
			}
			p := u.Port()
			if p == "" {
				p = "443"
			}
			if u.Hostname() == host && p == port {
				return d.DialContext(ctx, network, address)
			}
		}
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil || len(ips) == 0 {
		return nil, ErrUnavailable
	}
	for _, entry := range ips {
		if !publicProviderIP(entry.IP) {
			return nil, ErrForbidden
		}
	}
	var last error
	for _, entry := range ips {
		c, e := d.DialContext(ctx, network, net.JoinHostPort(entry.IP.String(), port))
		if e == nil {
			return c, nil
		}
		last = e
	}
	return nil, last
}

func publicProviderIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15"} {
		_, n, _ := net.ParseCIDR(block)
		if n.Contains(ip) {
			return false
		}
	}
	return true
}
