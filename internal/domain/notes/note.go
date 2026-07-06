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
	ID           string
	URI          string
	AttributedTo string
	AuthorID     string
	Text         string
	Visibility   Visibility
	Raw          map[string]any
	CreatedAt    time.Time
	PublishedAt  *time.Time
}

type Repository interface {
	FindByID(context.Context, string) (*Note, error)
	FindByURI(context.Context, string) (*Note, error)
	UpsertRemoteNote(context.Context, Note) (*Note, error)
}
