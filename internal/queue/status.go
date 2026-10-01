package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

var statusQueueNames = [...]string{QueueInbox, QueueDeliver, QueueSystem, QueuePollEnded, QueueMedia, QueueMetadata, QueueAccountDelete}

type Status struct {
	Name        string `json:"name"`
	Pending     int    `json:"pending"`
	Active      int    `json:"active"`
	Scheduled   int    `json:"scheduled"`
	Retry       int    `json:"retry"`
	Archived    int    `json:"archived"`
	Completed   int    `json:"completed"`
	Aggregating int    `json:"aggregating"`
	Paused      bool   `json:"paused"`
}

type Snapshot struct {
	Queues    []Status  `json:"queues"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StatusReader interface {
	Snapshot(context.Context) (Snapshot, error)
}

type statusInspector interface {
	Queues() ([]string, error)
	GetQueueInfo(string) (*asynq.QueueInfo, error)
	Close() error
}

type AsynqStatusReader struct {
	inspector statusInspector
}

func NewAsynqStatusReader(redis RedisConfig) *AsynqStatusReader {
	return &AsynqStatusReader{inspector: asynq.NewInspector(redisOpt(redis))}
}

func (r *AsynqStatusReader) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	names, err := r.inspector.Queues()
	if err != nil {
		return Snapshot{}, fmt.Errorf("list queues: %w", err)
	}
	existing := make(map[string]bool, len(names))
	for _, name := range names {
		existing[name] = true
	}
	snapshot := Snapshot{Queues: make([]Status, 0, len(statusQueueNames)), UpdatedAt: time.Now().UTC()}
	for _, name := range statusQueueNames {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		status := Status{Name: name}
		if existing[name] {
			info, err := r.inspector.GetQueueInfo(name)
			if err != nil {
				return Snapshot{}, fmt.Errorf("inspect queue %s: %w", name, err)
			}
			status.Pending = info.Pending
			status.Active = info.Active
			status.Scheduled = info.Scheduled
			status.Retry = info.Retry
			status.Archived = info.Archived
			status.Completed = info.Completed
			status.Aggregating = info.Aggregating
			status.Paused = info.Paused
		}
		snapshot.Queues = append(snapshot.Queues, status)
	}
	return snapshot, nil
}

func (r *AsynqStatusReader) Close() error { return r.inspector.Close() }
