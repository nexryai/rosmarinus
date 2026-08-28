package actors

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestActorPatchDistinguishesOmittedAndNull(t *testing.T) {
	var patch ActorPatch
	if err := json.Unmarshal([]byte(`{"name":"Alice","summary":null,"tags":[],"is_locked":false}`), &patch); err != nil {
		t.Fatal(err)
	}
	if !patch.Name.IsSet() || patch.Name.Value == nil || *patch.Name.Value != "Alice" {
		t.Fatalf("name patch = %+v", patch.Name)
	}
	if !patch.Summary.IsNull() {
		t.Fatalf("summary patch = %+v", patch.Summary)
	}
	if !patch.Tags.IsSet() || patch.Tags.Value == nil || len(*patch.Tags.Value) != 0 {
		t.Fatalf("tags patch = %+v", patch.Tags)
	}
	if !patch.IsLocked.IsSet() || patch.IsLocked.Value == nil || *patch.IsLocked.Value {
		t.Fatalf("locked patch = %+v", patch.IsLocked)
	}
	if patch.Birthday.IsSet() {
		t.Fatal("omitted birthday was treated as supplied")
	}
	encoded, err := json.Marshal(ActorPatch{Name: Present("Alice"), Summary: Clear[string]()})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"name":"Alice","summary":null}` {
		t.Fatalf("patch JSON = %s", encoded)
	}
}

func TestActorPatchApplyLeavesServerFieldsUntouched(t *testing.T) {
	host := "example.test"
	original := Actor{
		ID: "actor-id", OwnerAccountID: "account-id", Username: "alice", Type: "Person",
		Host: &host, URI: "https://example.test/users/actor-id", Name: "Old",
		ProfileFields: []ProfileField{{Name: "old", Value: "value"}}, Tags: []string{"old"},
	}
	patch := ActorPatch{
		Name:          Present("New"),
		Summary:       Clear[string](),
		ProfileFields: Present([]ProfileField{{Name: "site", Value: "https://example.test"}}),
		Tags:          Present([]string{}),
	}
	updated := patch.Apply(original)
	if updated.Name != "New" || updated.Summary != "" || len(updated.ProfileFields) != 1 || len(updated.Tags) != 0 {
		t.Fatalf("updated actor = %+v", updated)
	}
	if updated.ID != original.ID || updated.OwnerAccountID != original.OwnerAccountID || updated.Username != original.Username || updated.Type != original.Type || updated.URI != original.URI || !reflect.DeepEqual(updated.Host, original.Host) {
		t.Fatalf("server-owned fields changed: before=%+v after=%+v", original, updated)
	}
	updated.ProfileFields[0].Name = "changed"
	if original.ProfileFields[0].Name != "old" {
		t.Fatal("patch application aliased profile fields")
	}
}
