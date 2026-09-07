package mongostore

import "testing"

func TestRequireDocumentIDAcceptsOnlyCanonicalObjectIDStrings(t *testing.T) {
	if err := requireDocumentID("record id", "507f1f77bcf86cd799439011"); err != nil {
		t.Fatalf("canonical ObjectID string was rejected: %v", err)
	}
	for _, id := range []string{
		"record-1",
		"507F1F77BCF86CD799439011",
		" 507f1f77bcf86cd799439011 ",
	} {
		if err := requireDocumentID("record id", id); err == nil {
			t.Fatalf("non-canonical id %q was accepted", id)
		}
	}
}
