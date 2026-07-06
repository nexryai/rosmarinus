package signature

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var supportedAlgorithm = regexp.MustCompile(`(?i)^((dsa|rsa|ecdsa)-(sha256|sha384|sha512)|ed25519-sha512|hs2019)$`)

type HTTPSignature struct {
	KeyID         string
	Algorithm     string
	Headers       []string
	Signature     []byte
	SigningString string
}

func ParseRequest(r *http.Request, requiredHeaders []string) (HTTPSignature, error) {
	header := r.Header.Get("Signature")
	if header == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(auth), "signature ") {
			header = strings.TrimSpace(auth[len("Signature "):])
		}
	}
	if header == "" {
		return HTTPSignature{}, fmt.Errorf("missing signature header")
	}
	sig, err := ParseHeader(header)
	if err != nil {
		return HTTPSignature{}, err
	}
	if !IsSupportedAlgorithm(sig.Algorithm) {
		return HTTPSignature{}, fmt.Errorf("unsupported signature algorithm: %s", sig.Algorithm)
	}
	if err := requireSignedHeaders(sig.Headers, requiredHeaders); err != nil {
		return HTTPSignature{}, err
	}
	req := Request{
		URL:     "https://" + r.Host + r.URL.RequestURI(),
		Method:  r.Method,
		Headers: headerMap(r),
	}
	signingString, err := SigningString(req, sig.Headers)
	if err != nil {
		return HTTPSignature{}, err
	}
	sig.SigningString = signingString
	return sig, nil
}

func ParseHeader(header string) (HTTPSignature, error) {
	params, err := parseSignatureParams(header)
	if err != nil {
		return HTTPSignature{}, err
	}
	keyID := params["keyId"]
	if keyID == "" {
		keyID = params["keyid"]
	}
	if keyID == "" {
		return HTTPSignature{}, fmt.Errorf("signature keyId is required")
	}
	algorithm := params["algorithm"]
	if algorithm == "" {
		return HTTPSignature{}, fmt.Errorf("signature algorithm is required")
	}
	headers := strings.Fields(params["headers"])
	if len(headers) == 0 {
		return HTTPSignature{}, fmt.Errorf("signature headers are required")
	}
	rawSignature := params["signature"]
	if rawSignature == "" {
		return HTTPSignature{}, fmt.Errorf("signature value is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(rawSignature)
	if err != nil {
		return HTTPSignature{}, fmt.Errorf("decode signature: %w", err)
	}
	for i, header := range headers {
		headers[i] = strings.ToLower(header)
	}
	return HTTPSignature{
		KeyID:     keyID,
		Algorithm: strings.ToLower(algorithm),
		Headers:   headers,
		Signature: decoded,
	}, nil
}

func IsSupportedAlgorithm(algorithm string) bool {
	return supportedAlgorithm.MatchString(algorithm)
}

func VerifyRSA(sig HTTPSignature, publicKeyPEM string) error {
	pub, err := parseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(sig.SigningString))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig.Signature); err != nil {
		return fmt.Errorf("verify rsa signature: %w", err)
	}
	return nil
}

func SignRSA(signingString string, privateKeyPEM string) ([]byte, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(signingString))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return nil, fmt.Errorf("sign rsa signature: %w", err)
	}
	return sig, nil
}

func SignatureHeader(keyID string, headers []string, signature []byte) string {
	return fmt.Sprintf(`keyId="%s",algorithm="rsa-sha256",headers="%s",signature="%s"`, keyID, strings.Join(headers, " "), base64.StdEncoding.EncodeToString(signature))
}

func parseSignatureParams(header string) (map[string]string, error) {
	params := map[string]string{}
	for _, part := range splitComma(header) {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return nil, fmt.Errorf("invalid signature parameter: %s", part)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		if key != "" {
			params[key] = value
		}
	}
	return params, nil
}

func splitComma(value string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	for _, r := range value {
		switch r {
		case '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case ',':
			if inQuote {
				current.WriteRune(r)
			} else {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	parts = append(parts, current.String())
	return parts
}

func requireSignedHeaders(actual []string, required []string) error {
	seen := map[string]struct{}{}
	for _, header := range actual {
		seen[strings.ToLower(header)] = struct{}{}
	}
	for _, header := range required {
		if _, ok := seen[strings.ToLower(header)]; !ok {
			return fmt.Errorf("signature missing required header: %s", header)
		}
	}
	return nil
}

func headerMap(r *http.Request) map[string]string {
	headers := make(map[string]string, len(r.Header)+1)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	headers["Host"] = r.Host
	return headers
}

func parseRSAPublicKey(publicKeyPEM string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("decode public key pem")
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("public key is not rsa")
	}
	rsaKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse rsa public key: %w", err)
	}
	return rsaKey, nil
}

func parseRSAPrivateKey(privateKeyPEM string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("decode private key pem")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse rsa private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not rsa")
	}
	return rsaKey, nil
}
