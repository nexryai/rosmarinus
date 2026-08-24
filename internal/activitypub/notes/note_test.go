package notes

import (
	"fmt"
	"testing"
)

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

func TestCurrentMisskeyNoteTextConvertsHTMLToMFM(t *testing.T) {
	note, err := ParseRemoteNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host1.test/users/alice",
		"to":           PublicAudience,
		"content":      `<p>Hello <strong>world</strong> <a href="https://host2.test/@bob">@bob</a> <a href="https://host1.test/tags/Go">#Go</a></p>`,
		"tag": []any{
			map[string]any{"type": "Hashtag", "name": "#Go"},
		},
	}, "https://host1.test/notes/1")
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if note.Text != "Hello **world** @bob@host2.test #Go" {
		t.Fatalf("Text = %q", note.Text)
	}
}

func TestCurrentMisskeyNoteTextPrefersLegacyMFMOverHTML(t *testing.T) {
	note, err := ParseRemoteNote(map[string]any{
		"id":               "https://host1.test/notes/1",
		"type":             "Note",
		"attributedTo":     "https://host1.test/users/alice",
		"to":               PublicAudience,
		"content":          "<p>html</p>",
		"_misskey_content": "$[tada legacy]",
	}, "https://host1.test/notes/1")
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if note.Text != "$[tada legacy]" {
		t.Fatalf("Text = %q", note.Text)
	}
}

func TestConcordeNoteExtractsCWSensitiveReplyAndQuote(t *testing.T) {
	note, err := ParseRemoteNote(map[string]any{
		"id":             "https://host1.test/notes/1",
		"type":           "Note",
		"attributedTo":   "https://host1.test/users/alice",
		"to":             PublicAudience,
		"summary":        "cw",
		"sensitive":      true,
		"inReplyTo":      "https://host1.test/notes/root",
		"_misskey_quote": "https://host1.test/notes/quote",
		"quoteUrl":       "https://host1.test/notes/other",
		"content":        "hidden",
	}, "https://host1.test/notes/1")
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if note.ContentWarning == nil || *note.ContentWarning != "cw" {
		t.Fatalf("ContentWarning = %#v", note.ContentWarning)
	}
	if !note.Sensitive {
		t.Fatalf("Sensitive = false")
	}
	if note.InReplyToURI != "https://host1.test/notes/root" {
		t.Fatalf("InReplyToURI = %q", note.InReplyToURI)
	}
	if note.QuoteURI != "https://host1.test/notes/quote" {
		t.Fatalf("QuoteURI = %q", note.QuoteURI)
	}
}

func TestConcordeNoteEmptySummaryIsNotCW(t *testing.T) {
	note, err := ParseRemoteNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host1.test/users/alice",
		"to":           PublicAudience,
		"summary":      "",
		"content":      "plain",
	}, "https://host1.test/notes/1")
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if note.ContentWarning != nil {
		t.Fatalf("ContentWarning = %#v", note.ContentWarning)
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

func TestConcordeNoteExtractsAttachments(t *testing.T) {
	note, err := ParseRemoteNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host1.test/users/alice",
		"to":           PublicAudience,
		"sensitive":    true,
		"content":      "with file",
		"attachment": []any{
			map[string]any{
				"id":        "https://host1.test/files/1",
				"type":      "Document",
				"mediaType": "image/png",
				"url":       "https://host1.test/files/1.png",
				"name":      "image",
			},
			map[string]any{
				"type":      "Image",
				"mediaType": "image/webp",
				"url": []any{
					map[string]any{"href": "https://host1.test/files/2.webp"},
				},
				"sensitive": true,
			},
		},
	}, "https://host1.test/notes/1")
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if len(note.Attachments) != 2 {
		t.Fatalf("Attachments = %#v", note.Attachments)
	}
	if note.Attachments[0].URL != "https://host1.test/files/1.png" || note.Attachments[0].MediaType != "image/png" || !note.Attachments[0].Sensitive {
		t.Fatalf("Attachment[0] = %+v", note.Attachments[0])
	}
	if note.Attachments[1].URL != "https://host1.test/files/2.webp" || note.Attachments[1].Type != "Image" || !note.Attachments[1].Sensitive {
		t.Fatalf("Attachment[1] = %+v", note.Attachments[1])
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

func TestCurrentMisskeyAudienceAddsDirectRecipientsToMentions(t *testing.T) {
	recipient := "https://host2.test/users/bob"
	note, err := ParseRemoteNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host1.test/users/alice",
		"to":           []any{recipient, recipient},
		"content":      "direct",
	}, "https://host1.test/notes/1")
	if err != nil {
		t.Fatalf("ParseRemoteNote returned error: %v", err)
	}
	if note.Visibility != VisibilitySpecified {
		t.Fatalf("visibility = %q", note.Visibility)
	}
	if len(note.MentionURIs) != 1 || note.MentionURIs[0] != recipient {
		t.Fatalf("mention URIs = %#v", note.MentionURIs)
	}
	if len(note.VisibleUserURIs) != 1 || note.VisibleUserURIs[0] != recipient {
		t.Fatalf("visible user URIs = %#v", note.VisibleUserURIs)
	}
}

func TestCurrentMisskeyAudienceLimitIncludesDirectRecipients(t *testing.T) {
	recipients := make([]any, 0, maxRemoteNoteMentions+1)
	for i := 0; i <= maxRemoteNoteMentions; i++ {
		recipients = append(recipients, fmt.Sprintf("https://host2.test/users/%d", i))
	}
	err := ValidateNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host1.test/users/alice",
		"to":           recipients,
	}, "https://host1.test/notes/1")
	if err == nil {
		t.Fatal("ValidateNote accepted too many direct recipients")
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

func TestCurrentMisskeyNoteValidationRejectsUnsafePublishedTimestamp(t *testing.T) {
	for _, published := range []any{"not-a-date", "1999-12-31T23:59:59Z", 1} {
		err := ValidateNote(map[string]any{
			"id":           "https://host1.test/notes/1",
			"type":         "Note",
			"attributedTo": "https://host1.test/users/alice",
			"published":    published,
		}, "https://host1.test/notes/1")
		if err == nil {
			t.Fatalf("ValidateNote accepted published=%v", published)
		}
	}
}

func TestCurrentMisskeyNoteValidationRequiresHTTPSURL(t *testing.T) {
	for _, noteURL := range []any{
		"http://host1.test/notes/1",
		map[string]any{"href": "http://host1.test/notes/1"},
	} {
		err := ValidateNote(map[string]any{
			"id":           "https://host1.test/notes/1",
			"type":         "Note",
			"attributedTo": "https://host1.test/users/alice",
			"url":          noteURL,
		}, "https://host1.test/notes/1")
		if err == nil {
			t.Fatalf("ValidateNote accepted url=%v", noteURL)
		}
	}
}

func TestCurrentMisskeyNoteValidationLimitsRawMentions(t *testing.T) {
	tags := make([]any, 0, maxRemoteNoteMentions+2)
	for i := 0; i <= maxRemoteNoteMentions; i++ {
		tags = append(tags, map[string]any{
			"type": "Mention",
			"href": fmt.Sprintf("https://host2.test/users/%d", i),
		})
	}
	tags = append(tags, tags[0])
	err := ValidateNote(map[string]any{
		"id":           "https://host1.test/notes/1",
		"type":         "Note",
		"attributedTo": "https://host1.test/users/alice",
		"tag":          tags,
	}, "https://host1.test/notes/1")
	if err == nil {
		t.Fatal("ValidateNote accepted more than 20 unique mentions")
	}
}
