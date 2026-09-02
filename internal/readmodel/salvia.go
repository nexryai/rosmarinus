package readmodel

import (
	"context"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	"github.com/nexryai/rosmarinus/internal/domain/polls"
)

type Cursor struct {
	CreatedAt time.Time
	ID        string
}

type Note struct {
	Note      notes.Note
	Author    *actors.Actor
	Poll      *polls.Poll
	Reactions []ReactionSummary
	MyVotes   []int
	Reply     *NoteReference
	Quote     *NoteReference
	Renote    *NoteReference
}

type NoteReference struct {
	Note   notes.Note
	Author *actors.Actor
}

type ReactionSummary struct {
	Reaction string
	Count    int
	Reacted  bool
}

type Connection struct {
	Follow follows.Follow
	Actor  *actors.Actor
}

type Notification struct {
	Notification notifications.Notification
	Source       *actors.Actor
	Note         *Note
}

type Profile struct {
	Actor          *actors.Actor
	FollowersCount int
	FollowingCount int
}

type Reader interface {
	ListPublicTimeline(context.Context, string, Cursor, int) ([]Note, error)
	ListHomeTimeline(context.Context, string, Cursor, int) ([]Note, error)
	FindVisibleNote(context.Context, string, string) (*Note, error)
	ListVisibleThread(context.Context, string, string, Cursor, int) ([]Note, error)
	ListConnections(context.Context, string, string, string, string, int) ([]Connection, error)
	ListNotifications(context.Context, string, string, Cursor, int, *bool) ([]Notification, error)
	ListLocalEmojis(context.Context, string, int) ([]emojis.Emoji, error)
	FindProfile(context.Context, string, string) (*Profile, error)
}
