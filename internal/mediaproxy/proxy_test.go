package mediaproxy

import (
	"net/url"
	"testing"
)

func TestProxyURLUsesMisskeyCompatibleParameters(t *testing.T) {
	proxy, err := New("https://media-proxy.example/function/")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		variant  Variant
		selector string
	}{
		{variant: VariantDefault},
		{variant: VariantAvatar, selector: "avatar"},
		{variant: VariantEmoji, selector: "emoji"},
		{variant: VariantThumbnail, selector: "thumbnail"},
	} {
		projected, err := url.Parse(proxy.URL("https://remote.example/image.png?x=1", test.variant))
		if err != nil {
			t.Fatal(err)
		}
		if projected.Scheme+"://"+projected.Host+projected.Path != "https://media-proxy.example/function" {
			t.Fatalf("projected URL = %q", projected.String())
		}
		if projected.Query().Get("url") != "https://remote.example/image.png?x=1" {
			t.Fatalf("source URL = %q", projected.Query().Get("url"))
		}
		if test.selector != "" && projected.Query().Get(test.selector) != "1" {
			t.Fatalf("selector %q missing from %q", test.selector, projected.String())
		}
	}
	if proxy.Origin() != "https://media-proxy.example" {
		t.Fatalf("origin = %q", proxy.Origin())
	}
}

func TestProxyRejectsUnsafeConfiguration(t *testing.T) {
	for _, raw := range []string{"", "http://proxy.example", "https://user@proxy.example", "https://proxy.example?x=1", "https://proxy.example/#fragment"} {
		if _, err := New(raw); err == nil {
			t.Fatalf("New(%q) succeeded", raw)
		}
	}
}
