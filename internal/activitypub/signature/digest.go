package signature

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

const digestPrefix = "SHA-256="

func DigestHeader(body []byte) string {
	sum := sha256.Sum256(body)
	return digestPrefix + base64.StdEncoding.EncodeToString(sum[:])
}

func VerifyDigest(header string, body []byte) error {
	if header == "" {
		return fmt.Errorf("missing digest header")
	}
	parts := strings.SplitN(header, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid digest header")
	}
	if strings.ToUpper(parts[0]) != "SHA-256" {
		return fmt.Errorf("unsupported digest algorithm: %s", parts[0])
	}
	if header != DigestHeader(body) {
		return fmt.Errorf("invalid digest")
	}
	return nil
}
