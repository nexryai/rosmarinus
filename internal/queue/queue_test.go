package queue

import (
	"context"
	"testing"
	"time"
)

type fakeClient struct {
	task Task
}

func (f *fakeClient) Enqueue(ctx context.Context, task Task) error {
	f.task = task
	return nil
}

func (f *fakeClient) Close() error {
	return nil
}

func TestNewInboxTask(t *testing.T) {
	task := NewInboxTask(map[string]any{"type": "Create"}, map[string]any{"keyId": "https://example.test/key"}, 10, 5*time.Minute)
	if task.Type != TaskInbox || task.Queue != QueueInbox {
		t.Fatalf("unexpected task routing: type=%s queue=%s", task.Type, task.Queue)
	}
	if task.MaxRetry != 10 || task.Timeout != 5*time.Minute {
		t.Fatalf("unexpected task options")
	}
	payload, ok := task.Payload.(InboxPayload)
	if !ok {
		t.Fatalf("payload type = %T", task.Payload)
	}
	if payload.Version != 1 {
		t.Fatalf("payload version = %d", payload.Version)
	}
}

func TestClientInterface(t *testing.T) {
	client := &fakeClient{}
	err := client.Enqueue(context.Background(), NewDeliverTask("actor", "https://remote.example/inbox", map[string]any{"type": "Create"}, 17, time.Minute))
	if err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}
	if client.task.Type != TaskDeliver || client.task.Queue != QueueDeliver {
		t.Fatalf("unexpected task: %+v", client.task)
	}
}

func TestNewAccountDeleteTask(t *testing.T) {
	task := NewAccountDeleteTask("actor-id", "https://remote.example/users/alice")
	if task.Type != TaskAccountDelete || task.Queue != QueueAccountDelete {
		t.Fatalf("unexpected task routing: type=%s queue=%s", task.Type, task.Queue)
	}
	if task.MaxRetry != 10 || task.Timeout != 5*time.Minute {
		t.Fatalf("unexpected task options")
	}
	payload, ok := task.Payload.(AccountDeletePayload)
	if !ok {
		t.Fatalf("payload type = %T", task.Payload)
	}
	if payload.Version != 1 || payload.ActorID != "actor-id" || payload.ActorURI != "https://remote.example/users/alice" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestAPBackoffRange(t *testing.T) {
	got := APBackoff(1, nil, nil)
	if got < 16*time.Second || got > 20*time.Second {
		t.Fatalf("attempt 1 backoff = %s", got)
	}
	got = APBackoff(100, nil, nil)
	if got < 8*time.Hour || got > 8*time.Hour+96*time.Minute {
		t.Fatalf("capped backoff = %s", got)
	}
}
