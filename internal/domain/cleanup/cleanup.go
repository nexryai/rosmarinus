package cleanup

import "context"

type Result struct {
	Notes         int64
	Reactions     int64
	Follows       int64
	Blocks        int64
	Polls         int64
	Notifications int64
}

type NoteResult struct {
	Reactions     int64
	Polls         int64
	PollVotes     int64
	Notifications int64
}

type Repository interface {
	CleanupRemoteActor(context.Context, string) (Result, error)
	CleanupNote(context.Context, string) (NoteResult, error)
}
