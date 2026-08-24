package mediafetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type Result struct {
	Body        []byte
	ContentType string
}

type Fetcher struct {
	client          *http.Client
	maxBytes        int64
	allowedNetworks []netip.Prefix
}

func New(maxBytes int64, timeout time.Duration, userAgent string, client *http.Client) *Fetcher {
	return NewWithAllowedNetworks(maxBytes, timeout, userAgent, nil, client)
}

func NewWithAllowedNetworks(maxBytes int64, timeout time.Duration, userAgent string, allowedNetworks []string, client *http.Client) *Fetcher {
	fetcher := &Fetcher{maxBytes: maxBytes}
	for _, network := range allowedNetworks {
		if prefix, err := netip.ParsePrefix(network); err == nil {
			fetcher.allowedNetworks = append(fetcher.allowedNetworks, prefix)
		}
	}
	client = configureHTTPClient(timeout, userAgent, client, fetcher.allowedNetworks, fetcher.ValidateURL, "media")
	fetcher.client = client
	return fetcher
}

func NewSafeHTTPClient(timeout time.Duration, userAgent string, allowedNetworks []string) (*http.Client, func(*url.URL) error) {
	validator := &Fetcher{}
	for _, network := range allowedNetworks {
		if prefix, err := netip.ParsePrefix(network); err == nil {
			validator.allowedNetworks = append(validator.allowedNetworks, prefix)
		}
	}
	client := configureHTTPClient(timeout, userAgent, nil, validator.allowedNetworks, validator.ValidateURL, "HTTP")
	return client, validator.ValidateURL
}

func configureHTTPClient(timeout time.Duration, userAgent string, client *http.Client, allowedNetworks []netip.Prefix, validate func(*url.URL) error, operation string) *http.Client {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		// Resolve and validate the origin locally; an environment proxy could
		// otherwise resolve the target after the SSRF boundary.
		transport.Proxy = nil
		transport.DialContext = safeDialer((&net.Dialer{Timeout: 10 * time.Second}).DialContext, allowedNetworks)
		client = &http.Client{Transport: transport, Timeout: timeout}
	} else {
		clone := *client
		client = &clone
		if client.Timeout == 0 {
			client.Timeout = timeout
		}
	}
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("stopped after 5 %s redirects", operation)
		}
		if validate != nil {
			if err := validate(req.URL); err != nil {
				return err
			}
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	client.Transport = userAgentTransport{base: client.Transport, userAgent: userAgent}
	return client
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Result, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return Result{}, err
	}
	if err := f.ValidateURL(target); err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Result{}, err
	}
	res, err := f.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Result{}, fmt.Errorf("media fetch status %d", res.StatusCode)
	}
	if res.ContentLength > f.maxBytes {
		return Result{}, fmt.Errorf("media exceeds %d bytes", f.maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, f.maxBytes+1))
	if err != nil {
		return Result{}, err
	}
	if int64(len(body)) > f.maxBytes {
		return Result{}, fmt.Errorf("media exceeds %d bytes", f.maxBytes)
	}
	contentType := normalizedContentType(res.Header.Get("Content-Type"))
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = normalizedContentType(http.DetectContentType(body))
	}
	if !allowedContentType(contentType) {
		return Result{}, fmt.Errorf("media content type is not allowed: %s", contentType)
	}
	if detected := normalizedContentType(http.DetectContentType(body)); detected == "text/html" || detected == "image/svg+xml" || strings.Contains(detected, "xml") {
		return Result{}, fmt.Errorf("media payload type is not allowed: %s", detected)
	}
	return Result{Body: bytes.Clone(body), ContentType: contentType}, nil
}

func ValidateURL(target *url.URL) error {
	return validateURL(target, nil)
}

func (f *Fetcher) ValidateURL(target *url.URL) error {
	return validateURL(target, f.allowedNetworks)
}

func validateURL(target *url.URL, allowedNetworks []netip.Prefix) error {
	if target == nil || target.Scheme != "https" || target.Host == "" || target.User != nil || target.Fragment != "" {
		return fmt.Errorf("media url must be an absolute https url without credentials or fragment")
	}
	host := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("media host is not public")
	}
	if ip := net.ParseIP(host); ip != nil && !isAllowedIP(ip, allowedNetworks) {
		return fmt.Errorf("media host is not public")
	}
	return nil
}

func safeDialer(dial func(context.Context, string, string) (net.Conn, error), allowedNetworks []netip.Prefix) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve media host %s: %w", host, err)
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("resolve media host %s: no addresses", host)
		}
		for _, address := range addresses {
			if !isAllowedIP(address.IP, allowedNetworks) {
				return nil, fmt.Errorf("media host %s resolves to a non-public address", host)
			}
		}
		return dial(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
}

func isAllowedIP(ip net.IP, allowedNetworks []netip.Prefix) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	for _, prefix := range allowedNetworks {
		if prefix.Contains(address) {
			return true
		}
	}
	return isPublicIP(ip)
}

func isPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

func normalizedContentType(value string) string {
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = value[:index]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func allowedContentType(value string) bool {
	if strings.HasPrefix(value, "audio/") || strings.HasPrefix(value, "video/") {
		return true
	}
	switch value {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif", "application/pdf":
		return true
	default:
		return false
	}
}

type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
}

func (t userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if strings.TrimSpace(t.userAgent) != "" {
		clone.Header.Set("User-Agent", t.userAgent)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
