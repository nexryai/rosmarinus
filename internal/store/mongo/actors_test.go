package mongostore

import (
	"encoding/json"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

func TestActorDocumentPreservesOwnership(t *testing.T) {
	doc := fromActor(actors.Actor{
		ID:             "actor-1",
		OwnerAccountID: "account-1",
		IsSystemActor:  false,
	})
	if doc.OwnerAccountID != "account-1" {
		t.Fatalf("ownerAccountId = %q", doc.OwnerAccountID)
	}
	if doc.IsSystemActor {
		t.Fatal("owned actor must not be a system actor")
	}
	encoded, err := bson.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	raw := bson.Raw(encoded)
	if value, ok := raw.Lookup("isSystemActor").BooleanOK(); !ok || value {
		t.Fatalf("owned actor isSystemActor BSON value = %v, present=%v", value, ok)
	}
}

func TestSystemActorDocumentHasNoOwner(t *testing.T) {
	doc := fromActor(actors.Actor{ID: "relay", IsSystemActor: true})
	if !doc.IsSystemActor || doc.OwnerAccountID != "" {
		t.Fatalf("unexpected system actor document: %+v", doc)
	}
}

func TestActorDocumentPreservesSuspensionTimestamp(t *testing.T) {
	suspendedAt := time.Date(2026, 8, 29, 1, 2, 3, 4_000_000, time.UTC)
	doc := fromActor(actors.Actor{ID: "actor-1", IsSuspended: true, SuspendedAt: &suspendedAt})
	actor := actorFromDocument(doc)
	if !actor.IsSuspended || actor.SuspendedAt == nil || !actor.SuspendedAt.Equal(suspendedAt) {
		t.Fatalf("suspension state did not round trip: %+v", actor)
	}
	encoded, err := bson.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if value := bson.Raw(encoded).Lookup("suspendedAt"); value.Type != bson.TypeDateTime {
		t.Fatalf("suspendedAt BSON type = %v", value.Type)
	}
}

func TestActorPatchUpdatePreservesOmittedAndClearsNull(t *testing.T) {
	var patch actors.ActorPatch
	if err := json.Unmarshal([]byte(`{"name":"Alice","summary":null,"tags":[],"is_cat":false}`), &patch); err != nil {
		t.Fatal(err)
	}
	set, unset := actorPatchUpdate(patch)
	if set["name"] != "Alice" || set["isCat"] != false {
		t.Fatalf("set = %#v", set)
	}
	if _, ok := set["birthday"]; ok {
		t.Fatalf("omitted field was included in set: %#v", set)
	}
	if _, ok := unset["summary"]; !ok {
		t.Fatalf("null field was not included in unset: %#v", unset)
	}
	if _, ok := set["tags"]; !ok {
		t.Fatalf("empty array clear was not preserved as set: %#v", set)
	}
}

func TestActorPatchUpdateDoesNotExposeServerFields(t *testing.T) {
	patch := actors.ActorPatch{Name: actors.Present("Alice")}
	set, unset := actorPatchUpdate(patch)
	for _, field := range []string{
		"_id", "ownerAccountId", "isSystemActor", "host", "username", "usernameLower", "type",
		"uri", "inbox", "sharedInbox", "followersUri", "followingUri", "featuredUri",
		"publicKeyId", "publicKeyPem", "privateKeyPem",
	} {
		if _, ok := set[field]; ok {
			t.Fatalf("server field %q in set: %#v", field, set)
		}
		if _, ok := unset[field]; ok {
			t.Fatalf("server field %q in unset: %#v", field, unset)
		}
	}
}
