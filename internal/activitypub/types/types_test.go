package types

import "testing"

func TestGetAPID(t *testing.T) {
	id, err := GetAPID("https://example.test/notes/1")
	if err != nil || id != "https://example.test/notes/1" {
		t.Fatalf("GetAPID string = %q, %v", id, err)
	}
	id, err = GetAPID(map[string]any{"id": "https://example.test/users/a"})
	if err != nil || id != "https://example.test/users/a" {
		t.Fatalf("GetAPID object = %q, %v", id, err)
	}
}

func TestGetOneAPID(t *testing.T) {
	id, err := GetOneAPID([]any{"https://example.test/a", "https://example.test/b"})
	if err != nil || id != "https://example.test/a" {
		t.Fatalf("GetOneAPID = %q, %v", id, err)
	}
}

func TestGetAPType(t *testing.T) {
	typ, err := GetAPType(map[string]any{"type": "Create"})
	if err != nil || typ != "Create" {
		t.Fatalf("GetAPType string = %q, %v", typ, err)
	}
	typ, err = GetAPType(map[string]any{"type": []any{"Note", "Object"}})
	if err != nil || typ != "Note" {
		t.Fatalf("GetAPType array = %q, %v", typ, err)
	}
}

func TestPredicates(t *testing.T) {
	if !IsCreate(map[string]any{"type": "Create"}) {
		t.Fatalf("Create was not detected")
	}
	if !IsAnnounce(map[string]any{"type": "Announce"}) {
		t.Fatalf("Announce was not detected")
	}
	if !IsLike(map[string]any{"type": "EmojiReaction"}) {
		t.Fatalf("EmojiReaction was not detected as Like")
	}
	if !IsPost(map[string]any{"type": "Question"}) {
		t.Fatalf("Question was not detected as post")
	}
	if !IsActor(map[string]any{"type": "Service"}) {
		t.Fatalf("Service was not detected as actor")
	}
}
