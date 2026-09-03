package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"
)

func IDForURL(originalURL string) string {
	sum := sha256.Sum256([]byte(originalURL))
	return "media_" + hex.EncodeToString(sum[:])[:32]
}

const (
	StatePending = "pending"
	StateReady   = "ready"
	StateFailed  = "failed"
)

type Media struct {
	ID           string
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
	UpsertPending(context.Context, string, string) (*Media, error)
	StoreBlob(context.Context, string, io.Reader, string, int64, string) error
	OpenBlob(context.Context, string) (io.ReadCloser, error)
	MarkReady(context.Context, string, string, int64, string) (*Media, error)
	MarkFailed(context.Context, string, string) error
}
