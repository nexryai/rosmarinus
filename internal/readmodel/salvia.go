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

type EmojiCursor struct {
	Host string
	Name string
	ID   string
}

type EmojiListQuery struct {
	Remote bool
	Query  string
	Host   string
	After  EmojiCursor
	Limit  int
}

type Note struct {
	Note         notes.Note
	Author       *actors.Actor
	Poll         *polls.Poll
	Reactions    []ReactionSummary
	MyVotes      []int
	RepliesCount int
	Reply        *NoteReference
	Quote        *NoteReference
	Renote       *NoteReference
}

type NoteReference struct {
	Note         notes.Note
	Author       *actors.Actor
	Reactions    []ReactionSummary
	RepliesCount int
	Reply        *NoteReference
	Quote        *NoteReference
}

type ReactionSummary struct {
	Reaction string
	Count    int
	Reacted  bool
	Emoji    *emojis.Reference
}

type Connection struct {
	Follow follows.Follow
	Actor  *actors.Actor
}

type Notification struct {
	Notification  notifications.Notification
	Source        *actors.Actor
	Note          *Note
	Reaction      string
	ReactionEmoji *emojis.Reference
}

type Profile struct {
	Actor           *actors.Actor
	FollowersCount  int
	FollowingCount  int
	FollowStatus    string
	BlockedByViewer bool
	MutedByViewer   bool
	MuteExpiresAt   *time.Time
	PinnedNotes     []Note
}

type Reader interface {
	ListPublicTimeline(context.Context, string, Cursor, int) ([]Note, error)
	ListHomeTimeline(context.Context, string, Cursor, int) ([]Note, error)
	FindVisibleNote(context.Context, string, string) (*Note, error)
	ListVisibleThread(context.Context, string, string, Cursor, int) ([]Note, error)
	ListProfileNotes(context.Context, string, string, Cursor, int) ([]Note, error)
	ListConnections(context.Context, string, string, string, string, int) ([]Connection, error)
	ListNotifications(context.Context, string, string, Cursor, int, *bool) ([]Notification, error)
	CountUnreadNotifications(context.Context, string, string) (int64, error)
	ListEmojis(context.Context, EmojiListQuery) ([]emojis.Emoji, error)
	FindProfile(context.Context, string, string) (*Profile, error)
}
