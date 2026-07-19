package federation_test

import "testing"

func TestLoggableMisskeyResponse(t *testing.T) {
	body := []byte(`{"id":"user-id","token":"private-token","nested":{"accessToken":"private-access-token"}}`)
	want := `{"id":"user-id","nested":{"accessToken":"<redacted>"},"token":"<redacted>"}`
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
