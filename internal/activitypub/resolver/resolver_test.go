package resolver

import "testing"

func TestParseRemoteActor(t *testing.T) {
	actor, err := ParseRemoteActor(map[string]any{
		"type":              "Person",
		"id":                "https://remote.example/users/alice",
		"inbox":             "https://remote.example/users/alice/inbox",
		"outbox":            "https://remote.example/users/alice/outbox",
		"preferredUsername": "alice",
		"name":              "Alice",
		"endpoints": map[string]any{
			"sharedInbox": "https://remote.example/inbox",
		},
		"publicKey": map[string]any{
			"id":           "https://remote.example/users/alice#main-key",
			"publicKeyPem": "-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----\n",
		},
	}, "https://remote.example/users/alice")
	if err != nil {
		t.Fatalf("ParseRemoteActor returned error: %v", err)
	}
	if actor.URI != "https://remote.example/users/alice" || actor.SharedInbox != "https://remote.example/inbox" {
		t.Fatalf("unexpected actor: %+v", actor)
	}
	if actor.Host == nil || *actor.Host != "remote.example" {
		t.Fatalf("unexpected host: %+v", actor.Host)
	}
}

func TestParseRemoteActorRejectsWrongInboxHost(t *testing.T) {
	_, err := ParseRemoteActor(map[string]any{
		"type":              "Person",
		"id":                "https://remote.example/users/alice",
		"inbox":             "https://evil.example/inbox",
		"outbox":            "https://remote.example/users/alice/outbox",
		"preferredUsername": "alice",
	}, "https://remote.example/users/alice")
	if err == nil {
		t.Fatalf("ParseRemoteActor should reject wrong inbox host")
	}
}
