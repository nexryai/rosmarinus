package reactions

import (
	"testing"

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
