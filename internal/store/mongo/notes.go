package mongostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
)

type NoteRepository struct {
	collection *mongo.Collection
}

type noteDocument struct {
	ID             string                 `bson:"_id,omitempty"`
	URI            string                 `bson:"uri"`
	AttributedTo   string                 `bson:"attributedTo"`
	AuthorID       string                 `bson:"authorId"`
	Text           string                 `bson:"text"`
	ContentWarning *string                `bson:"contentWarning,omitempty"`
	Sensitive      bool                   `bson:"sensitive"`
	InReplyToURI   string                 `bson:"inReplyToUri,omitempty"`
	QuoteURI       string                 `bson:"quoteUri,omitempty"`
	Visibility     string                 `bson:"visibility"`
	MentionURIs    []string               `bson:"mentionUris,omitempty"`
	Hashtags       []string               `bson:"hashtags,omitempty"`
	Emojis         []emojiDocument        `bson:"emojis,omitempty"`
	Attachments    []attachmentDocument   `bson:"attachments,omitempty"`
	Raw            map[string]interface{} `bson:"raw"`
	CreatedAt      time.Time              `bson:"createdAt"`
	PublishedAt    *time.Time             `bson:"publishedAt,omitempty"`
}

type emojiDocument struct {
	Name      string     `bson:"name"`
	URI       string     `bson:"uri,omitempty"`
	UpdatedAt *time.Time `bson:"updatedAt,omitempty"`
	IconURL   string     `bson:"iconUrl,omitempty"`
	MediaType string     `bson:"mediaType,omitempty"`
}

type attachmentDocument struct {
	URI       string `bson:"uri,omitempty"`
	Type      string `bson:"type,omitempty"`
	MediaType string `bson:"mediaType,omitempty"`
	URL       string `bson:"url"`
	Name      string `bson:"name,omitempty"`
	Sensitive bool   `bson:"sensitive"`
}

func NewNoteRepository(db *mongo.Database) *NoteRepository {
	return &NoteRepository{collection: db.Collection("notes")}
}

func (r *NoteRepository) FindByID(ctx context.Context, id string) (*domainnotes.Note, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

func (r *NoteRepository) FindByURI(ctx context.Context, uri string) (*domainnotes.Note, error) {
	return r.findOne(ctx, bson.M{"uri": uri})
}

func (r *NoteRepository) UpsertRemoteNote(ctx context.Context, note domainnotes.Note) (*domainnotes.Note, error) {
	if note.URI == "" {
		return nil, fmt.Errorf("note uri is required")
	}
	if note.ID == "" {
		note.ID = remoteNoteID(note.URI)
	}
	if note.CreatedAt.IsZero() {
		note.CreatedAt = time.Now().UTC()
	}
	doc := fromNote(note)
	_, err := r.collection.UpdateOne(ctx, bson.M{"uri": note.URI}, bson.M{
		"$setOnInsert": doc,
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindByURI(ctx, note.URI)
}

func (r *NoteRepository) findOne(ctx context.Context, filter bson.M) (*domainnotes.Note, error) {
	var doc noteDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &domainnotes.Note{
		ID:             doc.ID,
		URI:            doc.URI,
		AttributedTo:   doc.AttributedTo,
		AuthorID:       doc.AuthorID,
		Text:           doc.Text,
		ContentWarning: doc.ContentWarning,
		Sensitive:      doc.Sensitive,
		InReplyToURI:   doc.InReplyToURI,
		QuoteURI:       doc.QuoteURI,
		Visibility:     domainnotes.Visibility(doc.Visibility),
		MentionURIs:    doc.MentionURIs,
		Hashtags:       doc.Hashtags,
		Emojis:         toDomainEmojis(doc.Emojis),
		Attachments:    toDomainAttachments(doc.Attachments),
		Raw:            mapAny(doc.Raw),
		CreatedAt:      doc.CreatedAt,
		PublishedAt:    doc.PublishedAt,
	}, nil
}

func fromNote(note domainnotes.Note) noteDocument {
	return noteDocument{
		ID:             note.ID,
		URI:            note.URI,
		AttributedTo:   note.AttributedTo,
		AuthorID:       note.AuthorID,
		Text:           note.Text,
		ContentWarning: note.ContentWarning,
		Sensitive:      note.Sensitive,
		InReplyToURI:   note.InReplyToURI,
		QuoteURI:       note.QuoteURI,
		Visibility:     string(note.Visibility),
		MentionURIs:    note.MentionURIs,
		Hashtags:       note.Hashtags,
		Emojis:         fromDomainEmojis(note.Emojis),
		Attachments:    fromDomainAttachments(note.Attachments),
		Raw:            mapInterface(note.Raw),
		CreatedAt:      note.CreatedAt,
		PublishedAt:    note.PublishedAt,
	}
}

func toDomainEmojis(src []emojiDocument) []domainnotes.Emoji {
	if len(src) == 0 {
		return nil
	}
	out := make([]domainnotes.Emoji, 0, len(src))
	for _, emoji := range src {
		out = append(out, domainnotes.Emoji{
			Name:      emoji.Name,
			URI:       emoji.URI,
			UpdatedAt: emoji.UpdatedAt,
			IconURL:   emoji.IconURL,
			MediaType: emoji.MediaType,
		})
	}
	return out
}

func fromDomainEmojis(src []domainnotes.Emoji) []emojiDocument {
	if len(src) == 0 {
		return nil
	}
	out := make([]emojiDocument, 0, len(src))
	for _, emoji := range src {
		out = append(out, emojiDocument{
			Name:      emoji.Name,
			URI:       emoji.URI,
			UpdatedAt: emoji.UpdatedAt,
			IconURL:   emoji.IconURL,
			MediaType: emoji.MediaType,
		})
	}
	return out
}

func toDomainAttachments(src []attachmentDocument) []domainnotes.Attachment {
	if len(src) == 0 {
		return nil
	}
	out := make([]domainnotes.Attachment, 0, len(src))
	for _, attachment := range src {
		out = append(out, domainnotes.Attachment{
			URI:       attachment.URI,
			Type:      attachment.Type,
			MediaType: attachment.MediaType,
			URL:       attachment.URL,
			Name:      attachment.Name,
			Sensitive: attachment.Sensitive,
		})
	}
	return out
}

func fromDomainAttachments(src []domainnotes.Attachment) []attachmentDocument {
	if len(src) == 0 {
		return nil
	}
	out := make([]attachmentDocument, 0, len(src))
	for _, attachment := range src {
		out = append(out, attachmentDocument{
			URI:       attachment.URI,
			Type:      attachment.Type,
			MediaType: attachment.MediaType,
			URL:       attachment.URL,
			Name:      attachment.Name,
			Sensitive: attachment.Sensitive,
		})
	}
	return out
}

func remoteNoteID(uri string) string {
	sum := sha256.Sum256([]byte(uri))
	return "remote_note_" + hex.EncodeToString(sum[:])[:24]
}

func mapInterface(src map[string]any) map[string]interface{} {
	if src == nil {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func mapAny(src map[string]interface{}) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
