package security

import "testing"

func TestIsPrivateAddress(t *testing.T) {
	tests := map[string]bool{
		"8.8.8.8":              false,
		"1.1.1.1":              false,
		"2606:4700:4700::1111": false,
		"127.0.0.1":            true,
		"10.0.0.1":             true,
		"172.16.5.4":           true,
		"192.168.1.1":          true,
		"169.254.1.1":          true,
		"0.0.0.0":              true,
		"100.100.100.100":      true,
		"192.0.2.1":            true,
		"198.51.100.1":         true,
		"203.0.113.1":          true,
		"240.0.0.1":            true,
		"::1":                  true,
		"fc00::1":              true,
		"fe80::1":              true,
		"::ffff:127.0.0.1":     true,
		"64:ff9b::7f00:1":      true,
		"not-an-ip":            true,
		"":                     true,
	}
	for address, want := range tests {
		if got := IsPrivateAddress(address); got != want {
			t.Errorf("IsPrivateAddress(%q) = %v, want %v", address, got, want)
		}
	}
}
