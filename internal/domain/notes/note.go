package notes

import (
	"context"
	"time"
)

type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityHome      Visibility = "home"
	VisibilityFollowers Visibility = "followers"
	VisibilitySpecified Visibility = "specified"
)

type Note struct {
	ID              string
	URI             string
	AttributedTo    string
	AuthorID        string
	Text            string
	ContentWarning  *string
	Sensitive       bool
	InReplyToURI    string
	ReplyID         string
	QuoteURI        string
	QuoteID         string
	RenoteID        string
	RenoteURI       string
	Visibility      Visibility
	MentionURIs     []string
	VisibleUserURIs []string
	Hashtags        []string
	Emojis          []Emoji
	Attachments     []Attachment
	Raw             map[string]any
	CreatedAt       time.Time
	PublishedAt     *time.Time
	DeletedAt       *time.Time
}

type Emoji struct {
	Name      string
	URI       string
	UpdatedAt *time.Time
	IconURL   string
	MediaType string
}

type Attachment struct {
	URI       string
	Type      string
	MediaType string
	URL       string
	Name      string
	Sensitive bool
}

type Repository interface {
	FindByID(context.Context, string) (*Note, error)
	FindAnyByID(context.Context, string) (*Note, error)
	FindByURI(context.Context, string) (*Note, error)
	FindAnyByURI(context.Context, string) (*Note, error)
	CreateLocalNote(context.Context, Note) (*Note, error)
	UpsertRemoteNote(context.Context, Note) (*Note, error)
	DeleteLocalNote(context.Context, string, string) error
	DeleteRemoteNote(context.Context, string, string) error
}
