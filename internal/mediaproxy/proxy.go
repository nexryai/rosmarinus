package mediaproxy

import (
	"fmt"
	"net/url"
	"strings"
)

type Variant string

const (
	VariantDefault Variant = ""
	VariantAvatar  Variant = "avatar"
	VariantEmoji   Variant = "emoji"
)

type Proxy struct {
	base   url.URL
	origin string
}

func New(rawURL string) (*Proxy, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("MEDIA_PROXY_URL must not be empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("MEDIA_PROXY_URL must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("MEDIA_PROXY_URL must not contain credentials, a query, or a fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &Proxy{base: *parsed, origin: parsed.Scheme + "://" + parsed.Host}, nil
}

func (p *Proxy) URL(source string, variant Variant) string {
	if p == nil || source == "" {
		return source
	}
	result := p.base
	query := url.Values{"url": []string{source}}
	if variant != VariantDefault {
		query.Set(string(variant), "1")
	}
	result.RawQuery = query.Encode()
	return result.String()
}

func (p *Proxy) Origin() string {
	if p == nil {
		return ""
	}
	return p.origin
}
