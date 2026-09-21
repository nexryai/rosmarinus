package security

import (
	"net/netip"
	"strings"
)

// IsPrivateAddress reports whether address is an IP literal that must not be
// reached by server-side fetches or embedded as an external link target.
// Addresses that cannot be parsed fail closed.
func IsPrivateAddress(address string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return true
	}
	// Unmap IPv4-mapped IPv6 addresses so they are judged by their IPv4
	// semantics instead of slipping through as a public-looking IPv6 address.
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() ||
		addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() {
		return true
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// nonPublicPrefixes lists global-unicast ranges that are still not reachable
// public endpoints, including Bogon ranges that the net package does not
// classify on its own (https://ipinfo.io/bogon).
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}
