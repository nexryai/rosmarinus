package types

import "testing"

func TestAuthorityMatchesCurrentMisskeyHostSemantics(t *testing.T) {
	for name, test := range map[string]struct {
		raw  string
		want string
	}{
		"hostname":     {raw: "https://Remote.Example/users/alice", want: "remote.example"},
		"default port": {raw: "https://remote.example:443/users/alice", want: "remote.example"},
		"custom port":  {raw: "https://remote.example:8443/users/alice", want: "remote.example:8443"},
		"punycode":     {raw: "https://例え.テスト/users/alice", want: "xn--r8jz45g.xn--zckzah"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Authority(test.raw)
			if err != nil || got != test.want {
				t.Fatalf("Authority(%q) = %q, %v; want %q", test.raw, got, err, test.want)
			}
		})
	}
}

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
	if !IsMove(map[string]any{"type": "Move"}) {
		t.Fatalf("Move was not detected")
	}
	if !IsAdd(map[string]any{"type": "Add"}) {
		t.Fatalf("Add was not detected")
	}
	if !IsRemove(map[string]any{"type": "Remove"}) {
		t.Fatalf("Remove was not detected")
	}
}
