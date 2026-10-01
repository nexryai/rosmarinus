package queue

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
)

type fakeStatusInspector struct {
	names []string
	info  map[string]*asynq.QueueInfo
	err   error
}

func (f *fakeStatusInspector) Queues() ([]string, error) { return f.names, f.err }
func (f *fakeStatusInspector) GetQueueInfo(name string) (*asynq.QueueInfo, error) {
	return f.info[name], f.err
}
func (f *fakeStatusInspector) Close() error { return nil }

func TestStatusSnapshotIncludesEmptyQueuesAndCounts(t *testing.T) {
	reader := &AsynqStatusReader{inspector: &fakeStatusInspector{
		names: []string{QueueInbox},
		info: map[string]*asynq.QueueInfo{QueueInbox: {
			Active: 2, Pending: 3, Retry: 4, Scheduled: 5, Archived: 6, Completed: 7, Aggregating: 8, Paused: true,
		}},
	}}
	snapshot, err := reader.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Queues) != len(statusQueueNames) {
		t.Fatalf("queues = %d", len(snapshot.Queues))
	}
	first := snapshot.Queues[0]
	if first.Name != QueueInbox || first.Active != 2 || first.Pending != 3 || first.Retry != 4 || first.Scheduled != 5 || first.Archived != 6 || first.Completed != 7 || first.Aggregating != 8 || !first.Paused {
		t.Fatalf("inbox status = %+v", first)
	}
	if snapshot.Queues[1].Name != QueueDeliver || snapshot.Queues[1].Active != 0 {
		t.Fatalf("empty queue = %+v", snapshot.Queues[1])
	}
}

func TestStatusSnapshotReturnsInspectorError(t *testing.T) {
	reader := &AsynqStatusReader{inspector: &fakeStatusInspector{err: errors.New("redis unavailable")}}
	if _, err := reader.Snapshot(context.Background()); err == nil {
		t.Fatal("expected inspector error")
	}
}
