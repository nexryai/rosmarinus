package mongostore

import (
	"testing"

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
}

func TestSystemActorDocumentHasNoOwner(t *testing.T) {
	doc := fromActor(actors.Actor{ID: "relay", IsSystemActor: true})
	if !doc.IsSystemActor || doc.OwnerAccountID != "" {
		t.Fatalf("unexpected system actor document: %+v", doc)
	}
}
