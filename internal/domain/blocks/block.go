package blocks

import (
	"context"
	"time"
)

type Block struct {
	ID                   string
	BlockerID            string
	BlockeeID            string
	BlockerURI           string
	BlockeeURI           string
	BlockerHost          *string
	BlockeeHost          *string
	CreatedAt            time.Time
	RemoteActivityID     string
	RemoteUndoActivityID string
}

type Repository interface {
	Find(context.Context, string, string) (*Block, error)
	Upsert(context.Context, Block) (*Block, error)
	Delete(context.Context, string, string, string) error
}
