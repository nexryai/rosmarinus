package mongostore

import (
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/emojis"
)

func TestEmojiRecordPreservesRemoteSource(t *testing.T) {
	remoteUpdatedAt := time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)
	doc := fromEmoji(emojis.Emoji{
		ID: "emoji-1", Host: "remote.example", Name: "party",
		URI:         "https://remote.example/emojis/party",
		OriginalURL: "https://remote.example/files/party.webp",
		MediaType:   "image/webp", RemoteUpdatedAt: &remoteUpdatedAt,
	})
	if doc.Host != "remote.example" || doc.Name != "party" || doc.PublicURL != doc.OriginalURL {
		t.Fatalf("unexpected emoji record: %+v", doc)
	}
	if doc.RemoteUpdatedAt == nil || !doc.RemoteUpdatedAt.Equal(remoteUpdatedAt) {
		t.Fatalf("remote updated time = %v", doc.RemoteUpdatedAt)
	}
	stored := toEmoji(doc)
	if stored.PublicURL != doc.PublicURL {
		t.Fatalf("public URL was not restored: %+v", stored)
	}
}

func TestEmojiIDIsStableAndHostScoped(t *testing.T) {
	first := emojiID("remote.example", "party")
	if first != emojiID("remote.example", "party") {
		t.Fatal("emoji ID is not stable")
	}
	if first == emojiID("other.example", "party") {
		t.Fatal("emoji ID must be scoped to the remote host")
	}
}

func TestLocalEmojiRecordPreservesPublicURL(t *testing.T) {
	doc := fromEmoji(emojis.Emoji{
		Name: "party", OriginalURL: "https://origin.example/party.webp",
		PublicURL: "https://local.example/media/party", MediaType: "image/webp",
	})
	if doc.PublicURL != "https://local.example/media/party" || doc.OriginalURL != "https://origin.example/party.webp" {
		t.Fatalf("unexpected local emoji record: %+v", doc)
	}
}
