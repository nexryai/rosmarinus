package federation_test

import "testing"

func TestLoggableMisskeyResponse(t *testing.T) {
	body := []byte(`{"i":"api-token","id":"user-id","password":"private-password","token":"private-token","nested":{"accessToken":"private-access-token"}}`)
	want := `{"i":"<redacted>","id":"user-id","nested":{"accessToken":"<redacted>"},"password":"<redacted>","token":"<redacted>"}`
	if got := loggableMisskeyResponse(body); got != want {
		t.Fatalf("loggableMisskeyResponse() = %q, want %q", got, want)
	}
}

func TestLoggableMisskeyResponsePreservesNonJSON(t *testing.T) {
	const body = "upstream unavailable"
	if got := loggableMisskeyResponse([]byte(body)); got != body {
		t.Fatalf("loggableMisskeyResponse() = %q, want %q", got, body)
	}
}
