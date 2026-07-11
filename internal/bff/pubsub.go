package bff

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ably/ably-go/ably"
)

const (
	DefaultChannel = "rosmarinus:bff"

	EventPostCreated             = "post.created"
	EventNotificationCreated     = "notification.created"
	EventFollowApprovalRequested = "follow.approval.requested"
	EventFollowApprovalCompleted = "follow.approval.completed"
)

type Channel interface {
	Publish(context.Context, string, interface{}) error
}

type Publisher struct {
	channel Channel
	now     func() time.Time
}

type ablyRESTChannel struct {
	channel *ably.RESTChannel
}

type Envelope struct {
	Version    int       `json:"version"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurred_at"`
	Data       any       `json:"data"`
}

type PostCreated struct {
	ActorID string `json:"actor_id"`
	NoteID  string `json:"note_id"`
	URI     string `json:"uri"`
}

type NotificationCreated struct {
	RecipientActorID string `json:"recipient_actor_id"`
	NotificationID   string `json:"notification_id"`
	Kind             string `json:"kind"`
	SourceActorID    string `json:"source_actor_id,omitempty"`
	NoteID           string `json:"note_id,omitempty"`
}

type FollowApproval struct {
	FollowerID  string `json:"follower_id"`
	FolloweeID  string `json:"followee_id"`
	FollowerURI string `json:"follower_uri"`
	FolloweeURI string `json:"followee_uri"`
}

func NewPublisher(channel Channel) *Publisher {
	return &Publisher{
		channel: channel,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func NewAblyPublisher(apiKey, channelName string) (*Publisher, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("ably api key is required")
	}
	if strings.TrimSpace(channelName) == "" {
		channelName = DefaultChannel
	}
	client, err := ably.NewREST(ably.WithKey(apiKey))
	if err != nil {
		return nil, err
	}
	return NewPublisher(ablyRESTChannel{channel: client.Channels.Get(channelName)}), nil
}

func (c ablyRESTChannel) Publish(ctx context.Context, name string, data interface{}) error {
	return c.channel.Publish(ctx, name, data)
}

func (p *Publisher) Publish(ctx context.Context, eventType string, data any) error {
	if p == nil || p.channel == nil {
		return nil
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return fmt.Errorf("bff event type is required")
	}
	envelope := Envelope{
		Version:    1,
		Type:       eventType,
		OccurredAt: p.now(),
		Data:       data,
	}
	return p.channel.Publish(ctx, eventType, envelope)
}

func (p *Publisher) PublishPostCreated(ctx context.Context, payload PostCreated) error {
	return p.Publish(ctx, EventPostCreated, payload)
}

func (p *Publisher) PublishNotificationCreated(ctx context.Context, payload NotificationCreated) error {
	return p.Publish(ctx, EventNotificationCreated, payload)
}

func (p *Publisher) PublishFollowApprovalRequested(ctx context.Context, payload FollowApproval) error {
	return p.Publish(ctx, EventFollowApprovalRequested, payload)
}

func (p *Publisher) PublishFollowApprovalCompleted(ctx context.Context, payload FollowApproval) error {
	return p.Publish(ctx, EventFollowApprovalCompleted, payload)
}
