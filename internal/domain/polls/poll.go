package polls

import (
	"context"
	"time"
)

type Poll struct {
	NoteID     string
	AuthorID   string
	AuthorHost *string
	Choices    []string
	Votes      []int
	Multiple   bool
	ExpiresAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Repository interface {
	FindByNoteID(context.Context, string) (*Poll, error)
	UpsertRemote(context.Context, Poll) (*Poll, error)
	UpdateRemoteVotes(context.Context, string, string, []int) (*Poll, error)
}
