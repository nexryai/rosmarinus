package main

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestNewMappingsKeepsObjectIDStringsAndMapsLegacyIDs(t *testing.T) {
	valid := "507f1f77bcf86cd799439011"
	mappings := newMappings(map[string]struct{}{
		valid:          {},
		"remote_alice": {},
		"local-alice":  {},
	})
	if len(mappings) != 2 {
		t.Fatalf("mapping count = %d, want 2", len(mappings))
	}
	seen := map[string]bool{}
	for _, mapping := range mappings {
		if mapping.Old == valid {
			t.Fatal("valid ObjectID string was remapped")
		}
		if _, err := bson.ObjectIDFromHex(mapping.New); err != nil {
			t.Fatalf("new ID is not ObjectID hex: %q: %v", mapping.New, err)
		}
		if mapping.New == valid || seen[mapping.New] {
			t.Fatalf("generated colliding ID: %q", mapping.New)
		}
		seen[mapping.New] = true
	}
}

func TestUniqueStringsPreservesFirstOccurrence(t *testing.T) {
	got := uniqueStrings(bson.A{"old", "older", "old", 42, ""})
	if len(got) != 2 || got[0] != "old" || got[1] != "older" {
		t.Fatalf("uniqueStrings = %#v", got)
	}
}
