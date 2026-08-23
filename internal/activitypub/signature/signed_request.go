package signature

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"time"

	gofedhttpsig "github.com/go-fed/httpsig"
)

const ActivityAccept = `application/activity+json, application/ld+json; profile="https://www.w3.org/ns/activitystreams"`

type PrivateKey struct {
	KeyID         string
	PrivateKeyPEM string
}

type SignedRequest struct {
	Method          string
	URL             string
	Headers         map[string]string
	SigningString   string
	Signature       []byte
	SignatureHeader string
}

func CreateSignedPost(key PrivateKey, targetURL string, body []byte, additionalHeaders map[string]string, now time.Time) (SignedRequest, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return SignedRequest{}, err
	}
	headers := mergeHeaders(map[string]string{
		"Date":         now.UTC().Format(http.TimeFormat),
		"Host":         u.Host,
		"Content-Type": "application/activity+json",
	}, additionalHeaders)
	include := []string{"(request-target)", "date", "host", "digest"}
	return signRequest(key, targetURL, "POST", headers, include, body)
}

func CreateSignedGet(key PrivateKey, targetURL string, additionalHeaders map[string]string, now time.Time) (SignedRequest, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return SignedRequest{}, err
	}
	headers := mergeHeaders(map[string]string{
		"Accept": ActivityAccept,
		"Date":   now.UTC().Format(http.TimeFormat),
		"Host":   u.Host,
	}, additionalHeaders)
	include := []string{"(request-target)", "date", "host"}
	return signRequest(key, targetURL, "GET", headers, include, nil)
}

func signRequest(key PrivateKey, targetURL, method string, headers map[string]string, include []string, body []byte) (SignedRequest, error) {
	privateKey, err := parseRSAPrivateKey(key.PrivateKeyPEM)
	if err != nil {
		return SignedRequest{}, err
	}
	req, err := http.NewRequest(method, targetURL, bytes.NewReader(body))
	if err != nil {
		return SignedRequest{}, err
	}
	for name, value := range headers {
		if strings.EqualFold(name, "host") {
			req.Host = value
		}
		req.Header.Set(name, value)
	}
	signer, _, err := gofedhttpsig.NewSigner([]gofedhttpsig.Algorithm{gofedhttpsig.RSA_SHA256}, gofedhttpsig.DigestSha256, include, gofedhttpsig.Signature, 0)
	if err != nil {
		return SignedRequest{}, err
	}
	if err := signer.SignRequest(privateKey, key.KeyID, req, body); err != nil {
		return SignedRequest{}, err
	}
	normalizeSignatureAlgorithm(req.Header, "rsa-sha256")
	signedHeaders := headersFromRequest(req)
	signingString, err := SigningString(Request{
		URL:     targetURL,
		Method:  method,
		Headers: signedHeaders,
	}, include)
	if err != nil {
		return SignedRequest{}, err
	}
	parsed, err := ParseHeader(req.Header.Get("Signature"))
	if err != nil {
		return SignedRequest{}, err
	}
	return SignedRequest{
		Method:          method,
		URL:             targetURL,
		Headers:         lowerHeaders(signedHeaders),
		SigningString:   signingString,
		Signature:       parsed.Signature,
		SignatureHeader: req.Header.Get("Signature"),
	}, nil
}

func mergeHeaders(base, additional map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(additional))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range additional {
		out[k] = v
	}
	return out
}

func headersFromRequest(req *http.Request) map[string]string {
	headers := make(map[string]string, len(req.Header)+1)
	for key, values := range req.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	headers["Host"] = req.Host
	return headers
}

func normalizeSignatureAlgorithm(header http.Header, algorithm string) {
	value := header.Get("Signature")
	if value == "" {
		return
	}
	value = strings.Replace(value, `algorithm="hs2019"`, `algorithm="`+algorithm+`"`, 1)
	header.Set("Signature", value)
}
