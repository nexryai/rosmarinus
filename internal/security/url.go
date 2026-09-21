package security

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateURL reports why checkURL is unsafe to trust as an external URL.
// Callers must pass allowUnsafeConnections=true only for note bodies authored
// by remote users, whose links may legitimately use plain http.
func ValidateURL(checkURL string, allowUnsafeConnections bool) error {
	if checkURL == "" || strings.TrimSpace(checkURL) != checkURL {
		return fmt.Errorf("url must not be empty or padded with whitespace")
	}
	if strings.ContainsAny(checkURL, "\x00\r\n\t") {
		return fmt.Errorf("url must not contain control characters")
	}
	parsed, err := url.Parse(checkURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if parsed.Scheme != "https" && (!allowUnsafeConnections || parsed.Scheme != "http") {
		return fmt.Errorf("url must use https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("url must be absolute and include a host")
	}
	if parsed.User != nil {
		return fmt.Errorf("url must not contain credentials")
	}
	host := strings.ToLower(strings.TrimRight(parsed.Hostname(), "."))
	if strings.Contains(host, ":") {
		return fmt.Errorf("url must not use an IPv6 literal host")
	}
	if isBlockedHostname(host) {
		return fmt.Errorf("url host is not public: %s", host)
	}
	if isNumericHost(host) {
		ip := net.ParseIP(host)
		if ip == nil || IsPrivateAddress(ip.String()) {
			return fmt.Errorf("url host is not public: %s", host)
		}
	} else {
		if !strings.Contains(host, ".") {
			return fmt.Errorf("url host must be a fully qualified domain name")
		}
		if ip := net.ParseIP(host); ip != nil && IsPrivateAddress(ip.String()) {
			return fmt.Errorf("url host is not public: %s", host)
		}
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return fmt.Errorf("url port is not allowed: %s", port)
	}
	return nil
}

// IsAllowedURL reports whether checkURL passes ValidateURL.
func IsAllowedURL(checkURL string, allowUnsafeConnections bool) bool {
	return ValidateURL(checkURL, allowUnsafeConnections) == nil
}

func isNumericHost(host string) bool {
	if host == "" {
		return false
	}
	for _, r := range host {
		if r != '.' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func isBlockedHostname(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".home.arpa", ".in-addr.arpa", ".ip6.arpa"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
