package signature

import (
	"net/http"
	"net/url"
	"time"
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
		"Host":         u.Hostname(),
		"Content-Type": "application/activity+json",
		"Digest":       DigestHeader(body),
	}, additionalHeaders)
	include := []string{"(request-target)", "date", "host", "digest"}
	return signRequest(key, targetURL, "POST", headers, include)
}

func CreateSignedGet(key PrivateKey, targetURL string, additionalHeaders map[string]string, now time.Time) (SignedRequest, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return SignedRequest{}, err
	}
	headers := mergeHeaders(map[string]string{
		"Accept": ActivityAccept,
		"Date":   now.UTC().Format(http.TimeFormat),
		"Host":   u.Hostname(),
	}, additionalHeaders)
	include := []string{"(request-target)", "date", "host", "accept"}
	return signRequest(key, targetURL, "GET", headers, include)
}

func signRequest(key PrivateKey, targetURL, method string, headers map[string]string, include []string) (SignedRequest, error) {
	signingString, err := SigningString(Request{
		URL:     targetURL,
		Method:  method,
		Headers: headers,
	}, include)
	if err != nil {
		return SignedRequest{}, err
	}
	rawSignature, err := SignRSA(signingString, key.PrivateKeyPEM)
	if err != nil {
		return SignedRequest{}, err
	}
	signatureHeader := SignatureHeader(key.KeyID, include, rawSignature)
	headers["Signature"] = signatureHeader
	return SignedRequest{
		Method:          method,
		URL:             targetURL,
		Headers:         lowerHeaders(headers),
		SigningString:   signingString,
		Signature:       rawSignature,
		SignatureHeader: signatureHeader,
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
