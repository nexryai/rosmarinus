package actors

import (
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/config"
	domainactors "github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
)

func TestRenderLocalActorIncludesProfileAndServerEndpoints(t *testing.T) {
	cfg := config.Config{PublicURL: "https://example.test"}
	actor := &domainactors.Actor{
		ID: "actor-id", Username: "alice", Name: "Alice", Summary: "hello **world**",
		URL: "https://example.test/@alice", URI: "https://example.test/users/actor-id",
		Inbox: "https://example.test/users/actor-id/inbox", SharedInbox: "https://example.test/inbox",
		FollowersURI: "https://example.test/users/actor-id/followers",
		FollowingURI: "https://example.test/users/actor-id/following",
		FeaturedURI:  "https://example.test/users/actor-id/collections/featured",
		PublicKeyID:  "https://example.test/users/actor-id#main-key", PublicKeyPEM: "pem",
		ProfileFields: []domainactors.ProfileField{{Name: "site", Value: "https://example.test"}},
		Birthday:      "2000-01-02", Location: "Earth", AvatarURL: "https://example.test/avatar.png",
		BannerURL: "https://example.test/banner.png", Tags: []string{"go"}, EmojiNames: []string{"blobcat"}, IsCat: true, IsDiscoverable: true,
	}
	body := RenderLocalActor(cfg, actor)
	if body["type"] != "Service" || body["id"] != actor.URI || body["inbox"] != actor.Inbox {
		t.Fatalf("identity/endpoints = %#v", body)
	}
	if body["summary"] != "hello <b>world</b>" || body["_misskey_summary"] != actor.Summary {
		t.Fatalf("summary = %#v", body["summary"])
	}
	if body["manuallyApprovesFollowers"] != true || body["discoverable"] != true || body["isCat"] != true {
		t.Fatalf("policy flags = %#v", body)
	}
	actor.IsBot = true
	if got := RenderLocalActor(cfg, actor)["type"]; got != "Service" {
		t.Fatalf("bot actor type = %v, want Service", got)
	}
	actor.IsBot = false
	actor.IsDiscoverable = false
	if got := RenderLocalActor(cfg, actor)["discoverable"]; got != false {
		t.Fatalf("discoverable=false was not reflected: %v", got)
	}
	actor.IsSystemActor = true
	if got := RenderLocalActor(cfg, actor)["discoverable"]; got != true {
		t.Fatalf("legacy system Actor discoverable = %v, want true", got)
	}
	actor.IsSystemActor = false
	attachment, ok := body["attachment"].([]any)
	if !ok || len(attachment) != 1 || attachment[0].(map[string]any)["type"] != "PropertyValue" {
		t.Fatalf("attachment = %#v", body["attachment"])
	}
	tags, ok := body["tag"].([]any)
	if !ok || len(tags) != 2 {
		t.Fatalf("tags = %#v", body["tag"])
	}
	resolved := RenderLocalActorWithEmojis(cfg, actor, []emojis.Emoji{{
		Name: "blobcat", URI: "https://example.test/emojis/blobcat",
		PublicURL: "https://example.test/media/blobcat.webp", MediaType: "image/webp",
		UpdatedAt: time.Date(2026, 8, 27, 1, 2, 3, 0, time.UTC),
	}})
	resolvedTags := resolved["tag"].([]any)
	emojiTag := resolvedTags[0].(map[string]any)
	icon := emojiTag["icon"].(map[string]any)
	if emojiTag["updated"] != "2026-08-27T01:02:03Z" || icon["url"] != "https://example.test/media/blobcat.webp" || icon["mediaType"] != "image/webp" {
		t.Fatalf("resolved emoji tag = %#v", emojiTag)
	}
	if body["vcard:bday"] != actor.Birthday || body["vcard:Address"] != actor.Location {
		t.Fatalf("vcard fields = %#v", body)
	}
}

func TestRenderUpdateContainsFullPersonObject(t *testing.T) {
	cfg := config.Config{PublicURL: "https://example.test"}
	actor := &domainactors.Actor{ID: "actor-id", Username: "alice", URI: "https://example.test/users/actor-id", Type: "Person"}
	published := time.Date(2026, 8, 28, 1, 2, 3, 0, time.UTC)
	body := RenderUpdateAt(cfg, actor, "https://example.test/updates/1", published)
	if body["type"] != "Update" || body["id"] != "https://example.test/updates/1" || body["actor"] != actor.URI {
		t.Fatalf("update = %#v", body)
	}
	if body["published"] != published.Format(time.RFC3339Nano) {
		t.Fatalf("published = %#v", body["published"])
	}
	person, ok := body["object"].(map[string]any)
	if !ok || person["type"] != "Person" || person["id"] != actor.URI || person["publicKey"] == nil || person["inbox"] == nil || person["followers"] == nil {
		t.Fatalf("person object = %#v", body["object"])
	}
}
