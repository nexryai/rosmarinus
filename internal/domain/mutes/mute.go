package mutes

import (
	"context"
	"time"
)

type Mute struct {
	ID        string
	MuterID   string
	MuteeID   string
	CreatedAt time.Time
	ExpiresAt *time.Time
}

func (mute Mute) Active(now time.Time) bool {
	return mute.ExpiresAt == nil || mute.ExpiresAt.After(now)
}

type Repository interface {
	FindActive(context.Context, string, string, time.Time) (*Mute, error)
	ListActiveMuteeIDs(context.Context, string, time.Time) ([]string, error)
	Upsert(context.Context, Mute) (*Mute, error)
	Delete(context.Context, string, string) error
}
