package signature

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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

func TestPostSigningStringMatchesCurrentMisskeyShape(t *testing.T) {
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

func TestGetSigningStringMatchesCurrentMisskeyShape(t *testing.T) {
	req := Request{
		URL:    "https://remote.example/users/alice",
		Method: "GET",
		Headers: map[string]string{
			"Accept": "application/activity+json, application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"",
			"Date":   "Mon, 06 Jul 2026 00:00:00 GMT",
			"Host":   "remote.example",
		},
	}
	got, err := SigningString(req, []string{"(request-target)", "date", "host"})
	if err != nil {
		t.Fatalf("SigningString returned error: %v", err)
	}
	want := "(request-target): get /users/alice\n" +
		"date: Mon, 06 Jul 2026 00:00:00 GMT\n" +
		"host: remote.example"
	if got != want {
		t.Fatalf("SigningString = %q, want %q", got, want)
	}
}

func TestParseHeader(t *testing.T) {
	raw := `keyId="https://example.test/users/alice#main-key",algorithm="rsa-sha256",headers="(request-target) date host digest",signature="YWJj"`
	sig, err := ParseHeader(raw)
	if err != nil {
		t.Fatalf("ParseHeader returned error: %v", err)
	}
	if sig.KeyID != "https://example.test/users/alice#main-key" {
		t.Fatalf("KeyID = %q", sig.KeyID)
	}
	if sig.Algorithm != "rsa-sha256" {
		t.Fatalf("Algorithm = %q", sig.Algorithm)
	}
	if len(sig.Headers) != 4 || sig.Headers[0] != "(request-target)" || sig.Headers[3] != "digest" {
		t.Fatalf("Headers = %#v", sig.Headers)
	}
	if string(sig.Signature) != "abc" {
		t.Fatalf("Signature = %q", string(sig.Signature))
	}
}

func TestParseRequestAndVerifyRSA(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://remote.example/inbox", nil)
	req.Host = "remote.example"
	req.Header.Set("Date", "Mon, 06 Jul 2026 00:00:00 GMT")
	req.Header.Set("Digest", "SHA-256=abc")

	signingString, err := SigningString(Request{
		URL:    "https://remote.example/inbox",
		Method: http.MethodPost,
		Headers: map[string]string{
			"Date":   req.Header.Get("Date"),
			"Host":   req.Host,
			"Digest": req.Header.Get("Digest"),
		},
	}, []string{"(request-target)", "date", "host", "digest"})
	if err != nil {
		t.Fatalf("SigningString returned error: %v", err)
	}
	sum := sha256.Sum256([]byte(signingString))
	rawSig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15 returned error: %v", err)
	}
	req.Header.Set("Signature", `keyId="https://example.test/users/alice#main-key",algorithm="rsa-sha256",headers="(request-target) date host digest",signature="`+base64.StdEncoding.EncodeToString(rawSig)+`"`)

	parsed, err := ParseRequest(req, []string{"(request-target)", "date", "host", "digest"})
	if err != nil {
		t.Fatalf("ParseRequest returned error: %v", err)
	}
	if parsed.SigningString != signingString {
		t.Fatalf("SigningString = %q, want %q", parsed.SigningString, signingString)
	}
	if err := VerifyRSA(parsed, publicKeyPEM(&privateKey.PublicKey)); err != nil {
		t.Fatalf("VerifyRSA returned error: %v", err)
	}
}

func TestCurrentMisskeyCreateSignedPostWithVerify(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	body := []byte(`{"a":1}`)
	req, err := CreateSignedPost(PrivateKey{
		KeyID:         "x",
		PrivateKeyPEM: privateKeyPEM(privateKey),
	}, "https://example.com:8443/inbox", body, map[string]string{"User-Agent": "UA"}, time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSignedPost returned error: %v", err)
	}
	if err := VerifyRSA(HTTPSignature{
		Algorithm:     "rsa-sha256",
		Signature:     req.Signature,
		SigningString: req.SigningString,
	}, publicKeyPEM(&privateKey.PublicKey)); err != nil {
		t.Fatalf("VerifyRSA returned error: %v", err)
	}
	if got := req.Headers["digest"]; got != DigestHeader(body) {
		t.Fatalf("Digest = %q", got)
	}
	if got := req.Headers["host"]; got != "example.com:8443" {
		t.Fatalf("Host = %q", got)
	}
}

func TestCurrentMisskeyCreateSignedGetWithVerify(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	req, err := CreateSignedGet(PrivateKey{
		KeyID:         "x",
		PrivateKeyPEM: privateKeyPEM(privateKey),
	}, "https://example.com:8443/outbox", map[string]string{"User-Agent": "UA"}, time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSignedGet returned error: %v", err)
	}
	if err := VerifyRSA(HTTPSignature{
		Algorithm:     "rsa-sha256",
		Signature:     req.Signature,
		SigningString: req.SigningString,
	}, publicKeyPEM(&privateKey.PublicKey)); err != nil {
		t.Fatalf("VerifyRSA returned error: %v", err)
	}
	if got := req.Headers["accept"]; got != ActivityAccept {
		t.Fatalf("Accept = %q", got)
	}
	if got := req.Headers["host"]; got != "example.com:8443" {
		t.Fatalf("Host = %q", got)
	}
	parsed, err := ParseHeader(req.SignatureHeader)
	if err != nil {
		t.Fatalf("ParseHeader returned error: %v", err)
	}
	wantHeaders := []string{"(request-target)", "date", "host"}
	if len(parsed.Headers) != len(wantHeaders) {
		t.Fatalf("signed headers = %#v", parsed.Headers)
	}
	for i := range wantHeaders {
		if parsed.Headers[i] != wantHeaders[i] {
			t.Fatalf("signed headers = %#v", parsed.Headers)
		}
	}
}

func publicKeyPEM(key *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func privateKeyPEM(key *rsa.PrivateKey) string {
	der := x509.MarshalPKCS1PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}
