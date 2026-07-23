package reactions

import (
	"context"
	"time"
)

type Reaction struct {
	ID                   string
	NoteID               string
	NoteURI              string
	ActorID              string
	ActorURI             string
	ActorHost            *string
	Reaction             string
	RemoteActivityID     string
	RemoteUndoActivityID string
	CreatedAt            time.Time
}

type Repository interface {
	Find(context.Context, string, string) (*Reaction, error)
	FindByID(context.Context, string) (*Reaction, error)
	Upsert(context.Context, Reaction) (*Reaction, error)
	Delete(context.Context, string, string, string) error
}
