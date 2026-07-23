package follows

import (
	"context"
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusAccepted Status = "accepted"
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
	Status               Status
	AcceptedAt           *time.Time
	RemoteActivityID     string
	RemoteUndoActivityID string
}

type Repository interface {
	Find(context.Context, string, string) (*Follow, error)
	ListFollowers(context.Context, string, int) ([]Follow, error)
	ListFollowersPage(context.Context, string, string, int) ([]Follow, error)
	Upsert(context.Context, Follow) (*Follow, error)
	Approve(context.Context, string, string) (*Follow, error)
	Delete(context.Context, string, string, string) error
}
