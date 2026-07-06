package signature

import "testing"

func TestDigestHeaderAndVerify(t *testing.T) {
	body := []byte(`{"type":"Create"}`)
	header := DigestHeader(body)
	if header != "SHA-256=JeE18werLvQnEoHViKDam+ZK1D8E27TBC2kIISI7pIY=" {
		t.Fatalf("DigestHeader = %q", header)
	}
	if err := VerifyDigest(header, body); err != nil {
		t.Fatalf("VerifyDigest returned error: %v", err)
	}
	if err := VerifyDigest(header, []byte(`{"type":"Delete"}`)); err == nil {
		t.Fatalf("VerifyDigest should reject changed body")
	}
}

func TestPostSigningStringMatchesConcordeShape(t *testing.T) {
	req := Request{
		URL:    "https://remote.example/inbox?ignored=true",
		Method: "POST",
		Headers: map[string]string{
			"Date":   "Mon, 06 Jul 2026 00:00:00 GMT",
			"Host":   "remote.example",
			"Digest": "SHA-256=abc",
		},
	}
	got, err := SigningString(req, []string{"(request-target)", "date", "host", "digest"})
	if err != nil {
		t.Fatalf("SigningString returned error: %v", err)
	}
	want := "(request-target): post /inbox\n" +
		"date: Mon, 06 Jul 2026 00:00:00 GMT\n" +
		"host: remote.example\n" +
		"digest: SHA-256=abc"
	if got != want {
		t.Fatalf("SigningString = %q, want %q", got, want)
	}
}

func TestGetSigningStringMatchesConcordeShape(t *testing.T) {
	req := Request{
		URL:    "https://remote.example/users/alice",
		Method: "GET",
		Headers: map[string]string{
			"Accept": "application/activity+json, application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"",
			"Date":   "Mon, 06 Jul 2026 00:00:00 GMT",
			"Host":   "remote.example",
		},
	}
	got, err := SigningString(req, []string{"(request-target)", "date", "host", "accept"})
	if err != nil {
		t.Fatalf("SigningString returned error: %v", err)
	}
	want := "(request-target): get /users/alice\n" +
		"date: Mon, 06 Jul 2026 00:00:00 GMT\n" +
		"host: remote.example\n" +
		"accept: application/activity+json, application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\""
	if got != want {
		t.Fatalf("SigningString = %q, want %q", got, want)
	}
}
