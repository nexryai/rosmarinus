package connector

import (
	"context"
	"testing"
	"time"
)

type dummyChannel struct {
	name string
	data any
}

func (d *dummyChannel) Publish(ctx context.Context, name string, data interface{}) error {
	_ = ctx
	d.name = name
	d.data = data
	return nil
}

func TestPublisherInjectsDummyChannel(t *testing.T) {
	channel := &dummyChannel{}
	publisher := NewPublisher(channel)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	publisher.now = func() time.Time { return now }

	err := publisher.PublishFollowApprovalRequested(context.Background(), FollowApproval{
		FollowerID:  "remote-alice",
		FolloweeID:  "relay",
		FollowerURI: "https://remote.example/users/alice",
		FolloweeURI: "https://rosmarinus.example/users/relay",
	})
	if err != nil {
		t.Fatalf("PublishFollowApprovalRequested returned error: %v", err)
	}
	if channel.name != EventFollowApprovalRequested {
		t.Fatalf("event name = %q", channel.name)
	}
	envelope, ok := channel.data.(Envelope)
	if !ok {
		t.Fatalf("data type = %T", channel.data)
	}
	if envelope.Version != 1 || envelope.Type != EventFollowApprovalRequested || !envelope.OccurredAt.Equal(now) {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	payload, ok := envelope.Data.(FollowApproval)
	if !ok {
		t.Fatalf("payload type = %T", envelope.Data)
	}
	if payload.FollowerID != "remote-alice" || payload.FolloweeID != "relay" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestPublisherAcceptsNilInstance(t *testing.T) {
	var publisher *Publisher
	if err := publisher.PublishPostCreated(context.Background(), PostCreated{ActorID: "relay", NoteID: "note"}); err != nil {
		t.Fatalf("PublishPostCreated returned error: %v", err)
	}
}

func TestPublisherRejectsEmptyEventType(t *testing.T) {
	err := NewPublisher(&dummyChannel{}).Publish(context.Background(), "", map[string]any{})
	if err == nil {
		t.Fatalf("expected empty event type to fail")
	}
}
