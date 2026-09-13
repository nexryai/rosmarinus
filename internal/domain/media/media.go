package media

import (
	"context"
	"errors"
	"time"
)

var (
	ErrObjectKeyConflict = errors.New("media object key already exists")
	ErrNotFound          = errors.New("media not found")
	ErrInvalidObject     = errors.New("uploaded media object is invalid")
)

const (
	StatePending = "pending"
	StateReady   = "ready"
	StateFailed  = "failed"
)

type Media struct {
	ID           string
	ObjectKey    string
	OwnerActorID string
	Name         string
	Width        int
	Height       int
	OriginalURL  string
	PublicURL    string
	ContentType  string
	Size         int64
	SHA256       string
	State        string
	Error        string
	CreatedAt    time.Time
	FetchedAt    *time.Time
}

type Repository interface {
	FindByID(context.Context, string) (*Media, error)
	ListByOwner(context.Context, string) ([]Media, error)
	CreatePendingLocal(context.Context, string, string, string, string, string, string, int64, string, int, int) (*Media, error)
	UpsertPending(context.Context, string, string, string) (*Media, error)
	MarkReady(context.Context, string, string, int64, string) (*Media, error)
	MarkFailed(context.Context, string, string) error
	Delete(context.Context, string) error
}
