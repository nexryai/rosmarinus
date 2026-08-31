package reactions

import (
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	domainreactions "github.com/nexryai/rosmarinus/internal/domain/reactions"
)

func TestRenderLike(t *testing.T) {
	rendered := RenderLike("https://example.test/", &domainreactions.Reaction{
		ID:       "reaction/id",
		NoteURI:  "https://remote.example/notes/1",
		ActorURI: "https://example.test/users/alice",
		Reaction: "👍",
	})
	if rendered["id"] != "https://example.test/likes/reaction%2Fid" ||
		rendered["type"] != "Like" ||
		rendered["actor"] != "https://example.test/users/alice" ||
		rendered["object"] != "https://remote.example/notes/1" ||
		rendered["_misskey_reaction"] != "👍" {
		t.Fatalf("unexpected Like activity: %#v", rendered)
	}
}

func TestRenderLikeWithLocalEmojiTag(t *testing.T) {
	updatedAt := time.Date(2026, 8, 31, 1, 2, 3, 0, time.UTC)
	rendered := RenderLikeWithEmoji("https://example.test/", &domainreactions.Reaction{
		ID:       "reaction-id",
		NoteURI:  "https://remote.example/notes/1",
		ActorURI: "https://example.test/users/alice",
		Reaction: ":party@.:",
	}, &emojis.Emoji{
		Name:      "party",
		PublicURL: "https://cdn.example.test/party.webp",
		MediaType: "image/webp",
		UpdatedAt: updatedAt,
	})
	tags, ok := rendered["tag"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("unexpected emoji tags: %#v", rendered["tag"])
	}
	tag, ok := tags[0].(map[string]any)
	icon, iconOK := tag["icon"].(map[string]any)
	if !ok || !iconOK || tag["id"] != "https://example.test/emojis/party" || tag["name"] != ":party:" || tag["updated"] != updatedAt.Format(time.RFC3339) || icon["url"] != "https://cdn.example.test/party.webp" || icon["mediaType"] != "image/webp" {
		t.Fatalf("unexpected emoji tag: %#v", tags[0])
	}
}

func TestLocalEmojiName(t *testing.T) {
	tests := []struct {
		reaction string
		name     string
		local    bool
	}{
		{reaction: ":party:", name: "party", local: true},
		{reaction: ":party@.:", name: "party", local: true},
		{reaction: ":party@remote.example:"},
		{reaction: "👍"},
	}
	for _, test := range tests {
		name, local := LocalEmojiName(test.reaction)
		if name != test.name || local != test.local {
			t.Fatalf("LocalEmojiName(%q) = %q, %v", test.reaction, name, local)
		}
	}
}

func TestRenderUndoLike(t *testing.T) {
	published := time.Date(2026, 7, 26, 1, 2, 3, 0, time.UTC)
	reaction := &domainreactions.Reaction{
		ID:       "reaction-id",
		NoteURI:  "https://remote.example/notes/1",
		ActorURI: "https://example.test/users/alice",
		Reaction: "👍",
	}
	rendered := RenderUndoLike("https://example.test", reaction, published)
	if rendered["id"] != "https://example.test/likes/reaction-id/undo" ||
		rendered["type"] != "Undo" ||
		rendered["actor"] != reaction.ActorURI ||
		rendered["published"] != published.Format(time.RFC3339) {
		t.Fatalf("unexpected Undo activity: %#v", rendered)
	}
	object, ok := rendered["object"].(map[string]any)
	if !ok || object["type"] != "Like" || object["id"] != "https://example.test/likes/reaction-id" {
		t.Fatalf("unexpected Undo object: %#v", rendered["object"])
	}
}
