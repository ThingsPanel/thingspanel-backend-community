package sdk

import (
	"net"
	"net/netip"
)

func PublicUnicast(ip net.IP) bool {
	parsed, err := netip.ParseAddr(ip.String())
	if err != nil || !parsed.IsGlobalUnicast() || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsMulticast() || parsed.IsUnspecified() {
		return false
	}
	if parsed.Is4() {
		for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4"} {
			if netip.MustParsePrefix(prefix).Contains(parsed) {
				return false
			}
		}
	}
	if parsed.Is6() {
		for _, prefix := range []string{"2001:db8::/32", "2001:10::/28"} {
			if netip.MustParsePrefix(prefix).Contains(parsed) {
				return false
			}
		}
	}
	return true
}
