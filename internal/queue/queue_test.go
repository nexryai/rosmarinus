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

func TestNewPollEndedTask(t *testing.T) {
	task := NewPollEndedTask("poll-note", 10*time.Minute)
	if task.Type != TaskPollEnded || task.Queue != QueuePollEnded || task.ProcessIn != 10*time.Minute {
		t.Fatalf("unexpected task: %+v", task)
	}
	payload, ok := task.Payload.(PollEndedPayload)
	if !ok || payload.Version != 1 || payload.NoteID != "poll-note" {
		t.Fatalf("unexpected payload: %#v", task.Payload)
	}
}

func TestNewMediaFetchTask(t *testing.T) {
	task := NewMediaFetchTask("media-id", "https://remote.example/file.png")
	if task.Type != TaskMedia || task.Queue != QueueMedia || task.MaxRetry != 7 || task.Timeout != 2*time.Minute {
		t.Fatalf("unexpected task: %+v", task)
	}
	payload, ok := task.Payload.(MediaFetchPayload)
	if !ok || payload.Version != 1 || payload.MediaID != "media-id" || payload.URL != "https://remote.example/file.png" {
		t.Fatalf("unexpected payload: %#v", task.Payload)
	}
}

func TestAPBackoffRange(t *testing.T) {
	got := APBackoff(0, nil, nil)
	if got < time.Minute || got > 72*time.Second {
		t.Fatalf("first retry backoff = %s", got)
	}
	got = APBackoff(1, nil, nil)
	if got < 3*time.Minute || got > 216*time.Second {
		t.Fatalf("second retry backoff = %s", got)
	}
	got = APBackoff(8, nil, nil)
	if got < 8*time.Hour || got > 8*time.Hour+96*time.Minute {
		t.Fatalf("capped backoff = %s", got)
	}
}

func TestAPBackoffBaseMatchesCurrentMisskeyAttempts(t *testing.T) {
	tests := []struct {
		retriesCompleted int
		want             time.Duration
	}{
		{retriesCompleted: 0, want: time.Minute},
		{retriesCompleted: 1, want: 3 * time.Minute},
		{retriesCompleted: 2, want: 7 * time.Minute},
		{retriesCompleted: 8, want: 8 * time.Hour},
	}
	for _, tt := range tests {
		if got := apBackoffBase(tt.retriesCompleted); got != tt.want {
			t.Fatalf("apBackoffBase(%d) = %s, want %s", tt.retriesCompleted, got, tt.want)
		}
	}
}
