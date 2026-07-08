package signature

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"

	gofedhttpsig "github.com/go-fed/httpsig"
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
	if _, err := gofedhttpsig.NewVerifier(r); err != nil {
		return HTTPSignature{}, fmt.Errorf("parse http signature with go-fed/httpsig: %w", err)
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
	req, err := requestFromSigningString(sig)
	if err != nil {
		return err
	}
	verifier, err := gofedhttpsig.NewVerifier(req)
	if err != nil {
		return fmt.Errorf("create go-fed/httpsig verifier: %w", err)
	}
	if err := verifier.Verify(pub, httpsigAlgorithm(sig.Algorithm)); err != nil {
		return fmt.Errorf("verify rsa signature: %w", err)
	}
	return nil
}

func SignRSA(signingString string, privateKeyPEM string) ([]byte, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	req, err := requestFromSigningString(HTTPSignature{
		KeyID:         "rosmarinus-signing-key",
		Algorithm:     "rsa-sha256",
		SigningString: signingString,
		Signature:     []byte("placeholder"),
	})
	if err != nil {
		return nil, err
	}
	req.Header.Del("Signature")
	headers, err := headersFromSigningString(signingString)
	if err != nil {
		return nil, err
	}
	signer, _, err := gofedhttpsig.NewSigner([]gofedhttpsig.Algorithm{gofedhttpsig.RSA_SHA256}, gofedhttpsig.DigestSha256, headers, gofedhttpsig.Signature, 0)
	if err != nil {
		return nil, fmt.Errorf("create go-fed/httpsig signer: %w", err)
	}
	if err := signer.SignRequest(key, "rosmarinus-signing-key", req, nil); err != nil {
		return nil, fmt.Errorf("sign rsa signature: %w", err)
	}
	parsed, err := ParseHeader(req.Header.Get("Signature"))
	if err != nil {
		return nil, err
	}
	return parsed.Signature, nil
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

func requestFromSigningString(sig HTTPSignature) (*http.Request, error) {
	method := http.MethodGet
	target := "/"
	headers := http.Header{}
	for _, line := range strings.Split(sig.SigningString, "\n") {
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, fmt.Errorf("invalid signing string line: %s", line)
		}
		if name == "(request-target)" {
			parts := strings.SplitN(value, " ", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid request-target: %s", value)
			}
			method = strings.ToUpper(parts[0])
			target = parts[1]
			continue
		}
		headers.Add(name, value)
	}
	req := httptest.NewRequest(method, "https://example.test"+target, nil)
	req.Header = headers
	if host := headers.Get("Host"); host != "" {
		req.Host = host
	} else {
		req.Header.Set("Host", req.Host)
	}
	keyID := sig.KeyID
	if keyID == "" {
		keyID = "rosmarinus-signing-key"
	}
	headersToSign := sig.Headers
	if len(headersToSign) == 0 {
		var err error
		headersToSign, err = headersFromSigningString(sig.SigningString)
		if err != nil {
			return nil, err
		}
	}
	req.Header.Set("Signature", SignatureHeader(keyID, headersToSign, sig.Signature))
	return req, nil
}

func headersFromSigningString(signingString string) ([]string, error) {
	lines := strings.Split(signingString, "\n")
	headers := make([]string, 0, len(lines))
	for _, line := range lines {
		name, _, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, fmt.Errorf("invalid signing string line: %s", line)
		}
		headers = append(headers, strings.ToLower(name))
	}
	return headers, nil
}

func httpsigAlgorithm(algorithm string) gofedhttpsig.Algorithm {
	switch strings.ToLower(algorithm) {
	case "rsa-sha384":
		return gofedhttpsig.RSA_SHA384
	case "rsa-sha512":
		return gofedhttpsig.RSA_SHA512
	default:
		return gofedhttpsig.RSA_SHA256
	}
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
