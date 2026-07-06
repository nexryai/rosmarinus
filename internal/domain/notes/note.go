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
	ID             string
	URI            string
	AttributedTo   string
	AuthorID       string
	Text           string
	ContentWarning *string
	Sensitive      bool
	InReplyToURI   string
	QuoteURI       string
	Visibility     Visibility
	MentionURIs    []string
	Hashtags       []string
	Emojis         []Emoji
	Raw            map[string]any
	CreatedAt      time.Time
	PublishedAt    *time.Time
}

type Emoji struct {
	Name      string
	URI       string
	UpdatedAt *time.Time
	IconURL   string
	MediaType string
}

type Repository interface {
	FindByID(context.Context, string) (*Note, error)
	FindByURI(context.Context, string) (*Note, error)
	UpsertRemoteNote(context.Context, Note) (*Note, error)
}
