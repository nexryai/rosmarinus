package mongostore

import (
	"testing"

	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
)

func TestNoteDocumentPreservesSpecifiedAudience(t *testing.T) {
	doc := fromNote(domainnotes.Note{
		ID:              "note-1",
		Visibility:      domainnotes.VisibilitySpecified,
		MentionURIs:     []string{"https://remote.example/users/bob"},
		VisibleUserURIs: []string{"https://remote.example/users/carol"},
	})
	if len(doc.MentionURIs) != 1 || doc.MentionURIs[0] != "https://remote.example/users/bob" {
		t.Fatalf("mention URIs = %#v", doc.MentionURIs)
	}
	if len(doc.VisibleUserURIs) != 1 || doc.VisibleUserURIs[0] != "https://remote.example/users/carol" {
		t.Fatalf("visible user URIs = %#v", doc.VisibleUserURIs)
	}
}

func TestNoteDocumentPreservesAttachmentMetadata(t *testing.T) {
	note := domainnotes.Note{Attachments: []domainnotes.Attachment{{
		URI: "https://remote.example/files/1", Type: "Document", MediaType: "image/png",
		URL: "https://remote.example/files/1.png", Name: "image", Width: 3600, Height: 1890, Sensitive: true,
	}}}
	doc := fromNote(note)
	attachments := toDomainAttachments(doc.Attachments)
	if len(attachments) != 1 || attachments[0] != note.Attachments[0] {
		t.Fatalf("attachments = %#v", attachments)
	}
}
