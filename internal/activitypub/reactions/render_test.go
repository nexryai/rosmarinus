package reactions

import (
	"testing"
	"time"

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
