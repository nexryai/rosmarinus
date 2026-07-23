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

func TestRenderSpecifiedNoteAddressesMentionedActors(t *testing.T) {
	note := &domainnotes.Note{
		URI:          "https://rosmarinus.example/notes/direct",
		AttributedTo: "https://rosmarinus.example/users/alice",
		Text:         "hello",
		Visibility:   domainnotes.VisibilitySpecified,
		MentionURIs: []string{
			"https://remote.example/users/bob",
			"https://other.example/users/carol",
		},
		CreatedAt: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
	}
	rendered := Render(note)
	to, ok := rendered["to"].([]string)
	if !ok || len(to) != 2 || to[0] != note.MentionURIs[0] || to[1] != note.MentionURIs[1] {
		t.Fatalf("to = %#v", rendered["to"])
	}
	cc, ok := rendered["cc"].([]string)
	if !ok || len(cc) != 0 {
		t.Fatalf("cc = %#v", rendered["cc"])
	}
}

func TestRenderCreateWrapsNoteAudience(t *testing.T) {
	published := time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC)
	note := &domainnotes.Note{
		URI:          "https://rosmarinus.example/notes/1",
		AttributedTo: "https://rosmarinus.example/users/alice",
		Text:         "hello",
		Visibility:   domainnotes.VisibilityPublic,
		CreatedAt:    published,
		PublishedAt:  &published,
	}
	rendered := RenderCreate(note)
	if rendered["id"] != note.URI+"/activity" || rendered["type"] != "Create" || rendered["actor"] != note.AttributedTo {
		t.Fatalf("unexpected Create activity: %#v", rendered)
	}
	if rendered["published"] != published.Format(time.RFC3339) {
		t.Fatalf("published = %#v", rendered["published"])
	}
	object, ok := rendered["object"].(map[string]any)
	if !ok || object["id"] != note.URI || object["@context"] != nil {
		t.Fatalf("object = %#v", rendered["object"])
	}
	if rendered["@context"] == nil || rendered["to"] == nil || rendered["cc"] == nil {
		t.Fatalf("missing Create activity context or audience: %#v", rendered)
	}
}
