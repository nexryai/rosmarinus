package notes

import (
	"testing"
	"time"

	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
)

func TestRenderAnnounceNote(t *testing.T) {
	note := &domainnotes.Note{
		URI:          "https://remote.example/activities/announce",
		AttributedTo: "https://remote.example/users/alice",
		AuthorID:     "remote_alice",
		RenoteID:     "note-id",
		RenoteURI:    "https://rosmarinus.example/notes/1",
		Visibility:   domainnotes.VisibilityPublic,
		CreatedAt:    time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
	}
	rendered := Render(note)
	if rendered["type"] != "Announce" {
		t.Fatalf("type = %v", rendered["type"])
	}
	if rendered["id"] != note.URI || rendered["actor"] != note.AttributedTo || rendered["object"] != note.RenoteURI {
		t.Fatalf("unexpected announce: %#v", rendered)
	}
	to, ok := rendered["to"].([]string)
	if !ok || len(to) != 1 || to[0] != PublicAudience {
		t.Fatalf("to = %#v", rendered["to"])
	}
}
