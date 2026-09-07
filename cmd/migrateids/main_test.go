package main

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestNewMappingsConvertsLegacyAndBSONObjectIDsToUniqueStrings(t *testing.T) {
	objectID := bson.NewObjectID()
	valid := objectID.Hex()
	mappings, err := newMappings([]any{"legacy", objectID, valid, objectID})
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 2 {
		t.Fatalf("mapping count = %d", len(mappings))
	}
	seen := map[string]struct{}{valid: {}}
	for _, mapping := range mappings {
		if !isObjectIDString(mapping.New) {
			t.Fatalf("new id is not an ObjectID string: %q", mapping.New)
		}
		if _, exists := seen[mapping.New]; exists {
			t.Fatalf("duplicate migrated id: %q", mapping.New)
		}
		seen[mapping.New] = struct{}{}
	}
}

func TestVoteScopeMatchesSingleAndMultipleVoteRules(t *testing.T) {
	if voteScope("note", "actor", 0, false) != voteScope("note", "actor", 1, false) {
		t.Fatal("single-choice vote scopes differ")
	}
	if voteScope("note", "actor", 0, true) == voteScope("note", "actor", 1, true) {
		t.Fatal("multiple-choice vote scopes collide")
	}
}
