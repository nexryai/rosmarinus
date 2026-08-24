package notes

import (
	"testing"
	"time"

	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/polls"
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
		VisibleUserURIs: []string{"https://remote.example/users/bob"},
		CreatedAt:       time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
	}
	rendered := Render(note)
	to, ok := rendered["to"].([]string)
	if !ok || len(to) != 1 || to[0] != note.VisibleUserURIs[0] {
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

func TestRenderDeleteUsesMisskeyCompatibleTombstone(t *testing.T) {
	deletedAt := time.Date(2026, 8, 24, 1, 2, 3, 0, time.UTC)
	note := &domainnotes.Note{
		URI: "https://rosmarinus.example/notes/1", AttributedTo: "https://rosmarinus.example/users/alice",
	}
	rendered := RenderDelete(note, deletedAt)
	if rendered["type"] != "Delete" || rendered["actor"] != note.AttributedTo || rendered["published"] != deletedAt.Format(time.RFC3339) {
		t.Fatalf("unexpected Delete activity: %#v", rendered)
	}
	tombstone, ok := rendered["object"].(map[string]any)
	if !ok || tombstone["type"] != "Tombstone" || tombstone["id"] != note.URI {
		t.Fatalf("unexpected Tombstone: %#v", rendered["object"])
	}
}

func TestRenderQuestionIncludesOrderedVoteCollections(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Hour)
	note := &domainnotes.Note{
		URI: "https://rosmarinus.example/notes/poll", AttributedTo: "https://rosmarinus.example/users/alice",
		Text: "choose", Visibility: domainnotes.VisibilityPublic, CreatedAt: time.Now().UTC(),
	}
	poll := &polls.Poll{Choices: []string{"cats", "dogs"}, Votes: []int{2, 3}, ExpiresAt: &expiresAt}
	rendered := RenderWithPoll(note, poll)
	choices, ok := rendered["oneOf"].([]any)
	if rendered["type"] != "Question" || !ok || len(choices) != 2 || rendered["endTime"] == nil {
		t.Fatalf("unexpected Question: %#v", rendered)
	}
	second := choices[1].(map[string]any)
	replies := second["replies"].(map[string]any)
	if second["name"] != "dogs" || replies["totalItems"] != 3 {
		t.Fatalf("unexpected second choice: %#v", second)
	}
	update := RenderQuestionUpdate(note, poll, time.Now().UTC())
	object, ok := update["object"].(map[string]any)
	if update["type"] != "Update" || !ok || object["type"] != "Question" || object["id"] != note.URI {
		t.Fatalf("unexpected Question Update: %#v", update)
	}
}
