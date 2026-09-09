package mongostore

import (
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/reactions"
)

func TestReactionDocumentPreservesCustomEmojiPresentation(t *testing.T) {
	original := reactions.Reaction{
		ID: "reaction-1", NoteID: "note-1", ActorID: "actor-1", Reaction: ":party@remote.test:",
		EmojiName: "party", EmojiURL: "https://remote.test/emoji.webp", EmojiMediaType: "image/webp",
		CreatedAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
	}

	decoded := toReaction(fromReaction(original))
	if decoded.EmojiName != original.EmojiName || decoded.EmojiURL != original.EmojiURL || decoded.EmojiMediaType != original.EmojiMediaType {
		t.Fatalf("custom emoji metadata was not preserved: %+v", decoded)
	}
}
