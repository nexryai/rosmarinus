package polls

import (
	"context"
	"errors"
	"time"
)

var ErrExpired = errors.New("poll is expired")
var ErrInvalidChoice = errors.New("poll choice is invalid")
var ErrAlreadyVoted = errors.New("poll vote already exists")
var ErrNotFound = errors.New("poll not found")

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

type Vote struct {
	ID        string
	NoteID    string
	ActorID   string
	Choice    int
	CreatedAt time.Time
}

type Repository interface {
	FindByNoteID(context.Context, string) (*Poll, error)
	UpsertLocal(context.Context, Poll) (*Poll, error)
	UpsertRemote(context.Context, Poll) (*Poll, error)
	UpdateRemoteVotes(context.Context, string, string, []int) (*Poll, error)
	RecordVote(context.Context, string, string, int, time.Time) (*Vote, *Poll, error)
	ListVoterActorIDs(context.Context, string) ([]string, error)
}
