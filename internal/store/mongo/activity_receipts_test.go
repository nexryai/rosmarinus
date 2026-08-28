package mongostore

import (
	"encoding/hex"
	"testing"
)

func TestNewActivityLeaseToken(t *testing.T) {
	first, err := newActivityLeaseToken()
	if err != nil {
		t.Fatalf("newActivityLeaseToken returned error: %v", err)
	}
	second, err := newActivityLeaseToken()
	if err != nil {
		t.Fatalf("newActivityLeaseToken returned error: %v", err)
	}
	if first == second {
		t.Fatalf("lease tokens must be unique")
	}
	decoded, err := hex.DecodeString(first)
	if err != nil {
		t.Fatalf("lease token is not hexadecimal: %v", err)
	}
	if len(decoded) != 16 {
		t.Fatalf("lease token has %d bytes, want 16", len(decoded))
	}
}
