package notifications

import (
	"context"
	"time"
)

const (
	KindFollowRequest = "followRequest"
	KindReaction      = "reaction"
	KindRenote        = "renote"
	KindReply         = "reply"
	KindMention       = "mention"
	KindPollEnded     = "pollEnded"
)

type Notification struct {
	ID                 string
	RecipientAccountID string
	RecipientActorID   string
	Kind               string
	SourceActorID      string
	NoteID             string
	RemoteActivityID   string
	CreatedAt          time.Time
	IsRead             bool
	ReadAt             *time.Time
}

type Repository interface {
	Upsert(context.Context, Notification) (*Notification, error)
	MarkRead(context.Context, string, string, string) (*Notification, error)
}
