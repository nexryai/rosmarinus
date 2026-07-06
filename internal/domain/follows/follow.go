package follows

import (
	"context"
	"time"
)

type Follow struct {
	ID                   string
	FollowerID           string
	FolloweeID           string
	FollowerURI          string
	FolloweeURI          string
	FollowerHost         *string
	FolloweeHost         *string
	FollowerInbox        string
	FollowerSharedInbox  string
	FolloweeInbox        string
	FolloweeSharedInbox  string
	CreatedAt            time.Time
	RemoteActivityID     string
	RemoteUndoActivityID string
}

type Repository interface {
	Find(context.Context, string, string) (*Follow, error)
	Upsert(context.Context, Follow) (*Follow, error)
	Delete(context.Context, string, string, string) error
}
