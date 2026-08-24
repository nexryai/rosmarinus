package queue

import (
	"context"
	"time"
)

const (
	TaskInbox         = "activitypub:inbox"
	TaskDeliver       = "activitypub:deliver"
	TaskMetadata      = "instance:metadata"
	TaskMedia         = "media:fetch"
	TaskPollEnded     = "poll:ended"
	TaskAccountDelete = "account:delete"

	QueueInbox         = "inbox"
	QueueDeliver       = "deliver"
	QueueSystem        = "system"
	QueuePollEnded     = "poll-ended"
	QueueMedia         = "media"
	QueueMetadata      = "metadata"
	QueueAccountDelete = "account-delete"
)

type Task struct {
	Type      string
	Queue     string
	Payload   any
	MaxRetry  int
	Timeout   time.Duration
	ProcessIn time.Duration
}

type Client interface {
	Enqueue(context.Context, Task) error
	Close() error
}

type InboxPayload struct {
	Version   int            `json:"version"`
	Activity  map[string]any `json:"activity"`
	Signature map[string]any `json:"signature"`
}

type DeliverPayload struct {
	Version int            `json:"version"`
	ActorID string         `json:"actor_id"`
	To      string         `json:"to"`
	Object  map[string]any `json:"object"`
}

type AccountDeletePayload struct {
	Version  int    `json:"version"`
	ActorID  string `json:"actor_id"`
	ActorURI string `json:"actor_uri"`
}

type PollEndedPayload struct {
	Version int    `json:"version"`
	NoteID  string `json:"note_id"`
}

type MediaFetchPayload struct {
	Version int    `json:"version"`
	MediaID string `json:"media_id"`
	URL     string `json:"url"`
}

func NewInboxTask(activity map[string]any, signature map[string]any, maxRetry int, timeout time.Duration) Task {
	return Task{
		Type:     TaskInbox,
		Queue:    QueueInbox,
		MaxRetry: maxRetry,
		Timeout:  timeout,
		Payload: InboxPayload{
			Version:   1,
			Activity:  activity,
			Signature: signature,
		},
	}
}

func NewDeliverTask(actorID, to string, object map[string]any, maxRetry int, timeout time.Duration) Task {
	return Task{
		Type:     TaskDeliver,
		Queue:    QueueDeliver,
		MaxRetry: maxRetry,
		Timeout:  timeout,
		Payload: DeliverPayload{
			Version: 1,
			ActorID: actorID,
			To:      to,
			Object:  object,
		},
	}
}

func NewAccountDeleteTask(actorID, actorURI string) Task {
	return Task{
		Type:     TaskAccountDelete,
		Queue:    QueueAccountDelete,
		MaxRetry: 10,
		Timeout:  5 * time.Minute,
		Payload: AccountDeletePayload{
			Version:  1,
			ActorID:  actorID,
			ActorURI: actorURI,
		},
	}
}

func NewPollEndedTask(noteID string, processIn time.Duration) Task {
	if processIn < 0 {
		processIn = 0
	}
	return Task{
		Type: TaskPollEnded, Queue: QueuePollEnded, MaxRetry: 10, Timeout: 5 * time.Minute, ProcessIn: processIn,
		Payload: PollEndedPayload{Version: 1, NoteID: noteID},
	}
}

func NewMediaFetchTask(mediaID, rawURL string) Task {
	return Task{
		Type: TaskMedia, Queue: QueueMedia, MaxRetry: 7, Timeout: 2 * time.Minute,
		Payload: MediaFetchPayload{Version: 1, MediaID: mediaID, URL: rawURL},
	}
}
