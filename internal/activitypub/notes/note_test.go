package notes

import "testing"

func TestConcordeMinimumNote(t *testing.T) {
	host := "https://host1.test"
	actorID := host + "/users/alice"
	post := map[string]any{
		"@context":     "https://www.w3.org/ns/activitystreams",
		"id":           host + "/notes/12345678",
		"type":         "Note",
		"attributedTo": actorID,
		"to":           PublicAudience,
		"content":      "test",
	}
	note, err := ParseRemoteNote(post, post["id"].(string))
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if note.URI != post["id"] {
		t.Fatalf("URI = %q", note.URI)
	}
	if note.Visibility != VisibilityPublic {
		t.Fatalf("Visibility = %q", note.Visibility)
	}
	if note.Text != post["content"] {
		t.Fatalf("Text = %q", note.Text)
	}
}

func TestConcordeNoteTextPrefersMisskeyMarkdownSource(t *testing.T) {
	note, err := ParseRemoteNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host1.test/users/alice",
		"to":           PublicAudience,
		"content":      "<p>html</p>",
		"source": map[string]any{
			"mediaType": "text/x.misskeymarkdown",
			"content":   "$[x2 mfm]",
		},
	}, "https://host1.test/notes/1")
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if note.Text != "$[x2 mfm]" {
		t.Fatalf("Text = %q", note.Text)
	}
}

func TestConcordeNoteExtractsTags(t *testing.T) {
	note, err := ParseRemoteNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host1.test/users/alice",
		"to":           PublicAudience,
		"content":      "hello @bob #tag :blob:",
		"tag": []any{
			map[string]any{
				"type": "Mention",
				"href": "https://host2.test/users/bob",
			},
			map[string]any{
				"type": "Mention",
				"href": "https://host2.test/users/bob",
			},
			map[string]any{
				"type": "Hashtag",
				"name": "#tag",
			},
			map[string]any{
				"id":      "https://host1.test/emojis/blob",
				"type":    "Emoji",
				"name":    ":blob:",
				"updated": "2026-07-06T00:00:00Z",
				"icon": map[string]any{
					"type":      "Image",
					"mediaType": "image/webp",
					"url":       "https://host1.test/files/blob.webp",
				},
			},
		},
	}, "https://host1.test/notes/1")
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if len(note.MentionURIs) != 1 || note.MentionURIs[0] != "https://host2.test/users/bob" {
		t.Fatalf("MentionURIs = %#v", note.MentionURIs)
	}
	if len(note.Hashtags) != 1 || note.Hashtags[0] != "tag" {
		t.Fatalf("Hashtags = %#v", note.Hashtags)
	}
	if len(note.Emojis) != 1 {
		t.Fatalf("Emojis = %#v", note.Emojis)
	}
	if note.Emojis[0].Name != "blob" || note.Emojis[0].IconURL != "https://host1.test/files/blob.webp" || note.Emojis[0].MediaType != "image/webp" {
		t.Fatalf("Emoji = %+v", note.Emojis[0])
	}
}

func TestConcordeNoteVisibility(t *testing.T) {
	actorID := "https://host1.test/users/alice"
	cases := []struct {
		name string
		to   any
		cc   any
		want Visibility
	}{
		{name: "to public", to: PublicAudience, want: VisibilityPublic},
		{name: "cc public", cc: PublicAudience, want: VisibilityHome},
		{name: "followers", to: actorID + "/followers", want: VisibilityFollowers},
		{name: "specified", to: "https://host1.test/users/bob", want: VisibilitySpecified},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseVisibility(actorID, tt.to, tt.cc); got != tt.want {
				t.Fatalf("ParseVisibility = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConcordeNoteRejectsWrongAttributedToHost(t *testing.T) {
	err := ValidateNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host2.test/users/alice",
	}, "https://host1.test/notes/1")
	if err == nil {
		t.Fatalf("ValidateNote should reject wrong attributedTo host")
	}
}
