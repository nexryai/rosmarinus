package emojis

import (
	"context"
	"time"
)

type Emoji struct {
	ID              string
	Host            string
	Name            string
	URI             string
	OriginalURL     string
	MediaType       string
	RemoteUpdatedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Repository interface {
	UpsertRemote(context.Context, Emoji) (*Emoji, error)
}
