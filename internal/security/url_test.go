package security

import "testing"

func TestIsAllowedURLRequiresHTTPSByDefault(t *testing.T) {
	allowed := []string{
		"https://example.com",
		"https://remote.example/@alice",
		"https://example.com:443/path",
		"https://8.8.8.8/resolve",
	}
	for _, value := range allowed {
		if !IsAllowedURL(value, false) {
			t.Errorf("IsAllowedURL(%q, false) = false, want true", value)
		}
	}
	rejected := []string{
		"",
		"http://example.com",
		"ftp://example.com",
		"javascript:alert(1)",
		"https://user:password@example.com",
		"https://localhost",
		"https://localhost/path",
		"https://service.local/path",
		"https://127.0.0.1",
		"https://10.0.0.1/actor",
		"https://[::1]/",
		"https://example.com:8080/",
		"https://example/",
		"https://127.1/",
		"https://2130706433/",
		"https:// example.com",
		"https://example.com/\nnext",
	}
	for _, value := range rejected {
		if IsAllowedURL(value, false) {
			t.Errorf("IsAllowedURL(%q, false) = true, want false", value)
		}
	}
}

func TestIsAllowedURLPermitsHTTPForNoteBodies(t *testing.T) {
	for _, value := range []string{
		"http://example.com/notes/1",
		"https://example.com/notes/1",
	} {
		if !IsAllowedURL(value, true) {
			t.Errorf("IsAllowedURL(%q, true) = false, want true", value)
		}
	}
	if IsAllowedURL("http://127.0.0.1/note", true) {
		t.Error("note body link to a private address must be rejected")
	}
	if IsAllowedURL("http://localhost/note", true) {
		t.Error("note body link to localhost must be rejected")
	}
}
