package resolver

import (
	"strings"
	"testing"
)

func TestConcordeMinimumActor(t *testing.T) {
	host := "https://host1.test"
	preferredUsername := "AliceTest"
	actorID := host + "/users/alicetest"
	actor, err := ParseRemoteActor(map[string]any{
		"@context":          "https://www.w3.org/ns/activitystreams",
		"id":                actorID,
		"type":              "Person",
		"preferredUsername": preferredUsername,
		"inbox":             actorID + "/inbox",
		"outbox":            actorID + "/outbox",
	}, actorID)
	if err != nil {
		t.Fatalf("ParseRemoteActor returned error: %v", err)
	}
	if actor.URI != actorID {
		t.Fatalf("URI = %q", actor.URI)
	}
	if actor.Username != preferredUsername {
		t.Fatalf("Username = %q", actor.Username)
	}
	if actor.Inbox != actorID+"/inbox" {
		t.Fatalf("Inbox = %q", actor.Inbox)
	}
}

func TestConcordeTruncateLongActorName(t *testing.T) {
	host := "https://host1.test"
	actorID := host + "/users/alicetest"
	name := strings.Repeat("a", 129)
	actor, err := ParseRemoteActor(map[string]any{
		"@context":          "https://www.w3.org/ns/activitystreams",
		"id":                actorID,
		"type":              "Person",
		"preferredUsername": "alicetest",
		"name":              name,
		"inbox":             actorID + "/inbox",
		"outbox":            actorID + "/outbox",
	}, actorID)
	if err != nil {
		t.Fatalf("ParseRemoteActor returned error: %v", err)
	}
	if actor.Name != name[:128] {
		t.Fatalf("Name length = %d", len(actor.Name))
	}
}

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
